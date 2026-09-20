package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Each route is a short-lived worker of the locally authorized network service.
// The GUI supplies data, not a shell command or arbitrary Mihomo configuration.
type domainRule struct {
	Domain            string `json:"domain"`
	IncludeSubdomains bool   `json:"includeSubdomains"`
}

type routeRequest struct {
	TransparentRouting bool     `json:"transparentRouting,omitempty"`
	CompanyShared      bool     `json:"companyShared,omitempty"`
	VPNAddresses       []string `json:"vpnAddresses,omitempty"`
	// Root-discovered resolver addresses; never accepted from session JSON.
	systemDevice     string
	coreGID          uint32
	RemoteNetworks   []string `json:"remoteNetworks,omitempty"`
	RemoteExcluded   []string `json:"remoteExcluded,omitempty"`
	systemLAN        []localNetworkRoute
	systemDNS        []netip.Addr
	LiveRouting      bool         `json:"liveRouting,omitempty"`
	DirectDomains    []domainRule `json:"directDomains,omitempty"`
	OwnerPID         int          `json:"ownerPID"`
	ControlDirectory string       `json:"controlDirectory"`
	Token            string       `json:"token"`
	SOCKSPort        uint16       `json:"socksPort"`
	AppPaths         []string     `json:"appPaths"`
	Gateway          string       `json:"gateway"`
	ProbeOnly        bool         `json:"probeOnly,omitempty"`
	RoutingMode      string       `json:"routingMode,omitempty"`
}

func (r routeRequest) validate() error {
	if r.TransparentRouting && (r.CompanyShared || !r.LiveRouting) {
		return errors.New("invalid transparent session")
	}
	if r.CompanyShared {
		if r.OwnerPID <= 1 || r.SOCKSPort == 0 || len(r.Token) < 32 || len(r.Token) > 200 || strings.ContainsAny(r.Token, "\x00\r\n:") || len(r.AppPaths) > 0 || r.LiveRouting || r.ProbeOnly || len(r.DirectDomains) > 0 || len(r.VPNAddresses) == 0 || len(r.VPNAddresses) > 16 {
			return errors.New("invalid shared company session")
		}
		if _, e := companyPlan(r.RemoteNetworks, nil, tailscaleBypass{Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6}}); e != nil {
			return e
		}
		gateway, e := netip.ParseAddr(r.Gateway)
		if e != nil {
			return e
		}
		if e = validateCompanyGateway(r.RemoteNetworks, gateway); e != nil {
			return e
		}
		return validateCompanyFamilies(r.RemoteNetworks, r.VPNAddresses)
	}

	if len(r.RemoteNetworks) > 256 || len(r.RemoteExcluded) > 256 {
		return errors.New("too many remote networks")
	}
	for _, value := range append(append([]string{}, r.RemoteNetworks...), r.RemoteExcluded...) {
		if _, ok := remotePrefix(value); !ok {
			return errors.New("invalid remote network")
		}
	}
	if len(r.DirectDomains) > 256 {
		return errors.New("too many domain rules")
	}
	for _, rule := range r.DirectDomains {
		if _, address := directIPPrefix(rule.Domain); !address && !validDirectDomain(rule.Domain) {
			return errors.New("invalid direct domain")
		}
	}
	if r.OwnerPID <= 1 || r.SOCKSPort == 0 || len(r.Token) < 32 || len(r.Token) > 200 || (!r.LiveRouting && r.mode() == modeWhitelist && len(r.AppPaths) == 0) || len(r.AppPaths) > 256 {
		return errors.New("invalid session")
	}
	if r.mode() != modeWhitelist && r.mode() != modeGlobal {
		return errors.New("invalid routing mode")
	}
	if strings.ContainsAny(r.Token, "\x00\r\n") {
		return errors.New("invalid token")
	}
	seenPaths := map[string]bool{}
	for _, path := range r.AppPaths {
		if seenPaths[path] {
			return errors.New("duplicate application path")
		}
		seenPaths[path] = true
		if !validRoutingTargetPath(path, r.LiveRouting) {
			return errors.New("invalid application path")
		}
	}
	if _, err := netip.ParseAddr(r.Gateway); err != nil {
		return errors.New("invalid VPN gateway")
	}
	return nil
}

func pathRule(path string) string {
	return "PROCESS-PATH-REGEX," + appBundleContentsPathPattern(path) + ",GoConnect"
}

