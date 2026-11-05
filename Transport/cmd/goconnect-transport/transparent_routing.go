package main

// Transparent routing uses owned IP routes, never PF or per-app launch tags.
// The core's DIRECT adapter is pinned to the original physical interface;
// the VPN server has a specific bypass route before broad routes are installed.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type originalNetwork struct{ Interface, Gateway string }

var errPhysicalNetwork = errors.New("当前公网由其它 VPN 接管或网关无效，请检查原网络。")

type transparentRoute struct {
	Prefix, Interface, Gateway string
	Scoped                     bool `json:"scoped,omitempty"`
	Pending                    bool `json:"pending,omitempty"`
}
type transparentState struct {
	Device  string             `json:"device"`
	Routes  []transparentRoute `json:"routes"`
	State   string             `json:"state"`
	CorePID int                `json:"corePID,omitempty"`
}

func originalPhysicalNetwork(probe bool) (originalNetwork, error) {
	return originalPhysicalNetworkWith(probe, net.Interfaces, systemCommand)
}
func originalPhysicalNetworkWith(probe bool, interfaces func() ([]net.Interface, error), command func(string, ...string) ([]byte, error)) (originalNetwork, error) {
	read := func(args ...string) (originalNetwork, error) {
		b, e := command("/sbin/route", args...)
		if e != nil {
			return originalNetwork{}, e
		}
		n := originalNetwork{Interface: companyRouteInterface(b)}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "gateway:" {
				n.Gateway = f[1]
			}
		}
		ip, e := netip.ParseAddr(n.Gateway)
		if !physicalInterface.MatchString(n.Interface) || e != nil || ip.IsUnspecified() || (ip.Zone() != "" && ip.Zone() != n.Interface) {
			return originalNetwork{}, errPhysicalNetwork
		}
		return n, nil
	}
	if !probe {
		n, err := read("-n", "get", "-inet", "default")
		if err == nil || errors.Is(err, errPhysicalNetwork) || diagnosticError(err).Reason == "system_timeout" {
			return n, err
		}
		return read("-n", "get", "-inet6", "default")
	}
	// Isolated validation only: no default routes will be installed.
	ifaces, e := interfaces()
	if e != nil {
		return originalNetwork{}, e
	}
	var lastFailure error
	for _, i := range ifaces {
		if physicalInterface.MatchString(i.Name) && i.Flags&net.FlagUp != 0 {
			if n, e := read("-n", "get", "-inet", "-ifscope", i.Name, "default"); e == nil {
				return n, nil
			} else if diagnosticError(lastFailure).Reason != "system_timeout" {
				lastFailure = e
			}
		}
	}
	// IPv6-only physical networks have no IPv4 default. Preserve a timeout as
	// the primary failure instead of hiding it behind another family lookup.
	if diagnosticError(lastFailure).Reason != "system_timeout" {
		for _, i := range ifaces {
			if physicalInterface.MatchString(i.Name) && i.Flags&net.FlagUp != 0 {
				if n, e := read("-n", "get", "-inet6", "-ifscope", i.Name, "default"); e == nil {
					return n, nil
				} else {
					lastFailure = e
				}
			}
		}
	}
	if lastFailure != nil {
		return originalNetwork{}, fmt.Errorf("找不到原网络接口: %w", lastFailure)
	}
	return originalNetwork{}, errors.New("找不到原网络接口")
}

func transparentCapturePrefixes(probe bool) []string {
	if probe {
		return []string{"1.1.1.1/32", "203.0.113.0/24"}
	}
	return []string{"0.0.0.0/1", "128.0.0.0/1", "::/1", "8000::/1"}
}

