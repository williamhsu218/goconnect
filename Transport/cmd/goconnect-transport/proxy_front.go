package main

// Explicit proxy ingress. This path must never create a TUN, inspect application
// sockets, or modify PF, system DNS, or system proxy settings.
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type proxyFrontRequest struct {
	Port          uint16       `json:"port"`
	Token         string       `json:"token"`
	DirectDomains []domainRule `json:"directDomains"`
}

func (r proxyFrontRequest) validate() error {
	if r.Port == 0 || len(r.Token) < 32 || len(r.Token) > 200 || strings.ContainsAny(r.Token, "\x00\r\n:") || len(r.DirectDomains) > 256 {
		return errors.New("invalid proxy configuration")
	}
	for _, rule := range r.DirectDomains {
		if _, ok := directIPPrefix(rule.Domain); !ok && !validDirectDomain(rule.Domain) {
			return errors.New("invalid direct rule")
		}
	}
	return nil
}

func makeProxyFrontConfig(r proxyFrontRequest, port uint16, local []localNetworkRoute) map[string]any {
	rules := []string{"DOMAIN,localhost,DIRECT", "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR6,::1/128,DIRECT,no-resolve", "IP-CIDR,169.254.0.0/16,DIRECT,no-resolve", "IP-CIDR6,fe80::/10,DIRECT,no-resolve", "IP-CIDR,100.64.0.0/10,DIRECT,no-resolve", "IP-CIDR6,fd7a:115c:a1e0::/48,DIRECT,no-resolve", "DOMAIN-SUFFIX,local,DIRECT"}
	for _, route := range local {
		kind := "IP-CIDR6"
		if route.Prefix.Addr().Is4() {
			kind = "IP-CIDR"
		}
		rules = append(rules, kind+","+route.Prefix.String()+",DIRECT,no-resolve")
	}
	rules = append(rules, directDomainRules(r.DirectDomains)...)
	rules = append(rules, "MATCH,GoConnect")
	return map[string]any{
		"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false,
		"mode": "rule", "log-level": "silent", "find-process-mode": "off", "ipv6": true,
		"geo-auto-update": false, "dns": map[string]any{"enable": false},
		"tun": map[string]any{"enable": false}, "sniffer": map[string]any{"enable": false},
		"profile": map[string]any{"store-selected": false, "store-fake-ip": false},
		"proxies": []map[string]any{{"name": "GoConnect", "type": "socks5", "server": "127.0.0.1", "port": r.Port, "username": "goconnect", "password": r.Token, "udp": true}},
		"rules":   rules,
	}
}

// SIGKILL does not guarantee that a process stuck inside the kernel can exit.
// Never synchronously wait forever on the session's control path.
func stopChild(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	grouped := cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid
	signal := func(sig syscall.Signal) {
		if grouped {
			_ = syscall.Kill(-cmd.Process.Pid, sig)
		} else {
			_ = cmd.Process.Signal(sig)
		}
	}
	exited := false
	select {
	case <-done:
		exited = true
	default:
	}
	if !exited {
		signal(syscall.SIGINT)
		select {
		case <-done:
			exited = true
		case <-time.After(2 * time.Second):
		}
	}
	// A shell hook can outlive OpenConnect after a signal. Stop the entire owned
	// group before route cleanup, even when its leader has already exited.
	if grouped || !exited {
		signal(syscall.SIGKILL)
	}
	if !exited {
		select {
		case <-done:
			exited = true
		case <-time.After(2 * time.Second):
		}
	}
	groupGone := !grouped
	if grouped {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH) {
				groupGone = true
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	if exited && groupGone {
		return nil
	}
	emit(map[string]any{"event": "quarantined"})
	return errors.New("子进程无法退出，已停止等待；请保留诊断记录，不要反复重连。")
}

func proxyFrontSession(parent context.Context) (result error) {
	defer func() {
		if result != nil {
			fail("本机代理入口已停止，请查看诊断后重新连接。")
		}
	}()
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 65537))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	var r proxyFrontRequest
	if json.Unmarshal(line, &r) != nil {
		return errors.New("invalid input")
	}
	if err = r.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, reader); cancel() }()
	local, err := discoverLocalNetworks()
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "GoConnect-proxy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	data, err := json.Marshal(makeProxyFrontConfig(r, port, local))
	if err != nil {
		return err
	}
	file := filepath.Join(dir, "config.json")
	if err = os.WriteFile(file, data, 0600); err != nil {
		return err
	}
	runtime, err := runtimePath()
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", file)
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
		if e := stopChild(cmd, done); e != nil && result == nil {
			result = e
		}
	}()
	for i := 0; i < 100; i++ {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 50*time.Millisecond)
		if e == nil {
			c.Close()
			emit(map[string]any{"event": "ready", "port": port})
			select {
			case <-ctx.Done():
				return nil
			case e := <-done:
				done <- e
				return errors.New("proxy exited")
			}
		}
		select {
		case e := <-done:
			done <- e
			return errors.New("proxy exited before ready")
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
	return errors.New("proxy readiness timeout")
}
