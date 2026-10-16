package main

// Recovery only consumes fixed root-owned journals while holding captureLock.
// It never kills a process or guesses ownership from a covering route lookup.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func ownedTransparentRouteInTable(table []byte, r transparentRoute) bool {
	want, err := netip.ParsePrefix(r.Prefix)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != r.Interface || !strings.Contains(f[2], "U") || !strings.Contains(f[2], "S") || strings.ContainsAny(f[2], "WLY") {
			continue
		}
		// Scoped and global entries are separate owners, even on the same NIC.
		if strings.Contains(f[2], "I") != r.Scoped {
			continue
		}
		var prefix netip.Prefix
		if f[0] == "default" {
			prefix = netip.PrefixFrom(want.Addr().Unmap(), 0).Masked()
		} else {
			var ok bool
			prefix, ok = parseRoutePrefix(f[0])
			if !ok {
				continue
			}
		}
		if prefix != want.Masked() {
			continue
		}
		if r.Gateway != "" {
			if f[1] != r.Gateway {
				continue
			}
		} else if !strings.HasPrefix(f[1], "link#") || strings.Contains(f[2], "G") {
			continue
		}
		return true
	}
	return false
}

func validateRecoveryState(s transparentState) error {
	number, err := strconv.Atoi(strings.TrimPrefix(s.Device, "utun"))
	if !strings.HasPrefix(s.Device, "utun") || err != nil || number < 100 || number >= 1000 || s.Device != fmt.Sprintf("utun%d", number) || len(s.Routes) > 16 || s.CorePID < 0 {
		return errors.New("invalid recovery journal")
	}
	switch s.State {
	case "starting", "ready", "stopped", "cleanupFailed":
	default:
		return errors.New("unknown recovery state")
	}
	for _, r := range s.Routes {
		p, err := netip.ParsePrefix(r.Prefix)
		if err != nil || p != p.Masked() || p.String() != r.Prefix {
			return errors.New("invalid recovery route")
		}
		if r.Interface == s.Device && !r.Scoped && r.Gateway == "" {
			allowed := false
			for _, value := range append(transparentCapturePrefixes(false), transparentCapturePrefixes(true)...) {
				if value == r.Prefix {
					allowed = true
				}
			}
			if allowed {
				continue
			}
		}
		gateway, e := netip.ParseAddr(r.Gateway)
		if e != nil || !physicalInterface.MatchString(r.Interface) || gateway.Is4() != p.Addr().Is4() || (gateway.Zone() != "" && gateway.Zone() != r.Interface) {
			return errors.New("invalid recovery gateway")
		}
		if r.Scoped && p.Bits() == 0 {
			continue
		}
		if !r.Scoped && p.Addr().Is4() && p.Bits() == 32 {
			continue
		}
		return errors.New("unrecognized recovery route")
	}
	return nil
}

func readRecoveryState(directory string, uid uint32) (transparentState, []byte, error) {
	var s transparentState
	st, err := os.Lstat(directory)
	if err != nil {
		return s, nil, err
	}
	if !st.IsDir() || st.Mode().Perm() != 0700 {
		return s, nil, errors.New("unsafe recovery directory")
	}
	f, err := os.Open(directory)
	if err != nil {
		return s, nil, err
	}
	defer f.Close()
	data, _, err := readOwned(f, "state.json", uid)
	if err != nil {
		return s, nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		return s, nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return s, nil, errors.New("trailing recovery data")
	}
	return s, data, validateRecoveryState(s)
}

func recoverySafe(s transparentState, processes []processIdentity, interfaces []net.Interface) bool {
	for _, p := range processes {
		if (s.CorePID > 0 && p.PID == s.CorePID) || (p.UID == 0 && filepath.Base(p.Path) == "mihomo" && strings.HasPrefix(p.Path, serviceBase+"/versions/")) {
			return false
		}
	}
	for _, i := range interfaces {
		if i.Name == s.Device {
			return false
		}
	}
	return true
}

