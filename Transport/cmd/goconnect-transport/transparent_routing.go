package main

// Transparent routing uses owned IP routes, never PF or per-app launch tags.
// The core's DIRECT adapter is pinned to the original physical interface;
// the VPN server has a specific bypass route before broad routes are installed.
import (
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
	"strings"
	"syscall"
	"time"
)

type originalNetwork struct{ Interface, Gateway string }
type transparentRoute struct {
	Prefix, Interface, Gateway string
	Scoped                     bool `json:"scoped,omitempty"`
}
type transparentState struct {
	Device string             `json:"device"`
	Routes []transparentRoute `json:"routes"`
	State  string             `json:"state"`
}

func originalPhysicalNetwork(probe bool) (originalNetwork, error) {
	read := func(args ...string) (originalNetwork, error) {
		b, e := systemCommand("/sbin/route", args...)
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
		if !physicalInterface.MatchString(n.Interface) || e != nil || !ip.Is4() {
			return originalNetwork{}, errors.New("当前公网由其它 VPN 接管，请先断开该 VPN，再连接 GoConnect。")
		}
		return n, nil
	}
	if !probe {
		return read("-n", "get", "-inet", "default")
	}
	// Isolated validation only: no default routes will be installed.
	ifaces, e := net.Interfaces()
	if e != nil {
		return originalNetwork{}, e
	}
	for _, i := range ifaces {
		if physicalInterface.MatchString(i.Name) && i.Flags&net.FlagUp != 0 {
			if n, e := read("-n", "get", "-inet", "-ifscope", i.Name, "default"); e == nil {
				return n, nil
			}
		}
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
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != gateway || f[1] != original.Gateway || f[3] != original.Interface {
			continue
		}
		flags := f[2]
		if strings.Contains(flags, "U") && strings.Contains(flags, "H") && strings.Contains(flags, "S") && !strings.ContainsAny(flags, "IW") {
			return true
		}
	}
	return false
}
func staticGatewayPresent(gateway string, original originalNetwork) bool {
	table, err := systemCommand("/usr/sbin/netstat", "-rn", "-f", "inet")
	return err == nil && staticGatewayInTable(table, gateway, original)
}
func gatewayUsesOriginal(gateway string, original originalNetwork) bool {
	b, err := systemCommand("/sbin/route", "-n", "get", "-inet", gateway)
	return err == nil && companyRouteInterface(b) == original.Interface
}
func pinTransparentGateway(path string, s *transparentState, gateway string, original originalNetwork) error {
	if !staticGatewayPresent(gateway, original) {
		if err := addTransparentRoute(path, s, transparentRoute{Prefix: gateway + "/32", Interface: original.Interface, Gateway: original.Gateway}); err != nil {
			return fmt.Errorf("无法固定 VPN 外层路径: %w", err)
		}
	}
	if !staticGatewayPresent(gateway, original) || !gatewayUsesOriginal(gateway, original) {
		return errors.New("VPN 网关静态绕行路由未生效")
	}
	return nil
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
		return append(args, "-host", strings.TrimSuffix(r.Prefix, "/32"), r.Gateway)
	}
	return append(args, "-net", r.Prefix, "-interface", r.Interface)
}
func scopedDefaultInTable(table []byte, original originalNetwork, gateway string) bool {
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "default" && f[1] == gateway && f[3] == original.Interface && strings.Contains(f[2], "I") && strings.Contains(f[2], "U") {
			return true
		}
	}
	return false
}
func originalIPv6Gateway(iface string) string {
	table, err := systemCommand("/usr/sbin/netstat", "-rn", "-f", "inet6")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "default" || f[3] != iface || strings.Contains(f[2], "I") {
			continue
		}
		a, e := netip.ParseAddr(f[1])
		if e == nil && a.Is6() && (a.Zone() == "" || a.Zone() == iface) {
			return f[1]
		}
	}
	return ""
}
func ensureTransparentDirectRoutes(path string, state *transparentState, original originalNetwork) error {
	routes := []transparentRoute{{Prefix: "0.0.0.0/0", Interface: original.Interface, Gateway: original.Gateway, Scoped: true}}
	if gateway := originalIPv6Gateway(original.Interface); gateway != "" {
		routes = append(routes, transparentRoute{Prefix: "::/0", Interface: original.Interface, Gateway: gateway, Scoped: true})
	}
	for _, r := range routes {
		family := "inet"
		if routeFamily(r.Prefix) == "-inet6" {
			family = "inet6"
		}
		table, err := systemCommand("/usr/sbin/netstat", "-rn", "-f", family)
		if err != nil {
			return err
		}
		if scopedDefaultInTable(table, original, r.Gateway) {
			continue
		} // preserve pre-existing system/VPN scoped routes
		if err = addTransparentRoute(path, state, r); err != nil {
			return fmt.Errorf("无法建立原网卡直连出口: %w", err)
		}
	}
	return nil
}

