package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var serviceBinaries = []string{"GoConnectTransport", "openconnect", "mihomo"}
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type servicePolicy struct {
	Version  string `json:"version"`
	UID      uint32 `json:"uid"`
	Revision string `json:"revision"`
}

// All persistent executable code and every ancestor are controlled by root.
// No user-writable app bundle, link, custom shell, or environment is executed.
func secureRootPath(path string, directory bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("non-canonical service path")
	}
	current := "/"
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		raw := st.Sys().(*syscall.Stat_t)
		if raw.Uid != 0 || st.Mode().Perm()&0o022 != 0 || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe service ownership or permissions")
		}
		if i < len(parts)-1 || directory {
			if !st.IsDir() {
				return errors.New("service directory required")
			}
		} else if !st.Mode().IsRegular() || raw.Nlink != 1 {
			return errors.New("unsafe service file")
		}
	}
	return nil
}

func ensureRootDirectory(path string) error {
	if err := secureRootPath(path, true); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ensureRootDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	return secureRootPath(path, true)
}

func rootLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = secureRootPath(path, false); err != nil {
		f.Close()
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("服务正在处理其它操作，请稍后重试。")
	}
	return f, nil
}

func readServicePolicy() (servicePolicy, error) {
	var p servicePolicy
	if err := secureRootPath(servicePolicyPath, false); err != nil {
		return p, err
	}
	data, err := os.ReadFile(servicePolicyPath)
	if err != nil || len(data) > 4096 {
		return p, errors.New("invalid service policy")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return p, err
	}
	if d.Decode(new(any)) != io.EOF || p.Version != serviceVersion || p.UID < 501 || !revisionPattern.MatchString(p.Revision) {
		return p, errors.New("invalid service policy")
	}
	return p, nil
}

func openRuntimeBinary(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > 200*1024*1024 {
		file.Close()
		return nil, errors.New("invalid runtime binary")
	}
	return file, nil
}

// Pin every file loaded by the privileged native OpenConnect path, including
// libraries and certificate roots. No Homebrew/user-writable load path is allowed.
func serviceRuntimeFiles(runtime string) ([]string, error) {
	files := []string{"share/cacert.pem"}
	for _, name := range serviceBinaries {
		files = append(files, "bin/"+name)
	}
	entries, err := os.ReadDir(filepath.Join(runtime, "lib"))
	if err != nil {
		return nil, err
	}
	if len(entries) > 128 {
		return nil, errors.New("too many runtime libraries")
	}
	for _, entry := range entries {
		if !regexp.MustCompile(`^[a-zA-Z0-9_.+-]+\.dylib$`).MatchString(entry.Name()) || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, errors.New("invalid runtime library")
		}
		files = append(files, "lib/"+entry.Name())
	}
	sort.Strings(files)
	return files, nil
}
func runtimeRevision(runtime string) (string, error) {
	files, err := serviceRuntimeFiles(runtime)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, name := range files {
		file, err := openRuntimeBinary(filepath.Join(runtime, name))
		if err != nil {
			return "", err
		}
		sum := sha256.New()
		_, err = io.Copy(sum, file)
		file.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s:%x\n", name, sum.Sum(nil))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func serviceOtoolPath() string {
	const commandLineTools = "/Library/Developer/CommandLineTools/usr/bin/otool"
	if info, err := os.Stat(commandLineTools); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
		return commandLineTools
	}
	return "/usr/bin/otool"
}

