package main

// Narrow company routes sharing the existing authenticated OpenConnect SOCKS session.
// No PF, process classification, system proxy, global DNS or default route changes.
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

type companyState struct {
	Device    string   `json:"device"`
	Networks  []string `json:"networks"`
	Added     []string `json:"added"`
	Addresses []string `json:"addresses"`
	State     string   `json:"state"`
}

func companyPlan(networks []string, local []localNetworkRoute, ts tailscaleBypass) ([]string, error) {
	if len(networks) == 0 || len(networks) > 256 {
		return nil, errors.New("请明确填写公司网段，不自动接管所有私网。")
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range networks {
		p, ok := remotePrefix(s)
		if !ok {
			return nil, errors.New("公司网段必须是明确的私有地址或 CIDR")
		}
		for _, r := range local {
			if p.Overlaps(r.Prefix) {
				return nil, errors.New("公司网段与本地网络重叠，请缩小范围或调整网段。")
			}
		}
		for _, r := range ts.Prefixes {
			if p.Overlaps(r) {
				return nil, errors.New("公司网段与 Tailscale 路由重叠，未改变网络。")
			}
		}
		if !seen[p.String()] {
			out = append(out, p.String())
			seen[p.String()] = true
		}
	}
	return out, nil
}

func validateCompanyGateway(networks []string, gateway netip.Addr) error {
	if !gateway.IsValid() || gateway.IsUnspecified() || gateway.IsLoopback() || gateway.IsMulticast() {
		return errors.New("无效的 VPN 网关")
	}
	for _, value := range networks {
		p, ok := remotePrefix(value)
		if !ok || p.Contains(gateway.Unmap()) {
			return errors.New("公司网段包含 VPN 服务器地址，会形成路由回环；请缩小公司网段。")
		}
	}
	return nil
}
func validateCompanyFamilies(networks, addresses []string) error {
	v4, v6 := false, false
	for _, value := range addresses {
		a, e := netip.ParseAddr(value)
		if e != nil {
			return e
		}
		if a.Is4() {
			v4 = true
		} else {
			v6 = true
		}
	}
	for _, value := range networks {
		p, ok := remotePrefix(value)
		if !ok || (p.Addr().Is4() && !v4) || (p.Addr().Is6() && !v6) {
			return errors.New("公司网段的地址类型未获得 VPN 支持，请检查 IPv4/IPv6 配置。")
		}
	}
	return nil
}
func readCompanyState(path string) (companyState, error) {
	var s companyState
	if filepath.Dir(path) == serviceRunDirectory || !strings.HasPrefix(filepath.Dir(path), serviceRunDirectory+"/company-") || filepath.Base(path) != "state.json" {
		return s, errors.New("invalid company state path")
	}
	if err := secureRootPath(filepath.Dir(path), true); err != nil {
		return s, err
	}
	if err := secureRootPath(path, false); err != nil {
		return s, err
	}
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	err = json.NewDecoder(io.LimitReader(f, 65537)).Decode(&s)
	if err == nil && (!safeTunInterface.MatchString(s.Device) || len(s.Networks) > 256 || len(s.Added) > 256) {
		err = errors.New("invalid company state")
	}
	return s, err
}
func saveCompanyState(path string, s companyState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func companyRouteInterface(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "interface:" {
			return f[1]
		}
	}
	return ""
}
func cleanupCompany(path string) error {
	s, err := readCompanyState(path)
	if err != nil {
		return err
	}
	var remaining []string
	for _, value := range s.Added {
		p, ok := remotePrefix(value)
		if !ok {
			return errors.New("invalid recorded route")
		}
		family := "-inet6"
		if p.Addr().Is4() {
			family = "-inet"
		}
		current, e := systemCommand("/sbin/route", "-n", "get", family, p.Addr().String())
		if e != nil {
			remaining = append(remaining, value)
			continue
		}
		// Never delete a route that another network service replaced.
		if companyRouteInterface(current) != s.Device {
			continue
		}
		if _, e = systemCommand("/sbin/route", "-n", "delete", family, "-net", p.String(), "-interface", s.Device); e != nil {
			remaining = append(remaining, value)
		}
	}
	s.Added = remaining
	s.State = "stopped"
	if len(remaining) > 0 {
		s.State = "cleanupFailed"
	}
	if err = saveCompanyState(path, s); err != nil {
		return err
	}
	if len(remaining) > 0 {
		return errors.New("部分公司路由尚未释放，请保留诊断记录。")
	}
	return nil
}

func makeSharedCompanyConfig(r routeRequest, device string, port uint16) map[string]any {
	rules := []string{}
	for _, value := range r.RemoteNetworks {
		p, _ := remotePrefix(value)
		kind := "IP-CIDR6"
		if p.Addr().Is4() {
			kind = "IP-CIDR"
		}
		rules = append(rules, kind+","+p.String()+",GoConnect,no-resolve")
	}
	rules = append(rules, "MATCH,REJECT")
	return map[string]any{
		"mode": "rule", "log-level": "silent", "find-process-mode": "off", "ipv6": true, "allow-lan": false, "bind-address": "127.0.0.1",
		"external-controller": fmt.Sprintf("127.0.0.1:%d", port), "secret": r.Token, "geo-auto-update": false,
		"dns": map[string]any{"enable": false}, "sniffer": map[string]any{"enable": false},
		"profile": map[string]any{"store-selected": false, "store-fake-ip": false},
		"tun":     map[string]any{"enable": true, "stack": "gvisor", "device": device, "mtu": 1400, "auto-route": false, "auto-detect-interface": false, "dns-hijack": []string{}, "inet4-address": []string{systemCaptureAddress + "/30"}, "inet6-address": []string{systemCaptureIPv6 + "/126"}},
		"proxies": []map[string]any{{"name": "GoConnect", "type": "socks5", "server": "127.0.0.1", "port": r.SOCKSPort, "username": "goconnect", "password": r.Token, "udp": true}}, "rules": rules,
	}
}

func installCompanyRoutes(path string) error {
	s, err := readCompanyState(path)
	if err != nil {
		return err
	}
	for _, value := range s.Networks {
		p, _ := remotePrefix(value)
		family := "-inet6"
		if p.Addr().Is4() {
			family = "-inet"
		}
		// Journal before mutation. No route replacement; a collision is an error.
		s.Added = append(s.Added, value)
		if err = saveCompanyState(path, s); err != nil {
			return err
		}
		if _, err = systemCommand("/sbin/route", "-n", "add", family, "-net", p.String(), "-interface", s.Device); err != nil {
			return errors.New("公司路由冲突，已停止配置。")
		}
	}
	s.State = "ready"
	return saveCompanyState(path, s)
}

func companySessionForOwner(ctx context.Context, path string, uid uint32, pid int) (result error) {
	c, err := openControl(path)
	if err != nil {
		return err
	}
	defer c.close()
	if c.uid != uid || c.request.OwnerPID != pid || !c.request.CompanyShared {
		c.snapshot.Message = "仅接受已有 AnyConnect 会话的公司网段。"
		c.write("failed")
		return errors.New("separate company session denied")
	}
	c.snapshot.CaptureMode = "shared-company-routes"
	c.write("starting")
	defer func() {
		if result != nil {
			c.snapshot.Message = result.Error()
			c.write("failed")
		} else {
			c.write("stopped")
		}
	}()
	lock, err := captureLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	leftovers, err := filepath.Glob(filepath.Join(serviceRunDirectory, "company-*"))
	if err != nil {
		return err
	}
	if len(leftovers) > 0 {
		return errors.New("发现未清理的公司路由，已暂停新连接；请先检查诊断。")
	}
	local, err := discoverLocalNetworks()
	if err != nil {
		return err
	}
	ts, err := discoverTailscale()
	if err != nil {
		return err
	}
	networks, err := companyPlan(c.request.RemoteNetworks, local, ts)
	if err != nil {
		return err
	}
	// Refuse collisions with the TUN's own addresses or another active tunnel.
	reserved := []netip.Prefix{netip.MustParsePrefix(systemCaptureAddress + "/30").Masked(), netip.MustParsePrefix(systemCaptureIPv6 + "/126").Masked()}
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, iface := range interfaces {
		addrs, e := iface.Addrs()
		if e != nil {
			return e
		}
		for _, addr := range addrs {
			p, e := netip.ParsePrefix(addr.String())
			if e != nil {
				continue
			}
			for _, r := range reserved {
				if r.Overlaps(p) {
					return errors.New("本机隧道地址已被占用，请结束冲突连接后重试。")
				}
			}
		}
	}
	for _, value := range networks {
		p, _ := remotePrefix(value)
		for _, r := range reserved {
			if p.Overlaps(r) {
				return errors.New("公司网段覆盖本机隧道地址，请缩小范围。")
			}
		}
	}
	runtime, err := runtimePath()
	if err != nil {
		return err
	}
	if err = secureRootPath(filepath.Join(runtime, "bin/mihomo"), false); err != nil {
		return err
	}
	device, err := chooseDevice()
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(serviceRunDirectory, "company-")
	if err != nil {
		return err
	}
	statePath := filepath.Join(dir, "state.json")
	if err = saveCompanyState(statePath, companyState{Device: device, Networks: networks, State: "starting"}); err != nil {
		return err
	}
	safeToClean := true
	defer func() {
		if !safeToClean {
			return
		}
		if e := cleanupCompany(statePath); e != nil {
			result = e
		} else {
			_ = os.RemoveAll(dir)
		}
	}()
	data, err := json.Marshal(makeSharedCompanyConfig(c.request, device, port))
	if err != nil {
		return err
	}
	config := filepath.Join(dir, "config.json")
	if err = os.WriteFile(config, data, 0600); err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", config)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		if e := stopChild(cmd, done); e != nil {
			safeToClean = false
			result = e
		}
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	started := time.Now()
	lastScope := time.Time{}
	isReady := false
	client := &http.Client{Timeout: time.Second}
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-done:
			done <- e
			return errors.New("公司网段转发进程已结束。")
		case <-tick.C:
			if !c.alive() {
				return nil
			}
			if !isReady {
				if ready(client, port, c.request.Token, device) {
					// Revalidate after TUN startup before any destination route is installed.
					local, e := discoverLocalNetworks()
					if e != nil {
						return e
					}
					tails, e := discoverTailscale()
					if e != nil {
						return e
					}
					if _, e = companyPlan(networks, local, tails); e != nil {
						return e
					}
					if e = installCompanyRoutes(statePath); e != nil {
						return e
					}
					isReady = true
					c.snapshot.SystemDevice = device
					c.snapshot.RemoteNetworks = networks
					c.snapshot.TailscaleActive = len(tails.Interfaces) > 0
					c.snapshot.Message = "公司网段与代理共用当前 AnyConnect 会话。"
					c.write("ready")
				} else if time.Since(started) > 15*time.Second {
					return errors.New("公司网段转发入口启动超时。")
				}
			} else if time.Since(lastScope) > 15*time.Second {
				lastScope = time.Now()
				local, e := discoverLocalNetworks()
				if e != nil {
					return e
				}
				tails, e := discoverTailscale()
				if e != nil {
					return e
				}
				if _, e = companyPlan(networks, local, tails); e != nil {
					return e
				}
			}
		}
	}
}