func addTransparentRoute(path string, s *transparentState, r transparentRoute) error {
	// Intent is journaled before mutation. Existing routes are never replaced.
	s.Routes = append(s.Routes, r)
	if e := saveTransparentState(path, *s); e != nil {
		return e
	}
	args := transparentRouteArguments("add", r)
	output, e := systemCommand("/sbin/route", args...)
	if e != nil && strings.Contains(string(output), "File exists") {
		// A concurrent owner won the route; do not delete its route on unwind.
		s.Routes = s.Routes[:len(s.Routes)-1]
		_ = saveTransparentState(path, *s)
	}
	return e
}
func cleanupTransparentRoutes(path string, s *transparentState) error {
	var failed []transparentRoute
	for i := len(s.Routes) - 1; i >= 0; i-- {
		r := s.Routes[i]
		p, e := netip.ParsePrefix(r.Prefix)
		if e != nil {
			return e
		}
		if r.Scoped {
			family := "inet"
			if routeFamily(r.Prefix) == "-inet6" {
				family = "inet6"
			}
			table, err := systemCommand("/usr/sbin/netstat", "-rn", "-f", family)
			if err != nil {
				failed = append(failed, r)
				continue
			}
			if !scopedDefaultInTable(table, originalNetwork{Interface: r.Interface}, r.Gateway) {
				continue
			}
		}
		query := []string{"-n", "get", routeFamily(r.Prefix)}
		if r.Scoped {
			query = append(query, "-ifscope", r.Interface)
		}
		query = append(query, p.Addr().String())
		b, e := systemCommand("/sbin/route", query...)
		if e != nil {
			failed = append(failed, r)
			continue
		}
		if companyRouteInterface(b) != r.Interface {
			continue
		}
		args := transparentRouteArguments("delete", r)
		if _, e = systemCommand("/sbin/route", args...); e != nil {
			failed = append(failed, r)
		}
	}
	s.Routes = failed
	s.State = "stopped"
	if len(failed) > 0 {
		s.State = "cleanupFailed"
	}
	if e := saveTransparentState(path, *s); e != nil {
		return e
	}
	if len(failed) > 0 {
		return errors.New("路由清理未完成，已暂停新连接")
	}
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
	defer func() {
		if result != nil {
			c.snapshot.Message = result.Error()
			c.write("failed")
		} else {
			c.write("stopped")
		}
	}()
	lock, e := captureLock()
	if e != nil {
		return e
	}
	defer lock.Close()
	for _, pattern := range []string{"transparent-*", "company-*"} {
		old, e := filepath.Glob(filepath.Join(serviceRunDirectory, pattern))
		if e != nil {
			return e
		}
		if len(old) > 0 {
			return errors.New("存在未清理的网络会话，已暂停重连；请检查诊断。")
		}
	}
	r := c.request
	if _, e = prepareLiveApps(r.AppPaths, false); e != nil {
		return e
	}
	original, e := originalPhysicalNetwork(r.ProbeOnly)
	if e != nil {
		return e
	}
	local, e := discoverLocalNetworks()
	if e != nil {
		return e
	}
	ts, e := discoverTailscale()
	if e != nil {
		return e
	}
	if len(r.RemoteNetworks) > 0 {
		if _, e = companyPlan(r.RemoteNetworks, local, ts); e != nil {
			return e
		}
		if e = validateCompanyGateway(r.RemoteNetworks, netip.MustParseAddr(r.Gateway)); e != nil {
			return e
		}
	}
	if e = validateTransparentAddresses(r.RemoteNetworks); e != nil {
		return e
	}
	device, e := chooseDevice()
	if e != nil {
		return e
	}
	port, e := freePort()
	if e != nil {
		return e
	}
	runtime, e := runtimePath()
	if e != nil {
		return e
	}
	if e = secureRootPath(filepath.Join(runtime, "bin/mihomo"), false); e != nil {
		return e
	}
	dir, e := os.MkdirTemp(serviceRunDirectory, "transparent-")
	if e != nil {
		return e
	}
	statePath := filepath.Join(dir, "state.json")
	state := transparentState{Device: device, State: "starting"}
	if e = saveTransparentState(statePath, state); e != nil {
		return e
	}
	var child *exec.Cmd
	var done chan error
	defer func() {
		// Restore the network before waiting for a potentially blocked core. Retain
		// a quarantine record if either cleanup or child termination fails.
		cleanupErr := cleanupTransparentRoutes(statePath, &state)
		var stopErr error
		if child != nil {
			stopErr = stopChild(child, done)
		}
		if cleanupErr != nil {
			result = cleanupErr
		} else if stopErr != nil {
			result = stopErr
		} else {
			_ = os.RemoveAll(dir)
		}
	}()
	config, e := json.Marshal(makeTransparentConfig(r, device, port, original, local, ts))
	if e != nil {
		return e
	}
	file := filepath.Join(dir, "config.json")
	if e = os.WriteFile(file, config, 0600); e != nil {
		return e
	}
	cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return e
	}
	child = cmd
	done = make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	client := &http.Client{Timeout: time.Second}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	start := time.Now()
	active := false
	lastHealth := time.Time{}
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-done:
			done <- e
			return errors.New("透明分流核心已退出")
		case <-tick.C:
			if !c.alive() {
				return nil
			}
			if !active {
				if !ready(client, port, r.Token, device) {
					if time.Since(start) > 15*time.Second {
						return errors.New("透明分流入口启动超时")
					}
					continue
				}
				if !r.ProbeOnly {
					current, e := originalPhysicalNetwork(false)
					if e != nil || current != original {
						return errors.New("原网络已改变，请重新连接")
					}
					// Keep the tunnel's outer connection on its original physical path.
					gw := netip.MustParseAddr(r.Gateway)
					if !gw.Is4() {
						return errors.New("当前透明连接要求 IPv4 VPN 网关")
					}
					b, e := systemCommand("/sbin/route", "-n", "get", r.Gateway)
					if e != nil {
						return e
					}
					if companyRouteInterface(b) != original.Interface {
						return errors.New("VPN 服务器未使用原网络，请先结束冲突的 VPN")
					}
					if e = pinTransparentGateway(statePath, &state, r.Gateway, original); e != nil {
						return e
					}
				} else {
					// Documentation-only address: verifies the outer gateway
					// remains pinned even when its enclosing prefix enters TUN.
					if e = pinTransparentGateway(statePath, &state, "203.0.113.5", original); e != nil {
						return e
					}
				}
				if e = ensureTransparentDirectRoutes(statePath, &state, original); e != nil {
					return e
				}
				for _, prefix := range transparentCapturePrefixes(r.ProbeOnly) {
					if e = addTransparentRoute(statePath, &state, transparentRoute{Prefix: prefix, Interface: device}); e != nil {
						return fmt.Errorf("无法添加透明路由 %s: %w", prefix, e)
					}
				}
				outer := r.Gateway
				if r.ProbeOnly {
					outer = "203.0.113.5"
				}
				if !staticGatewayPresent(outer, original) || !gatewayUsesOriginal(outer, original) {
					return errors.New("接管路由后 VPN 网关绕行失效，已停止连接")
				}
				active = true
				state.State = "ready"
				if e = saveTransparentState(statePath, state); e != nil {
					return e
				}
				c.snapshot.SystemDevice = device
				c.snapshot.RemoteNetworks = r.RemoteNetworks
				c.snapshot.TailscaleActive = len(ts.Interfaces) > 0
				c.snapshot.TailscaleRoutes = len(ts.Prefixes)
				c.snapshot.Message = "透明 App 分流已启用，无需应用代理设置。"
				c.write("ready")
			} else if time.Since(lastHealth) > 5*time.Second {
				lastHealth = time.Now()
				if !r.ProbeOnly {
					now, e := originalPhysicalNetwork(true)
					if e != nil || now != original {
						return errors.New("原网络已改变，已停止本次连接；请重新连接")
					}
					b, e := systemCommand("/sbin/route", "-n", "get", r.Gateway)
					if e != nil || companyRouteInterface(b) != original.Interface {
						return errors.New("VPN 外层路径改变，已停止连接以避免回流")
					}
				}
				if ready(client, port, r.Token, device) {
					failures = 0
				} else {
					failures++
					trace("transparent.health_failed", int64(failures))
				}
				if failures >= 3 {
					return errors.New("分流核心连续未响应，已恢复原网络并停止本次连接")
				}
			}
			if active {
				// Poll only metadata and counts; leave forwarding and route ownership unchanged.
				_ = c.refreshConnections(client, port)
			}
		}
	}
}