func backupRecoveryState(name string, data []byte) error {
	directory := serviceBase + "/recovery"
	if err := ensureRootDirectory(directory); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return err
	}
	// One bounded journal backup; no core configuration, token or credentials.
	record, err := json.Marshal(struct {
		Time    string          `json:"time"`
		Session string          `json:"session"`
		Journal json.RawMessage `json:"journal"`
	}{time.Now().UTC().Format(time.RFC3339), name, data})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".recovery-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(record); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(directory, "last-session.json"))
}

func recoverySummary() string {
	path := serviceBase + "/recovery/last-session.json"
	if secureRootPath(path, false) != nil {
		return "unavailable"
	}
	f, err := os.Open(filepath.Dir(path))
	if err != nil {
		return "unavailable"
	}
	defer f.Close()
	data, _, err := readOwned(f, filepath.Base(path), 0)
	if err != nil {
		return "unavailable"
	}
	var record struct {
		Time    string           `json:"time"`
		Journal transparentState `json:"journal"`
	}
	if json.Unmarshal(data, &record) != nil || validateRecoveryState(record.Journal) != nil {
		return "unavailable"
	}
	// Expose only bounded metadata through the existing authenticated diagnostics.
	result, _ := json.Marshal(struct {
		Time          string `json:"time"`
		PreviousState string `json:"previousState"`
		Routes        int    `json:"recordedRoutes"`
	}{record.Time, record.Journal.State, len(record.Journal.Routes)})
	return string(result)
}

func recoverTransparentDirectory(directory string, uid uint32, processes []processIdentity, interfaces []net.Interface, command func(string, ...string) ([]byte, error), backup func(string, []byte) error) error {
	s, data, err := readRecoveryState(directory, uid)
	if err != nil {
		trace("session.recovery_invalid", 1)
		return errors.New("残留会话记录无效，已保留现场；请导出诊断。")
	}
	if !recoverySafe(s, processes, interfaces) {
		trace("session.recovery_active", 1)
		return errors.New("旧分流进程或虚拟网卡仍在使用，已暂停重连；请导出诊断。")
	}
	if err = backup(filepath.Base(directory), data); err != nil {
		return errors.New("无法备份残留会话，未清理；请导出诊断。")
	}
	trace("session.recovery_started", int64(len(s.Routes)))
	if err = cleanupTransparentRoutesWith(filepath.Join(directory, "state.json"), &s, command); err != nil {
		trace("session.recovery_failed", 1)
		return err
	}
	if err = os.RemoveAll(directory); err != nil {
		trace("session.recovery_failed", 2)
		return errors.New("无法释放残留会话目录；请导出诊断。")
	}
	trace("session.recovery_ok", 1)
	return nil
}

// The caller must hold the exclusive capture lock, including during install.
func recoverAbandonedSessions() error {
	if err := secureRootPath(serviceRunDirectory, true); err != nil {
		return err
	}
	entries, err := os.ReadDir(serviceRunDirectory)
	if err != nil {
		return err
	}
	var directories []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "company-") {
			return errors.New("存在旧公司路由记录，已保留现场；请导出诊断。")
		}
		if strings.HasPrefix(entry.Name(), "transparent-") {
			directories = append(directories, filepath.Join(serviceRunDirectory, entry.Name()))
		}
	}
	if len(directories) == 0 {
		return nil
	}
	if len(directories) > 8 {
		return errors.New("残留会话数量异常，已暂停恢复；请导出诊断。")
	}
	processes, err := processIdentities()
	if err != nil {
		return errors.New("无法确认旧分流进程状态，未清理残留会话。")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, directory := range directories {
		if err = secureRootPath(directory, true); err != nil {
			return err
		}
		if err = recoverTransparentDirectory(directory, 0, processes, interfaces, systemCommand, backupRecoveryState); err != nil {
			return err
		}
	}
	return nil
}