func makeTransparentConfig(r routeRequest, device string, port uint16, original originalNetwork, local []localNetworkRoute, ts tailscaleBypass) map[string]any {
	c := makeSharedCompanyConfig(r, device, port)
	c["find-process-mode"] = "strict"
	// Explicit interface binding prevents DIRECT traffic entering our own TUN.
	c["proxies"] = []map[string]any{
		{"name": "GoConnect", "type": "socks5", "server": "127.0.0.1", "port": r.SOCKSPort, "username": "goconnect", "password": r.Token, "udp": true},
		{"name": "OriginalNetwork", "type": "direct", "interface-name": original.Interface},
	}
	rules := []string{"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR6,::1/128,DIRECT,no-resolve", "DOMAIN-SUFFIX,local,OriginalNetwork"}
	// More-specific system routes normally bypass the TUN entirely. Preserve
	// their interfaces too for scoped sockets which nevertheless enter it.
	destinations := map[string]string{}
	proxies := c["proxies"].([]map[string]any)
	addScope := func(prefix netip.Prefix, iface string) {
		name, ok := destinations[iface]
		if !ok {
			name = fmt.Sprintf("Local%d", len(destinations))
			destinations[iface] = name
			proxies = append(proxies, map[string]any{"name": name, "type": "direct", "interface-name": iface})
		}
		kind := "IP-CIDR"
		if prefix.Addr().Is6() {
			kind = "IP-CIDR6"
		}
		rules = append(rules, kind+","+prefix.String()+","+name+",no-resolve")
	}
	// Tailscale's existing OS routes are never replaced by private catch-all rules.
	for _, p := range ts.Prefixes {
		if len(ts.Interfaces) == 1 {
			addScope(p, ts.Interfaces[0])
			continue
		}
		kind := "IP-CIDR"
		if p.Addr().Is6() {
			kind = "IP-CIDR6"
		}
		rules = append(rules, kind+","+p.String()+",REJECT,no-resolve")
	}
	for _, l := range local {
		addScope(l.Prefix, l.Interface)
	}
	for _, rule := range directDomainRules(r.DirectDomains) {
		rules = append(rules, strings.ReplaceAll(rule, ",DIRECT", ",OriginalNetwork"))
	}
	for _, value := range r.RemoteNetworks {
		p, _ := remotePrefix(value)
		kind := "IP-CIDR"
		if p.Addr().Is6() {
			kind = "IP-CIDR6"
		}
		rules = append(rules, kind+","+value+",GoConnect,no-resolve")
	}
	target, fallback := "GoConnect", "OriginalNetwork"
	if r.mode() == modeGlobal {
		target, fallback = "OriginalNetwork", "GoConnect"
	}
	for _, path := range r.AppPaths {
		rules = append(rules, processPathRule(path, target))
	}
	rules = append(rules, "MATCH,"+fallback)
	c["rules"] = rules
	c["proxies"] = proxies
	// Recover a hostname for IPv4-only VPN egress and domain exceptions; never
	// decrypt TLS. Unrecoverable IP-only requests retain their destination IP.
	c["sniffer"] = map[string]any{"enable": true, "parse-pure-ip": true, "force-dns-mapping": true, "override-destination": true, "sniff": map[string]any{"HTTP": map[string]any{"ports": []string{"1-65535"}}, "TLS": map[string]any{"ports": []string{"1-65535"}}, "QUIC": map[string]any{"ports": []int{443, 8443}}}}
	return c
}

// A scoped cached lookup may shadow the static entry in `route get`. Verify
// ownership in the routing table and forwarding interface independently.
func staticGatewayInTable(table []byte, gateway string, original originalNetwork) bool {
	address, err := netip.ParseAddr(gateway)
	if err != nil {
		return false
	}
	want := netip.PrefixFrom(address, address.BitLen())
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !sameRouteGateway(f[1], original.Gateway) || f[3] != original.Interface {
			continue
		}
		prefix, ok := parseRoutePrefix(f[0])
		if !ok || prefix != want {
			continue
		}
		flags := f[2]
		if strings.Contains(flags, "U") && strings.Contains(flags, "H") && strings.Contains(flags, "S") && !strings.ContainsAny(flags, "IW") {
			return true
		}
	}
	return false
}

// Compare numeric addresses while retaining the IPv6 zone as part of ownership.
func sameRouteGateway(actual, expected string) bool {
	a, err := netip.ParseAddr(actual)
	b, otherErr := netip.ParseAddr(expected)
	return err == nil && otherErr == nil && a == b
}