func makeConfig(r routeRequest, runtime, device string, port uint16) map[string]any {
	if r.LiveRouting {
		return makeLiveConfig(r, device, port)
	}
	// A SOCKS endpoint belongs to one VPN session. Never fall back to DIRECT on failure.
	rules := directDomainRules(r.DirectDomains)
	for _, app := range r.AppPaths {
		rule := pathRule(app)
		if r.mode() == modeGlobal {
			rule = strings.TrimSuffix(rule, ",GoConnect") + ",REJECT"
		}
		rules = append(rules, rule)
	}
	if r.mode() != modeWhitelist {
		rules = append(rules, "MATCH,GoConnect")
	} else {
		rules = append(rules, "MATCH,REJECT")
	}
	config := map[string]any{
		"mode": "rule", "find-process-mode": "always", "ipv6": false,
		"allow-lan": false, "log-level": "silent", "geo-auto-update": false,
		"external-controller": net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), "secret": r.Token,
		"profile": map[string]any{"store-selected": false, "store-fake-ip": false},
		"dns":     map[string]any{"enable": false, "fake-ip-range": captureAddress + "/30"}, "sniffer": map[string]any{"enable": false},
		"tun": map[string]any{
			"enable": true, "stack": "gvisor", "device": device, "mtu": 1400,
			"auto-route": false, "auto-detect-interface": false, "dns-hijack": []string{},
			"inet6-address": []string{},
		},
		"proxies": []map[string]any{{"name": "GoConnect", "type": "socks5", "server": "127.0.0.1", "port": r.SOCKSPort, "username": "goconnect", "password": r.Token, "udp": true}},
		"rules":   rules,
	}
	if len(r.DirectDomains) > 0 {
		config["sniffer"] = map[string]any{"enable": true, "parse-pure-ip": true, "force-dns-mapping": true, "override-destination": false, "sniff": map[string]any{"HTTP": map[string]any{"ports": []string{"1-65535"}}, "TLS": map[string]any{"ports": []string{"1-65535"}}, "QUIC": map[string]any{"ports": []int{443, 8443}}}}
	}
	return config
}
func validDirectDomain(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") || s != strings.ToLower(s) || strings.Trim(s, "0123456789.") == "" {
		return false
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func directDomainRules(domains []domainRule) []string {
	rules := []string{}
	for _, r := range domains {
		if prefix, ok := directIPPrefix(r.Domain); ok {
			kind := "IP-CIDR6"
			if prefix.Addr().Is4() {
				kind = "IP-CIDR"
			}
			rules = append(rules, kind+","+prefix.String()+",DIRECT,no-resolve")
			continue
		}
		kind := "DOMAIN"
		if r.IncludeSubdomains {
			kind = "DOMAIN-SUFFIX"
		}
		rules = append(rules, kind+","+r.Domain+",DIRECT")
	}
	return rules
}

type controlFiles struct {
	directory   *os.File
	status      *os.File
	uid         uint32
	request     routeRequest
	started     unix.Timeval
	snapshot    routeSnapshot
	apps        []capturedApp
	lastCommand uint64
}

// Open relative to an already-open directory and never follow user-supplied symlinks.
func readOwned(dir *os.File, name string, uid uint32) ([]byte, os.FileInfo, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	raw := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 || raw.Uid != uid || raw.Nlink != 1 || st.Size() > 65536 {
		return nil, nil, errors.New("unsafe control file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	return data, st, err
}

func openControl(path string) (*controlFiles, error) { return inspectControl(path, true) }

func inspectControl(path string, createStatus bool) (*controlFiles, error) {
	if filepath.Base(path) != "session.json" {
		return nil, errors.New("invalid session filename")
	}
	dirPath, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), dirPath)
	ok := false
	defer func() {
		if !ok {
			_ = dir.Close()
		}
	}()
	st, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	uid := st.Sys().(*syscall.Stat_t).Uid
	if st.Mode().Perm() != 0o700 || uid == 0 {
		return nil, errors.New("unsafe control directory")
	}
	data, _, err := readOwned(dir, "session.json", uid)
	if err != nil {
		return nil, err
	}
	var r routeRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&r); err != nil {
		return nil, err
	}
	if err = r.validate(); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(r.ControlDirectory)
	if err != nil || canonical != dirPath {
		return nil, errors.New("control directory mismatch")
	}
	owner, err := unix.SysctlKinfoProc("kern.proc.pid", r.OwnerPID)
	if err != nil || owner.Eproc.Ucred.Uid != uid {
		return nil, errors.New("owner is gone")
	}
	for _, app := range r.AppPaths {
		resolved, e := filepath.EvalSymlinks(app)
		if e != nil || resolved != app {
			return nil, errors.New("application moved")
		}
		info, e := os.Stat(filepath.Join(app, "Contents"))
		if (e != nil || !info.IsDir()) && !r.LiveRouting {
			return nil, errors.New("invalid application bundle")
		}
	}
	c := &controlFiles{directory: dir, uid: uid, request: r, started: owner.Proc.P_starttime}
	if !c.alive() {
		return nil, errors.New("session expired")
	}
	if !createStatus {
		ok = true
		return c, nil
	}
	statusFD, err := unix.Openat(fd, "status", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	c.status = os.NewFile(uintptr(statusFD), "status")
	if os.Geteuid() == 0 {
		if err = c.status.Chown(int(uid), -1); err != nil {
			_ = c.status.Close()
			return nil, err
		}
	}
	ok = true
	return c, nil
}

func (c *controlFiles) alive() bool {
	data, st, err := readOwned(c.directory, "lease", c.uid)
	if err != nil || string(data) != c.request.Token || time.Since(st.ModTime()) > 5*time.Second || time.Until(st.ModTime()) > time.Second {
		return false
	}
	owner, err := unix.SysctlKinfoProc("kern.proc.pid", c.request.OwnerPID)
	return err == nil && owner.Eproc.Ucred.Uid == c.uid && owner.Proc.P_starttime == c.started
}
func (c *controlFiles) close() {
	if c.status != nil {
		_ = c.status.Close()
	}
	_ = c.directory.Close()
}

func runtimePath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(self)), nil
}
func chooseDevice() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for _, item := range interfaces {
		used[item.Name] = true
	}
	for i := 100; i < 1000; i++ {
		name := fmt.Sprintf("utun%d", i)
		if !used[name] {
			return name, nil
		}
	}
	return "", errors.New("no free interface")
}
func freePort() (uint16, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port), nil
}
func writeConfig(r routeRequest, runtime, device string, port uint16) (string, error) {
	dir, err := os.MkdirTemp("", "goconnect-routing-")
	if err != nil {
		return "", err
	}
	if r.LiveRouting {
		if err := writeLiveRules(dir, r.AppPaths); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	data, err := json.Marshal(makeConfig(r, runtime, device, port))
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}
func ready(client *http.Client, port uint16, token, device string) bool {
	iface, err := net.InterfaceByName(device)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return false
	}
	request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/configs", port), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var body struct {
		Tun struct {
			Enable bool `json:"enable"`
		} `json:"tun"`
	}
	return response.StatusCode == 200 && json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&body) == nil && body.Tun.Enable
}

