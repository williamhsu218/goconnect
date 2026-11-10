package main

import (
	"bytes"
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
	Company            *companyReservation `json:"company,omitempty"`
	TransparentRouting bool                `json:"transparentRouting,omitempty"`
	CompanyShared      bool                `json:"companyShared,omitempty"`
	VPNAddresses       []string            `json:"vpnAddresses,omitempty"`
	// Root-discovered resolver addresses; never accepted from session JSON.
	systemDevice     string
	coreGID          uint32
	processOwnerUID  uint32   // Authenticated service peer, never accepted from JSON.
	RemoteNetworks   []string `json:"remoteNetworks,omitempty"`
	RemoteExcluded   []string `json:"remoteExcluded,omitempty"`
	systemLAN        []localNetworkRoute
	systemDNS        []netip.Addr
	managedDNS       *managedDNSPlan
	LiveRouting      bool         `json:"liveRouting,omitempty"`
	DirectDomains    []domainRule `json:"directDomains,omitempty"`
	OwnerPID         int          `json:"ownerPID"`
	ControlDirectory string       `json:"controlDirectory"`
	Token            string       `json:"token"`
	SOCKSPort        uint16       `json:"socksPort"`
	AppPaths         []string     `json:"appPaths"`
	DirectAppPaths   []string     `json:"directAppPaths,omitempty"`
	Gateway          string       `json:"gateway"`
	ProbeOnly        bool         `json:"probeOnly,omitempty"`
	RoutingMode      string       `json:"routingMode,omitempty"`
}

func (r routeRequest) validate() error {
	if len(r.DirectAppPaths) > 256 || (len(r.DirectAppPaths) > 0 && (!r.TransparentRouting || r.mode() != modeWhitelist || r.CompanyShared)) {
		return errors.New("invalid direct application scope")
	}
	for _, path := range r.DirectAppPaths {
		if !validRoutingTargetPath(path, true) {
			return errors.New("invalid direct application path")
		}
	}
	if r.Company != nil {
		if !r.TransparentRouting || r.ProbeOnly || r.CompanyShared || len(r.RemoteNetworks) > 0 {
			return errors.New("invalid company reservation mode")
		}
		if err := r.Company.validate(); err != nil {
			return err
		}
	}
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
	reason, _ := c.leaseDiagnostics()
	return reason.Reason == ""
}

// Renewed every second on a dedicated queue. A bounded scheduling allowance
// does not weaken explicit revocation or owner UID/start-time verification.
const leaseTimeout = 30 * time.Second

func (c *controlFiles) leaseDiagnostics() (diagnosticDetails, error) {
	d := diagnosticDetails{Stage: "lease", TimeoutMS: leaseTimeout.Milliseconds()}
	data, st, err := readOwned(c.directory, "lease", c.uid)
	if err != nil {
		d.Reason = "lease_unreadable"
		if os.IsNotExist(err) {
			d.Reason = "lease_missing"
		}
		return d, err
	}
	if string(data) != c.request.Token {
		d.Reason = "lease_token_mismatch"
		return d, nil
	}
	age := time.Since(st.ModTime())
	d.DurationMS = age.Milliseconds()
	if age < -time.Second {
		d.Reason = "lease_future"
		return d, nil
	}
	owner, err := unix.SysctlKinfoProc("kern.proc.pid", c.request.OwnerPID)
	if err != nil {
		d.Reason = "owner_missing"
		return d, err
	}
	if owner.Eproc.Ucred.Uid != c.uid || owner.Proc.P_starttime != c.started {
		d.Reason = "owner_changed"
		return d, nil
	}
	if age > leaseTimeout {
		d.Reason = "lease_expired"
		return d, nil
	}
	return diagnosticDetails{}, nil
}

// Used directly by defer so a panic cannot publish a successful stop.
func (c *controlFiles) finishSession(result *error) {
	value := recover()
	if value != nil {
		*result = errors.New("分流会话异常结束，请导出诊断。")
	}
	if *result != nil {
		c.snapshot.Message = (*result).Error()
		c.write("failed")
	} else {
		c.write("stopped")
	}
	if value != nil {
		panic(value)
	}
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
	ok, _ := coreReadiness(client, port, token, device)
	return ok
}

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