func physicalDefaultGatewayInTable(table []byte, original originalNetwork) bool {
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "default" || f[3] != original.Interface {
			continue
		}
		// Our own scoped default can retain the old router after the physical
		// IPv6 default changes. It cannot serve as evidence of that router's health.
		flags := f[2]
		if strings.Contains(flags, "U") && strings.Contains(flags, "G") && !strings.ContainsAny(flags, "IWLY") && sameRouteGateway(f[1], original.Gateway) {
			return true
		}
	}
	return false
}

func gatewayRouteInterfaceWith(gateway string, command func(string, ...string) ([]byte, error)) (string, error) {
	b, err := command("/sbin/route", "-n", "get", "-"+gatewayTableFamily(gateway), gateway)
	return companyRouteInterface(b), err
}
func staticGatewayPresent(gateway string, original originalNetwork) bool {
	table, err := systemCommand("/usr/sbin/netstat", "-rn", "-f", gatewayTableFamily(gateway))
	return err == nil && staticGatewayInTable(table, gateway, original)
}

// Preserve command failures across the combined boolean check. Otherwise a
// command timeout is indistinguishable from a successfully observed route change.
func transparentGatewayReady(gateway string, original originalNetwork) (bool, error) {
	return transparentGatewayReadyWith(gateway, original, systemCommand)
}

func transparentGatewayReadyWith(gateway string, original originalNetwork, command func(string, ...string) ([]byte, error)) (bool, error) {
	table, err := command("/usr/sbin/netstat", "-rn", "-f", gatewayTableFamily(gateway))
	if err != nil || !staticGatewayInTable(table, gateway, original) || !physicalDefaultGatewayInTable(table, original) {
		return false, err
	}
	actual, err := gatewayRouteInterfaceWith(gateway, command)
	return err == nil && actual == original.Interface, err
}
func pinTransparentGateway(path string, s *transparentState, gateway string, original originalNetwork) error {
	return pinTransparentGatewayWith(path, s, gateway, original, systemCommand)
}
func pinTransparentGatewayWith(path string, s *transparentState, gateway string, original originalNetwork, command func(string, ...string) ([]byte, error)) error {
	table, err := command("/usr/sbin/netstat", "-rn", "-f", gatewayTableFamily(gateway))
	if err != nil {
		return err
	}
	if !staticGatewayInTable(table, gateway, original) {
		address := netip.MustParseAddr(gateway)
		if err := addTransparentRouteWith(path, s, transparentRoute{Prefix: netip.PrefixFrom(address, address.BitLen()).String(), Interface: original.Interface, Gateway: original.Gateway}, command); err != nil {
			return fmt.Errorf("无法固定 VPN 外层路径: %w", err)
		}
	}
	if ok, err := transparentGatewayReadyWith(gateway, original, command); !ok {
		if err != nil {
			return fmt.Errorf("VPN 网关静态绕行检查失败: %w", err)
		}
		return errors.New("VPN 网关静态绕行路由未生效")
	}
	return nil
}

func gatewayTableFamily(gateway string) string {
	if a, err := netip.ParseAddr(gateway); err == nil && a.Is6() {
		return "inet6"
	}
	return "inet"
}

func gatewayPhysicalNetwork(gateway string, original originalNetwork, command func(string, ...string) ([]byte, error)) (originalNetwork, error) {
	if gatewayTableFamily(gateway) == gatewayTableFamily(original.Gateway) {
		return original, nil
	}
	b, err := command("/sbin/route", "-n", "get", "-"+gatewayTableFamily(gateway), "-ifscope", original.Interface, "default")
	if err != nil {
		return originalNetwork{}, err
	}
	n := originalNetwork{Interface: companyRouteInterface(b)}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "gateway:" {
			n.Gateway = f[1]
		}
	}
	a, err := netip.ParseAddr(n.Gateway)
	if err != nil || n.Interface != original.Interface || a.IsUnspecified() || gatewayTableFamily(gateway) != gatewayTableFamily(n.Gateway) || (a.Zone() != "" && a.Zone() != n.Interface) {
		return originalNetwork{}, errPhysicalNetwork
	}
	return n, nil
}