func routeSession(ctx context.Context, path string) (resultErr error) {
	return routeSessionForOwner(ctx, path, 0, 0)
}

func routeSessionForOwner(ctx context.Context, path string, uid uint32, ownerPID int) (resultErr error) {
	if os.Geteuid() != 0 {
		return errors.New("administrator authorization required")
	}
	control, err := openControl(path)
	if err != nil {
		return err
	}
	defer control.close()
	if uid != 0 && (control.uid != uid || control.request.OwnerPID != ownerPID) {
		return errors.New("service session owner mismatch")
	}
	ctx, revoke := context.WithCancel(ctx)
	defer revoke()
	leaseWatchDone := make(chan struct{})
	defer close(leaseWatchDone)
	go func() {
		watch := time.NewTicker(200 * time.Millisecond)
		defer watch.Stop()
		for {
			select {
			case <-leaseWatchDone:
				return
			case <-watch.C:
				if !control.alive() {
					trace("route.owner_lost", 0)
					revoke()
					return
				}
			}
		}
	}()
	lock, err := captureLock()
	if err != nil {
		control.snapshot.Message = err.Error()
		control.write("failed")
		return err
	}
	defer lock.Close()
	trace("route.starting", 0)
	control.write("starting")
	result := false
	defer func() {
		if resultErr != nil {
			trace("route.failed", 1)
		} else {
			trace("route.stopped", 0)
		}
		if result {
			control.write("stopped")
		} else {
			if control.snapshot.Message == "" {
				control.snapshot.Message = "分流核心启动或运行失败，请检查管理员授权及其它 TUN。"
			}
			if resultErr != nil && strings.Contains(resultErr.Error(), "TUN did not start") {
				control.snapshot.Message = "未能创建 TUN 网络接口，请检查管理员权限或其它 VPN 冲突。"
			}
			if resultErr != nil && strings.Contains(resultErr.Error(), "controller unavailable") {
				control.snapshot.Message = "分流核心的状态读取已中断，正在释放本次 TUN。"
			}
			control.write("failed")
		}
	}()
	trace("route.environment_check", 0)
	runtime, err := runtimePath()
	if err != nil {
		return err
	}
	if err = validateCaptureEnvironment(); err != nil {
		control.snapshot.Message = err.Error()
		return err
	}
	if control.request.LiveRouting {
		control.request.systemLAN, err = discoverLocalNetworks()
		if err != nil {
			control.snapshot.Message = "无法识别本机局域网，未启用分流。"
			return err
		}
		control.request.systemDNS, err = discoverSystemDNS()
		if err != nil {
			control.snapshot.Message = "无法读取系统 DNS，未启用分流。"
			return err
		}
		control.apps, err = prepareLiveApps(control.request.AppPaths, control.request.ProbeOnly)
	} else {
		control.apps, err = prepareCapturedApps(control.uid, control.request.AppPaths, control.request.ProbeOnly)
	}
	if err != nil {
		control.snapshot.Message = err.Error()
		return err
	}
	if control.request.ProbeOnly && !control.request.LiveRouting {
		defer func() {
			for _, app := range control.apps {
				processes, e := processIdentities()
				if e != nil {
					continue
				}
				used := false
				for _, process := range processes {
					if process.GID == app.GID {
						used = true
					}
				}
				if !used {
					_, _ = systemCommand("/usr/sbin/dseditgroup", "-o", "delete", app.Group)
				}
			}
		}()
	}
	if !control.request.ProbeOnly && !control.request.LiveRouting {
		processes, e := processIdentities()
		if e != nil {
			return e
		}
		for _, app := range control.apps {
			_, unmanaged := appProcessCounts(app, control.uid, processes)
			if unmanaged > 0 {
				control.snapshot.Message = fmt.Sprintf("请先退出 %s，再连接；GoConnect 将为它启用专属线路。", strings.TrimSuffix(filepath.Base(app.Path), ".app"))
				return errors.New("unmanaged application is already running")
			}
		}
	}
	trace("route.network_discovery", 0)
	baseline, err := routeInterface(probeTarget)
	if err != nil {
		return err
	}
	tailscale, err := discoverTailscale()
	if err != nil {
		return errors.New("无法读取 Tailscale 网络路由，未修改网络。")
	}
	device, err := chooseDevice()
	if err != nil {
		return err
	}
	if control.request.hasSystemCapture() {
		control.request.systemDevice, err = chooseOtherDevice(device)
		if err != nil {
			return err
		}
		control.request.coreGID, err = unusedCoreGID()
		if err != nil {
			return err
		}
	}
	control.snapshot.SystemDevice = control.request.systemDevice
	control.snapshot.RemoteNetworks = control.request.RemoteNetworks
	control.snapshot.Device = device
	port, err := freePort()
	if err != nil {
		return err
	}
	dir, err := writeConfig(control.request, runtime, device, port)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if !control.alive() {
		return errors.New("session canceled")
	}
	cmd := exec.Command(filepath.Join(runtime, "bin", "mihomo"), "-d", dir, "-f", filepath.Join(dir, "config.json"))
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + dir}
	if control.request.hasSystemCapture() {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: control.request.coreGID, Groups: []uint32{}}}
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	trace("core.started", int64(cmd.Process.Pid))
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	running := true
	var filter *packetFilter
	defer func() {
		if filter != nil {
			var cleanupError error
			for attempt := 0; attempt < 3; attempt++ {
				trace("pf.cleanup_begin", int64(attempt))
				cleanupError = filter.close()
				if cleanupError != nil {
					trace("pf.cleanup_failed", int64(attempt))
				} else {
					trace("pf.cleanup_ok", int64(attempt))
				}
				if cleanupError == nil {
					break
				}
			}
			if cleanupError != nil {
				result = false
				resultErr = cleanupError
				control.snapshot.Message = "本次应用分流规则未能完全释放，请在诊断中检查。"
			}
		}
		if running {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-ended:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-ended
			}
		}
	}()
	// Avoid inherited proxy environment. The controller never leaves loopback.
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	deadline := time.Now().Add(12 * time.Second)
	reported := false
	nextSnapshot := time.Time{}
	nextNetworkCheck := time.Time{}
	nextDiagnostic := time.Time{}
	networkCheck := networkPoller{baseline: baseline, read: func() (routingNetworkState, error) { return readRoutingNetwork(control.request.LiveRouting) }}
	lastRules := ""
	missedSnapshots := 0
	for {
		select {
		case <-ctx.Done():
			trace("route.cancelled", 0)
			result = true
			return nil
		case <-ended:
			running = false
			trace("core.exited", int64(cmd.ProcessState.ExitCode()))
			return errors.New("routing process exited")
		case <-tick.C:
			if !control.alive() {
				result = true
				return nil
			}
			if !reported {
				if systemDeviceReady(control.request) && ready(client, port, control.request.Token, device) && (!control.request.LiveRouting || liveRulesReady(client, port, control.request.Token, len(liveRulePayload(control.request.AppPaths)))) {
					after, e := routeInterface(probeTarget)
					if e != nil || after != baseline {
						return errors.New("unexpected default route change")
					}
					trace("pf.install_begin", 0)
					filter, e = startPacketFilter(control.uid, control.apps, device, control.request.Token, control.request, tailscale)
					if e != nil {
						control.snapshot.Message = e.Error()
						return e
					}
					control.snapshot.CaptureMode = "app-group"
					if control.request.LiveRouting {
						control.snapshot.CaptureMode = "process-rules"
					}
					control.snapshot.RoutingMode = control.request.mode()
					control.snapshot.TailscaleActive = len(tailscale.Interfaces) > 0
					control.snapshot.TailscaleRoutes = len(tailscale.Prefixes)
					lastRules = routingRules(control.uid, control.apps, device, control.request, tailscale)
					control.snapshot.OriginalRoute = true
					// Connecting installs routing only. Apps launch exclusively through
					// an explicit per-app command, in both whitelist and global modes.
					reported = true
					trace("route.ready", 0)
					control.write("ready")
				} else if time.Now().After(deadline) {
					trace("core.start_timeout", 0)
					return errors.New("TUN did not start")
				}
			}
			if reported && time.Now().After(nextSnapshot) {
				if time.Now().After(nextDiagnostic) {
					trace("route.loop_alive", 0)
					nextDiagnostic = time.Now().Add(30 * time.Second)
				}
				if time.Now().After(nextNetworkCheck) {
					sample, e := networkCheck.poll()
					if e != nil {
						control.snapshot.Message = "网络巡检持续失败或原路由已变化，已停止本次分流。"
						return e
					}
					if sample != nil {
						fresh := sample.Tailscale
						fresh.Prefixes = mergePrefixes(tailscale.Prefixes, fresh.Prefixes)
						if len(fresh.Prefixes) > 1024 {
							return errors.New("too many Tailscale subnet routes")
						}
						tailscale = fresh
						control.request.systemLAN = sample.LAN
						control.request.systemDNS = sample.DNS
						rules := routingRules(control.uid, control.apps, device, control.request, tailscale)
						if rules != lastRules {
							trace("pf.network_update", 0)
							if e = filter.load(rules); e != nil {
								return e
							}
							lastRules = rules
						}
						control.snapshot.TailscaleActive = len(tailscale.Interfaces) > 0
						control.snapshot.TailscaleRoutes = len(tailscale.Prefixes)
					}
					nextNetworkCheck = time.Now().Add(2 * time.Second)
				}
				if control.request.LiveRouting {
					if err := control.handleLiveCommand(ctx, client, port, dir); err != nil {
						control.snapshot.Message = err.Error()
						return err
					}
				} else {
					control.handleCommand(ctx, runtime)
				}
				nextSnapshot = time.Now().Add(time.Second)
				connections, e := coreSnapshot(client, port, control.request.Token)
				_, interfaceError := net.InterfaceByName(device)
				if e != nil || interfaceError != nil || !systemDeviceReady(control.request) {
					missedSnapshots++
					trace("core.snapshot_failed", int64(missedSnapshots))
				} else {
					if missedSnapshots > 0 {
						trace("core.snapshot_recovered", 0)
					}
					missedSnapshots = 0
					control.snapshot = summarizeConnections(connections, control.request.AppPaths, control.snapshot)
					control.updateAppProcesses()
					control.write("ready")
				}
				if missedSnapshots >= 3 {
					return errors.New("controller unavailable")
				}
			}
		}
	}
}

// Non-elevated syntax validation only: -t never starts a TUN or changes routes.
func checkConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var r routeRequest
	if err = json.Unmarshal(data, &r); err != nil {
		return err
	}
	if err = r.validate(); err != nil {
		return err
	}
	runtime, err := runtimePath()
	if err != nil {
		return err
	}
	r.systemDevice = "utun101"
	dir, err := writeConfig(r, runtime, "utun100", 19091)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(filepath.Join(runtime, "bin", "mihomo"), "-t", "-d", dir, "-f", filepath.Join(dir, "config.json"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
