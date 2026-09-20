package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const captureAddress = "198.19.254.1"

type capturedApp struct {
	Path, Binary, Group string
	GID                 uint32
}

type processIdentity struct {
	UID, GID uint32
	PID      int
	Path     string
}

var processLine = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s+(.+)$`)

func processIdentities() ([]processIdentity, error) {
	data, err := systemCommand("/bin/ps", "-axo", "uid=,gid=,pid=,comm=")
	if err != nil {
		return nil, err
	}
	var result []processIdentity
	for _, line := range strings.Split(string(data), "\n") {
		m := processLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		uid, _ := strconv.ParseUint(m[1], 10, 32)
		gid, _ := strconv.ParseUint(m[2], 10, 32)
		pid, _ := strconv.Atoi(m[3])
		result = append(result, processIdentity{uint32(uid), uint32(gid), pid, m[4]})
	}
	return result, nil
}

func systemCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	type output struct {
		data []byte
		err  error
	}
	done := make(chan output, 1)
	go func() { data, err := cmd.Output(); done <- output{data, err} }()
	select {
	case result := <-done:
		return result.data, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("system command timeout: %s", name)
	}

}

func appGroupName(uid uint32, path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("goc_%d_%s", uid, hex.EncodeToString(sum[:8]))
}

// Groups have no members and grant no privileges. They are persistent labels so
// an already-tagged app remains identifiable across reconnects and profile changes.
func ensureAppGroup(uid uint32, path string) (string, uint32, error) {
	name, gid, err := ensureAppGroupAttempt(uid, path, 0)
	if err == nil {
		err = adoptAppGroupPath(uid, name, path)
	}
	return name, gid, err
}

func ensureAppGroupAttempt(uid uint32, path string, attempt int) (string, uint32, error) {
	if attempt >= 3 {
		return "", 0, errors.New("无法确认本机应用分流标记，请稍后重试。")
	}
	name := appGroupName(uid, path)
	data, err := systemCommand("/usr/bin/dscl", ".", "-list", "/Groups", "PrimaryGroupID")
	if err != nil {
		return "", 0, errors.New("无法读取本机应用分流标记。")
	}
	used := map[uint32]int{}
	var existing uint32
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, e := strconv.ParseUint(fields[1], 10, 32)
		if e != nil {
			continue
		}
		id := uint32(value)
		used[id]++
		if fields[0] == name {
			existing = id
		}
	}
	if existing != 0 {
		if existing < 61000 || existing >= 65000 || used[existing] != 1 {
			return "", 0, errors.New("应用分流标记冲突，未修改网络。")
		}
		return name, existing, nil
	}
	processGroups, err := processGroupIDs()
	if err != nil {
		return "", 0, err
	}
	for gid := range processGroups {
		used[gid]++
	}
	var gid uint32 = 61000
	for used[gid] != 0 && gid < 65000 {
		gid++
	}
	if gid >= 65000 {
		return "", 0, errors.New("本机应用分流标记已用完。")
	}
	if _, err = systemCommand("/usr/sbin/dseditgroup", "-o", "create", "-i", strconv.FormatUint(uint64(gid), 10), "-r", appGroupDescription(path), name); err != nil {
		return "", 0, errors.New("无法创建本机应用分流标记。")
	}
	// Re-read the directory instead of assuming the allocator won a race.
	return ensureAppGroupAttempt(uid, path, attempt+1)
}

func captureLock() (*os.File, error) {
	fd, err := unix.Open("/var/run/com.willhsu.GoConnect.capture.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "capture lock")
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	raw := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0o600 || raw.Uid != 0 || raw.Nlink != 1 {
		f.Close()
		return nil, errors.New("unsafe capture lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("已有 GoConnect 应用分流会话，请先断开。")
	}
	return f, nil
}

func appExecutable(path string, probe bool) (string, error) {
	name := "Probe"
	if !probe {
		if strings.HasPrefix(path, "/System/") || filepath.Base(path) == "Safari.app" || filepath.Base(path) == "Safari Technology Preview.app" {
			return "", errors.New("系统应用及 Safari 不支持本机启动标记分流，请从白名单移除。")
		}
		data, err := systemCommand("/usr/bin/plutil", "-extract", "CFBundleExecutable", "raw", "-o", "-", filepath.Join(path, "Contents/Info.plist"))
		if err != nil {
			return "", errors.New("无法读取白名单应用的启动文件。")
		}
		name = strings.TrimSpace(string(data))
	}
	if name == "" || filepath.Base(name) != name || name == "." || strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("invalid application executable")
	}
	binary := filepath.Join(path, "Contents/MacOS", name)
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil || !strings.HasPrefix(resolved, path+"/Contents/") {
		return "", errors.New("application executable moved")
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return "", errors.New("application is not executable")
	}
	return resolved, nil
}

func prepareCapturedApps(uid uint32, paths []string, probe bool) ([]capturedApp, error) {
	// Maintenance cannot turn a usable connection into a failed one.
	if err := collectAppGroups(uid, paths, false); err != nil {
		fmt.Fprintln(os.Stderr, "routing label maintenance deferred")
	}
	var apps []capturedApp
	for _, path := range paths {
		binary, err := appExecutable(path, probe)
		if err != nil {
			return nil, err
		}
		group, gid, err := ensureAppGroup(uid, path)
		if err != nil {
			return nil, err
		}
		apps = append(apps, capturedApp{path, binary, group, gid})
	}
	return apps, nil
}

func appProcessCounts(app capturedApp, uid uint32, processes []processIdentity) (managed, unmanaged int) {
	for _, process := range processes {
		if process.UID != uid || !strings.HasPrefix(process.Path, app.Path+"/Contents/") {
			continue
		}
		if process.GID == app.GID {
			managed++
		} else {
			unmanaged++
		}
	}
	return
}

func appMainRunning(app capturedApp, uid uint32, processes []processIdentity) bool {
	for _, process := range processes {
		if process.UID == uid && process.GID == app.GID && process.Path == app.Binary {
			return true
		}
	}
	return false
}

func validateCaptureEnvironment() error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	addressesToReserve := []netip.Addr{netip.MustParseAddr(captureAddress), netip.MustParseAddr(systemCaptureAddress), netip.MustParseAddr(liveIPv6Address), netip.MustParseAddr(systemCaptureIPv6)}
	for _, iface := range interfaces {
		addresses, e := iface.Addrs()
		if e != nil {
			return e
		}
		for _, item := range addresses {
			prefix, e := netip.ParsePrefix(item.String())
			if e == nil {
				for _, address := range addressesToReserve {
					if prefix.Contains(address) {
						return errors.New("本次 TUN 地址已被其它接口使用，未修改网络。")
					}
				}
			}
		}
	}
	// A second global TUN binds sockets to its interface; replies injected on
	// ours cannot reach them. Check actual routes, not just known process names.
	for _, target := range []string{"1.1.1.1", "8.8.8.8"} {
		iface, err := routeInterface(target)
		if err != nil {
			return err
		}
		if strings.HasPrefix(iface, "utun") || strings.HasPrefix(iface, "ppp") || strings.HasPrefix(iface, "ipsec") {
			return errors.New("其它 VPN 正在接管公网路由，请先断开它的全局隧道。")
		}
	}
	return nil
}

func routeInterface(target string) (string, error) {
	data, err := systemCommand("/sbin/route", "-n", "get", target)
	if err != nil {
		return "", errors.New("无法读取原网络路由。")
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "interface:" {
			return fields[1], nil
		}
	}
	return "", errors.New("原网络路由不可用。")
}

func captureRules(uid uint32, apps []capturedApp, device string, probe, blocked bool) string {
	var rules strings.Builder
	for _, app := range apps {
		target := "any"
		if probe {
			target = probeTarget
		}
		if !blocked {
			// Recheck the socket owner on every packet. Floating PF states can
			// outlive the socket and capture an unrelated app that reuses its port;
			// an interface-scoped flush cannot remove those floating states.
			fmt.Fprintf(&rules, "pass out quick on ! lo0 route-to (%s %s) inet proto { tcp udp } from any to %s user %d group %d flags any no state\n", device, captureAddress, target, uid, app.GID)
		}
		// Always name TCP/UDP: PF cannot attribute other IP protocols to a user
		// or group. An unqualified block rule could affect unrelated ICMP traffic.
		fmt.Fprintf(&rules, "block return out quick on ! lo0 inet proto { tcp udp } from any to %s user %d group %d\n", target, uid, app.GID)
		fmt.Fprintf(&rules, "block return out quick on ! lo0 inet6 proto { tcp udp } from any to any user %d group %d\n", uid, app.GID)
	}
	return rules.String()
}

type packetFilter struct {
	anchor, token string
	loaded        bool
}

func (p *packetFilter) load(rules string) error {
	for _, dry := range []bool{true, false} {
		args := []string{"-q", "-a", p.anchor, "-f", "-"}
		if dry {
			args = append([]string{"-n"}, args...)
		}
		if !dry {
			p.loaded = true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, "/sbin/pfctl", args...)
		cmd.Stdin = strings.NewReader(rules)
		err := cmd.Run()
		cancel()
		if err != nil {
			return errors.New("无法安装本次应用分流规则。")
		}
	}
	p.loaded = true
	return nil
}

func startPacketFilter(uid uint32, apps []capturedApp, device, token string, request routeRequest, tailscale tailscaleBypass) (*packetFilter, error) {
	main, err := systemCommand("/sbin/pfctl", "-sr")
	if err != nil || !regexp.MustCompile(`(?m)^anchor "com\.apple/\*" all\s*$`).Match(main) {
		return nil, errors.New("系统 PF 入口已被其它工具修改，无法安全启用应用分流。")
	}
	info, err := systemCommand("/sbin/pfctl", "-si")
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(info, []byte("Status: Enabled")) {
		nested, e := systemCommand("/sbin/pfctl", "-a", "*", "-sr")
		if e != nil || regexp.MustCompile(`(?m)^\s*(pass|block|match)\s`).Match(nested) {
			return nil, errors.New("PF 中存在未启用的其它过滤规则，未改变其状态。")
		}
	}
	sum := sha256.Sum256([]byte(token))
	p := &packetFilter{anchor: fmt.Sprintf("com.apple/goconnect-%d-%x", uid, sum[:8])}
	if err = p.load(routingRules(uid, apps, device, request, tailscale)); err != nil {
		_ = p.close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/sbin/pfctl", "-E")
	out, err := cmd.CombinedOutput()
	m := regexp.MustCompile(`Token\s*:\s*(\d+)`).FindSubmatch(out)
	if len(m) == 2 {
		p.token = string(m[1])
	}
	if err != nil || p.token == "" {
		_ = p.close()
		return nil, errors.New("无法取得本次 PF 启用凭证。")
	}
	return p, nil
}

func (p *packetFilter) close() error {
	if p == nil {
		return nil
	}
	if p.loaded {
		if err := p.load(""); err != nil {
			return err
		}
		p.loaded = false
	}
	if p.token != "" {
		if _, err := systemCommand("/sbin/pfctl", "-X", p.token); err != nil {
			return err
		}
		p.token = ""
	}
	return nil
}

func launchCapturedApp(ctx context.Context, uid uint32, app capturedApp, runtime string) error {
	processes, err := processIdentities()
	if err != nil {
		return err
	}
	_, unmanaged := appProcessCounts(app, uid, processes)
	if unmanaged > 0 {
		return fmt.Errorf("请先退出 %s，再点击连接或启动；GoConnect 会为它启用专属线路。", strings.TrimSuffix(filepath.Base(app.Path), ".app"))
	}
	if appMainRunning(app, uid, processes) {
		return nil
	}
	cmd := exec.Command("/bin/launchctl", "asuser", strconv.FormatUint(uint64(uid), 10), filepath.Join(runtime, "bin/GoConnectLauncher"), strconv.FormatUint(uint64(uid), 10), strconv.FormatUint(uint64(app.GID), 10), app.Binary)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("%s 未能启动。", strings.TrimSuffix(filepath.Base(app.Path), ".app"))
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processes, e := processIdentities()
		if e == nil {
			_, unmanaged = appProcessCounts(app, uid, processes)
			if appMainRunning(app, uid, processes) && unmanaged == 0 {
				return nil
			}
			if unmanaged > 0 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("未能确认 %s 的分流标记；该 App 可能不支持此启动方式，未将它报告为已正确分流。", strings.TrimSuffix(filepath.Base(app.Path), ".app"))
}