func saveTransparentState(path string, s transparentState) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if e = os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(path+".tmp", path)
}
func routeFamily(prefix string) string {
	p, _ := netip.ParsePrefix(prefix)
	if p.Addr().Is6() {
		return "-inet6"
	}
	return "-inet"
}
func transparentRouteArguments(action string, r transparentRoute) []string {
	args := []string{"-n", action, routeFamily(r.Prefix)}
	if r.Scoped {
		return append(args, "-ifscope", r.Interface, "-net", r.Prefix, r.Gateway)
	}
	if r.Gateway != "" {
		prefix := netip.MustParsePrefix(r.Prefix)
		return append(args, "-host", prefix.Addr().String(), r.Gateway)
	}
	return append(args, "-net", r.Prefix, "-interface", r.Interface)
}
func scopedDefaultInTable(table []byte, original originalNetwork, gateway string) bool {
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "default" && sameRouteGateway(f[1], gateway) && f[3] == original.Interface && strings.Contains(f[2], "I") && strings.Contains(f[2], "U") {
			return true
		}
	}
	return false
}
func originalIPv6Gateway(iface string, command func(string, ...string) ([]byte, error)) (string, error) {
	table, err := command("/usr/sbin/netstat", "-rn", "-f", "inet6")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "default" || f[3] != iface || !strings.Contains(f[2], "U") || !strings.Contains(f[2], "G") || strings.ContainsAny(f[2], "IWLY") {
			continue
		}
		a, e := netip.ParseAddr(f[1])
		if e == nil && a.Is6() && (a.Zone() == "" || a.Zone() == iface) {
			return f[1], nil
		}
	}
	return "", nil
}
func ensureTransparentDirectRoutes(path string, state *transparentState, original originalNetwork) error {
	return ensureTransparentDirectRoutesWith(path, state, original, systemCommand)
}
func transparentDirectRoutes(original originalNetwork, command func(string, ...string) ([]byte, error)) ([]transparentRoute, error) {
	prefix := "0.0.0.0/0"
	if gatewayTableFamily(original.Gateway) == "inet6" {
		prefix = "::/0"
	}
	routes := []transparentRoute{{Prefix: prefix, Interface: original.Interface, Gateway: original.Gateway, Scoped: true}}
	if prefix != "::/0" {
		gateway, err := originalIPv6Gateway(original.Interface, command)
		if err != nil {
			return nil, err
		}
		if gateway != "" {
			routes = append(routes, transparentRoute{Prefix: "::/0", Interface: original.Interface, Gateway: gateway, Scoped: true})
		}
	}
	return routes, nil
}
func ensureTransparentDirectRoutesWith(path string, state *transparentState, original originalNetwork, command func(string, ...string) ([]byte, error)) error {
	routes, err := transparentDirectRoutes(original, command)
	if err != nil {
		return err
	}
	for _, r := range routes {
		family := "inet"
		if routeFamily(r.Prefix) == "-inet6" {
			family = "inet6"
		}
		table, err := command("/usr/sbin/netstat", "-rn", "-f", family)
		if err != nil {
			return err
		}
		if scopedDefaultInTable(table, original, r.Gateway) {
			continue
		} // preserve pre-existing system/VPN scoped routes
		if err = addTransparentRouteWith(path, state, r, command); err != nil {
			return fmt.Errorf("无法建立原网卡直连出口: %w", err)
		}
	}
	return nil
}