func copyServiceRuntime(source, target string) error {
	files, err := serviceRuntimeFiles(source)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, name := range files {
		allowed[name] = true
	}
	for _, dir := range []string{"bin", "lib", "share"} {
		if err = os.Mkdir(filepath.Join(target, dir), 0755); err != nil {
			return err
		}
	}
	for _, name := range files {
		input, err := openRuntimeBinary(filepath.Join(source, name))
		if err != nil {
			return err
		}
		mode := os.FileMode(0755)
		if strings.HasPrefix(name, "share/") {
			mode = 0644
		}
		dest := filepath.Join(target, name)
		output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			input.Close()
			return err
		}
		_, err = io.Copy(output, io.LimitReader(input, 200*1024*1024+1))
		input.Close()
		syncErr := output.Sync()
		output.Close()
		if err != nil {
			return err
		}
		if syncErr != nil {
			return syncErr
		}
	}
	for _, name := range files {
		if strings.HasPrefix(name, "share/") {
			continue
		}
		dest := filepath.Join(target, name)
		if _, err = systemCommand("/usr/bin/codesign", "--verify", "--strict", dest); err != nil {
			return errors.New("服务组件签名校验失败。")
		}
		deps, err := systemCommand(serviceOtoolPath(), "-L", dest)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(deps), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			dep := fields[0]
			if strings.HasPrefix(dep, "/usr/lib/") || strings.HasPrefix(dep, "/System/Library/") {
				continue
			}
			if strings.HasPrefix(dep, "@loader_path/") {
				relative := filepath.Clean(filepath.Join(filepath.Dir(name), strings.TrimPrefix(dep, "@loader_path/")))
				if allowed[relative] && strings.HasPrefix(relative, "lib/") {
					continue
				}
			}
			return errors.New("服务组件引用了可变的外部依赖。")
		}
	}
	return nil
}