func addTransparentRoute(path string, s *transparentState, r transparentRoute) error {
	return addTransparentRouteWith(path, s, r, systemCommand)
}
func addTransparentRouteWith(path string, s *transparentState, r transparentRoute, command func(string, ...string) ([]byte, error)) error {
	return addTransparentRouteWithJournal(s, r, command, func(state transparentState) error { return saveTransparentState(path, state) })
}
func addTransparentRouteWithJournal(s *transparentState, r transparentRoute, command func(string, ...string) ([]byte, error), save func(transparentState) error) error {
	family := strings.TrimPrefix(routeFamily(r.Prefix), "-")
	table, err := command("/usr/sbin/netstat", "-rn", "-f", family)
	if err != nil {
		return err
	}
	if routeDestinationInTable(table, r) {
		return fmt.Errorf("路由已存在，未取得所有权: %w", syscall.EEXIST)
	}
	// Pending intents do not establish ownership; retain them after an unknown
	// or late result instead of letting recovery delete a concurrent owner's route.
	r.Pending = true
	s.Routes = append(s.Routes, r)
	if e := save(*s); e != nil {
		s.Routes = s.Routes[:len(s.Routes)-1]
		return e
	}
	args := transparentRouteArguments("add", r)
	output, e := command("/sbin/route", args...)
	// Darwin route can print a failed mutation while still exiting zero.
	if strings.Contains(string(output), ": File exists") || errors.Is(e, syscall.EEXIST) {
		s.Routes = s.Routes[:len(s.Routes)-1]
		return errors.Join(fmt.Errorf("路由冲突，未取得所有权: %w", syscall.EEXIST), save(*s))
	}
	if errors.Is(e, context.DeadlineExceeded) || diagnosticError(e).Reason == "system_timeout" {
		return e
	}
	table, queryErr := command("/usr/sbin/netstat", "-rn", "-f", family)
	if queryErr != nil {
		return errors.Join(e, queryErr)
	}
	if !ownedTransparentRouteInTable(table, r) {
		s.Routes = s.Routes[:len(s.Routes)-1]
		return errors.Join(e, errors.New("新增路由未通过精确查表验证"), save(*s))
	}
	if e != nil {
		return e
	}
	s.Routes[len(s.Routes)-1].Pending = false
	return save(*s)
}
func cleanupTransparentRoutes(path string, s *transparentState) error {
	return cleanupTransparentRoutesWith(path, s, systemCommand)
}

func cleanupTransparentRoutesWith(path string, s *transparentState, command func(string, ...string) ([]byte, error)) error {
	return cleanupTransparentRoutesWithDiagnostics(path, s, command, traceDetails)
}
func cleanupTransparentRoutesWithDiagnostics(path string, s *transparentState, command func(string, ...string) ([]byte, error), record func(string, int64, diagnosticDetails)) error {
	trace("transparent.cleanup_started", int64(len(s.Routes)))
	failure := func(reason string, index int, route transparentRoute, err error) {
		d := diagnosticError(err)
		d.Check = d.Reason
		d.Stage = "cleanup"
		d.Reason = reason
		d.RouteIndex = &index
		d.Interface = route.Interface
		d.Scoped = &route.Scoped
		if prefix, e := netip.ParsePrefix(route.Prefix); e == nil {
			d.Family = 6
			if prefix.Addr().Is4() {
				d.Family = 4
			}
			n := prefix.Bits()
			d.PrefixBits = &n
		}
		record("transparent.route_cleanup_failed", int64(index), d)
	}
	var failed []transparentRoute
	for i := len(s.Routes) - 1; i >= 0; i-- {
		r := s.Routes[i]
		if r.Pending {
			failure("route_add_incomplete", i, r, nil)
			failed = append(failed, r)
			continue
		}
		if _, e := netip.ParsePrefix(r.Prefix); e != nil {
			failure("invalid_route", i, r, e)
			return e
		}
		family := strings.TrimPrefix(routeFamily(r.Prefix), "-")
		table, e := command("/usr/sbin/netstat", "-rn", "-f", family)
		if e != nil {
			failure("read_failed", i, r, e)
			failed = append(failed, r)
			continue
		}
		if !ownedTransparentRouteInTable(table, r) {
			continue
		}
		_, e = command("/sbin/route", transparentRouteArguments("delete", r)...)
		{
			table, queryErr := command("/usr/sbin/netstat", "-rn", "-f", family)
			if queryErr != nil {
				if e != nil {
					failure("delete_failed", i, r, e)
				}
				failure("recheck_failed", i, r, queryErr)
				failed = append(failed, r)
			} else if ownedTransparentRouteInTable(table, r) {
				failure("delete_still_present", i, r, e)
				failed = append(failed, r)
			}
		}
	}
	for i, j := 0, len(failed)-1; i < j; i, j = i+1, j-1 {
		failed[i], failed[j] = failed[j], failed[i]
	}
	s.Routes = failed
	s.State = "stopped"
	if len(failed) > 0 {
		s.State = "cleanupFailed"
	}
	if e := saveTransparentState(path, *s); e != nil {
		d := diagnosticError(e)
		d.Stage = "cleanup"
		d.Reason = "journal_write_failed"
		record("transparent.cleanup_journal_failed", int64(len(failed)), d)
		return e
	}
	if len(failed) > 0 {
		trace("transparent.cleanup_failed", int64(len(failed)))
		return errors.New("路由清理未完成，已暂停新连接")
	}
	trace("transparent.cleanup_ok", 0)
	return nil
}