func atomicRootWrite(path string, data []byte) error {
	if err := secureRootPath(filepath.Dir(path), true); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		if err = secureRootPath(path, false); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".goconnect-write-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0o644); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	temp.Close()
	if err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func networkServicePlist(revision string) []byte {
	// revision is a SHA-256, never user-provided plist syntax.
	executable := filepath.Join(serviceBase, "versions", revision, "bin/GoConnectTransport")
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>service-run</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>ProcessType</key><string>Background</string>
<key>ThrottleInterval</key><integer>10</integer><key>ExitTimeOut</key><integer>20</integer>
<key>Umask</key><integer>63</integer>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/usr/bin:/bin:/usr/sbin:/sbin</string><key>HOME</key><string>/var/root</string><key>TMPDIR</key><string>/private/var/tmp</string></dict>
</dict></plist>
`, serviceLabel, executable))
}

func stopInstalledService() error {
	if _, err := systemCommand("/bin/launchctl", "print", "system/"+serviceLabel); err != nil {
		return nil
	}
	if _, err := systemCommand("/bin/launchctl", "bootout", "system/"+serviceLabel); err != nil {
		return errors.New("无法停止本机服务，请稍后重试。")
	}
	return nil
}

func installNetworkService(rawUID string) (result error) {
	if os.Geteuid() != 0 {
		return errors.New("首次安装服务需要管理员授权。")
	}
	uid, err := strconv.ParseUint(rawUID, 10, 32)
	if err != nil || uid < 501 {
		return errors.New("invalid local user")
	}
	if _, err = user.LookupId(rawUID); err != nil {
		return err
	}
	if err = ensureRootDirectory(serviceRunDirectory); err != nil {
		return err
	}
	lock, err := rootLock(serviceRunDirectory + "/install.lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	capture, err := captureLock()
	if err != nil {
		return errors.New("请先断开 VPN 或停止分流自检，再安装服务。")
	}
	defer capture.Close()
	if err = ensureRootDirectory(serviceBase + "/versions"); err != nil {
		return err
	}
	if err = ensureRootDirectory(filepath.Dir(servicePlist)); err != nil {
		return err
	}
	source, err := runtimePath()
	if err != nil {
		return err
	}
	revision, err := runtimeRevision(source)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(serviceBase+"/versions", ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = os.Chmod(stage, 0o755); err != nil {
		return err
	}
	if err = copyServiceRuntime(source, stage); err != nil {
		return err
	}
	copied, err := runtimeRevision(stage)
	if err != nil || copied != revision {
		return errors.New("安装期间服务组件发生变化，请重试。")
	}
	target := filepath.Join(serviceBase, "versions", revision)
	if _, err = os.Lstat(target); os.IsNotExist(err) {
		if err = os.Rename(stage, target); err != nil {
			return err
		}
	} else {
		if err = secureRootPath(target, true); err != nil {
			return err
		}
		existing, e := runtimeRevision(target)
		if e != nil || existing != revision {
			return errors.New("已安装组件校验失败，请卸载服务后重装。")
		}
		for _, name := range serviceBinaries {
			if err = secureRootPath(filepath.Join(target, "bin", name), false); err != nil {
				return err
			}
		}
	}
	var oldPolicy, oldPlist []byte
	for path, dest := range map[string]*[]byte{servicePolicyPath: &oldPolicy, servicePlist: &oldPlist} {
		if _, e := os.Lstat(path); os.IsNotExist(e) {
			continue
		}
		if e := secureRootPath(path, false); e != nil {
			return e
		}
		data, e := os.ReadFile(path)
		if e != nil || len(data) > 65536 {
			return errors.New("invalid previous service installation")
		}
		*dest = data
	}
	if err = stopInstalledService(); err != nil {
		return err
	}
	// A failed replacement restores the previous approved service and launch job.
	defer func() {
		if result == nil {
			return
		}
		_ = stopInstalledService()
		if len(oldPolicy) > 0 {
			_ = atomicRootWrite(servicePolicyPath, oldPolicy)
		} else {
			_ = os.Remove(servicePolicyPath)
		}
		if len(oldPlist) > 0 {
			_ = atomicRootWrite(servicePlist, oldPlist)
			_, _ = systemCommand("/bin/launchctl", "bootstrap", "system", servicePlist)
		} else {
			_ = os.Remove(servicePlist)
		}
	}()
	data, _ := json.Marshal(servicePolicy{Version: serviceVersion, UID: uint32(uid), Revision: revision})
	if err = atomicRootWrite(servicePolicyPath, data); err != nil {
		return err
	}
	if err = atomicRootWrite(servicePlist, networkServicePlist(revision)); err != nil {
		return err
	}
	if _, err = systemCommand("/bin/launchctl", "enable", "system/"+serviceLabel); err != nil {
		return err
	}
	if _, err = systemCommand("/bin/launchctl", "bootstrap", "system", servicePlist); err != nil {
		return errors.New("macOS 未能启动本机服务。")
	}
	for attempt := 0; attempt < 30; attempt++ {
		conn, e := serviceConnect()
		if e == nil {
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_ = json.NewEncoder(conn).Encode(serviceRequest{Action: "status"})
			var reply serviceReply
			e = json.NewDecoder(conn).Decode(&reply)
			conn.Close()
			if e == nil && reply.State == "ready" {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("服务未就绪，已尝试恢复之前的安装。")
}

func uninstallNetworkService() error {
	if os.Geteuid() != 0 {
		return errors.New("移除本机服务需要管理员授权。")
	}
	if err := ensureRootDirectory(serviceRunDirectory); err != nil {
		return err
	}
	lock, err := rootLock(serviceRunDirectory + "/install.lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	capture, err := captureLock()
	if err != nil {
		return errors.New("请先断开 VPN 或停止分流自检。")
	}
	defer capture.Close()
	policy, policyErr := readServicePolicy()
	if err = stopInstalledService(); err != nil {
		return err
	}
	if policyErr == nil {
		if e := collectAppGroups(policy.UID, nil, true); e != nil {
			fmt.Fprintln(os.Stderr, "routing label maintenance deferred")
		}
	}
	if _, err = os.Lstat(servicePlist); err == nil {
		if err = secureRootPath(servicePlist, false); err != nil {
			return err
		}
		if err = os.Remove(servicePlist); err != nil {
			return err
		}
	}
	if _, err = os.Lstat(serviceBase); err == nil {
		if err = secureRootPath(serviceBase, true); err != nil {
			return err
		}
		if err = os.RemoveAll(filepath.Join(serviceBase, "versions")); err != nil {
			return err
		}
		if err = os.Remove(servicePolicyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// Keep the locked root-owned coordination directory; no user data is here.
	_ = os.Remove(serviceSocket)
	return nil
}