func validateTransparentAddresses(remote []string) error {
	reserved := []netip.Prefix{netip.MustParsePrefix(systemCaptureAddress + "/30").Masked(), netip.MustParsePrefix(systemCaptureIPv6 + "/126").Masked()}
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	var prefixes []netip.Prefix
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return err
		}
		for _, addr := range addrs {
			if p, err := netip.ParsePrefix(addr.String()); err == nil {
				prefixes = append(prefixes, p)
			}
		}
	}
	for _, value := range remote {
		p, ok := remotePrefix(value)
		if !ok {
			return errors.New("invalid remote prefix")
		}
		prefixes = append(prefixes, p)
	}
	for _, p := range prefixes {
		for _, r := range reserved {
			if r.Overlaps(p) {
				return errors.New("本机或公司网段与隧道地址冲突，请调整后重试。")
			}
		}
	}
	return nil
}

func transparentSession(ctx context.Context, path string, uid uint32, pid int) (result error) {
	exitLog := newSessionExitDiagnostics()
	defer exitLog.complete(&result)
	c, e := openControl(path)
	if e != nil {
		return e
	}
	defer c.close()
	if c.uid != uid || c.request.OwnerPID != pid || !c.request.TransparentRouting {
		return errors.New("invalid transparent owner")
	}
	c.snapshot.CaptureMode = "transparent-routes"
	c.snapshot.RoutingMode = c.request.mode()
	c.write("starting")
	defer c.finishSession(&result)
	exitLog.stage = "lock"
	lock, e := captureLock()
	if e != nil {
		return e
	}
	defer lock.Close()
	exitLog.stage = "recovery"
	if e = recoverAbandonedSessions(); e != nil {
		return e
	}
	r := c.request
	exitLog.stage = "apps"
	if _, e = prepareLiveApps(r.AppPaths, false); e != nil {
		return e
	}
	exitLog.stage = "network"
	original, e := originalPhysicalNetwork(r.ProbeOnly)
	if e != nil {
		return e
	}
	outerOriginal := original
	if !r.ProbeOnly {
		outerOriginal, e = gatewayPhysicalNetwork(r.Gateway, original, systemCommand)
		if e != nil {
			return e
		}
	}
	exitLog.stage = "local"
	local, e := discoverLocalNetworks()
	if e != nil {
		return e
	}
	exitLog.stage = "tailscale"
	ts, e := discoverTailscale()
	if e != nil {
		return e
	}
	exitLog.stage = "company"
	if len(r.RemoteNetworks) > 0 {
		if _, e = companyPlan(r.RemoteNetworks, local, ts); e != nil {
			return e
		}
		if e = validateCompanyGateway(r.RemoteNetworks, netip.MustParseAddr(r.Gateway)); e != nil {
			return e
		}
	}
	exitLog.stage = "address"
	if e = validateTransparentAddresses(r.RemoteNetworks); e != nil {
		return e
	}
	exitLog.stage = "device"
	device, e := chooseDevice()
	if e != nil {
		return e
	}
	exitLog.stage = "port"
	port, e := freePort()
	if e != nil {
		return e
	}
	exitLog.stage = "runtime"
	runtime, e := runtimePath()
	if e != nil {
		return e
	}
	if e = secureRootPath(filepath.Join(runtime, "bin/mihomo"), false); e != nil {
		return e
	}
	exitLog.stage = "journal"
	state := transparentState{Device: device, State: "starting"}
	dir, e := initializeSessionDirectory(serviceRunDirectory, "transparent-", func(path string) error { return saveTransparentState(path, state) })
	if e != nil {
		return e
	}
	statePath := filepath.Join(dir, "state.json")
	connection := transparentConnection{control: c, port: port, original: original, outer: outerOriginal, device: device, statePath: statePath, state: &state, runtime: runtime, local: local, tails: ts, exitLog: exitLog, ops: defaultTransparentRuntime()}
	return connection.run(ctx)
}
