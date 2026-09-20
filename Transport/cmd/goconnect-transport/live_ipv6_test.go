package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type hostnameOnlyVPN struct {
	target   string
	calls    atomic.Int32
	rejected atomic.Int32
}

func (d *hostnameOnlyVPN) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.calls.Add(1)
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "example.com" {
		d.rejected.Add(1)
		return nil, errors.New("IPv4 VPN requires remote hostname resolution")
	}
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

// Use an IPv6 literal at ingress, real TLS SNI and an upstream that cannot
// dial literal IPv6. No TUN/routes or external credentials are involved.
func TestLiveIPv6TLSUsesHostnameWithoutBypassingVPN(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("requires bundled core")
	}
	for _, name := range []string{"old-no-override", "global-vpn", "whitelist-vpn", "direct-domain", "direct-app", "vpn-down"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			token := strings.Repeat("v", 40)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "oauth-transport-ok") }))
			defer server.Close()
			_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			upstream := &hostnameOnlyVPN{target: server.Listener.Addr().String()}
			if name == "vpn-down" {
				upstream.target = "127.0.0.1:0"
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go (&bridge.SOCKSServer{Dialer: upstream, Token: token}).Serve(ctx, listener)
			control, e := freePort()
			if e != nil {
				t.Fatal(e)
			}
			inbound, e := freePort()
			if e != nil {
				t.Fatal(e)
			}
			r := routeRequest{LiveRouting: true, RoutingMode: modeGlobal, SOCKSPort: uint16(listener.Addr().(*net.TCPAddr).Port), Token: token}
			var paths []string
			if name == "whitelist-vpn" || name == "direct-app" {
				self, _ := os.Executable()
				self, _ = filepath.EvalSymlinks(self)
				paths = []string{self}
			}
			if name == "whitelist-vpn" {
				r.RoutingMode = modeWhitelist
			}
			if name == "direct-domain" {
				r.DirectDomains = []domainRule{{Domain: "example.com"}}
			}
			config := makeLiveConfig(r, "utun999", control)
			config["tun"].(map[string]any)["enable"] = false
			config["socks-port"] = inbound
			config["log-level"] = "debug"
			if name == "direct-app" || name == "direct-domain" || name == "vpn-down" {
				config["hosts"] = map[string]any{"example.com": "127.0.0.1"}
			}
			if name == "old-no-override" {
				config["sniffer"].(map[string]any)["override-destination"] = false
			}
			if err = writeLiveRules(dir, paths); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", path)
			coreLogPath := filepath.Join(dir, "core.log")
			coreLog, err := os.Create(coreLogPath)
			if err != nil {
				t.Fatal(err)
			}
			defer coreLog.Close()
			cmd.Stdout = coreLog
			cmd.Stderr = coreLog
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			api := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
			defer api.CloseIdleConnections()
			ready := false
			for i := 0; i < 60; i++ {
				if liveRulesReady(api, control, token, 1) {
					ready = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !ready {
				t.Fatal("core not ready")
			}
			dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", inbound), nil, &net.Dialer{Timeout: time.Second})
			if err != nil {
				snapshot, _ := coreSnapshot(api, control, token)
				contents, _ := os.ReadFile(coreLogPath)
				t.Logf("core connections: %+v; expected paths: %+v\n%s", snapshot, paths, contents)
				t.Fatal(err)
			}
			raw, err := dialer.Dial("tcp", net.JoinHostPort("2001:db8::1234", port))
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			raw.SetDeadline(time.Now().Add(3 * time.Second))
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			secure := tls.Client(raw, &tls.Config{RootCAs: roots, ServerName: "example.com"})
			err = secure.Handshake()
			if name == "old-no-override" || name == "vpn-down" {
				if err == nil {
					t.Fatal("failed VPN leaked to reachable DIRECT destination")
				}
				if upstream.calls.Load() == 0 {
					t.Fatal("VPN was not attempted")
				}
				if name == "old-no-override" && upstream.rejected.Load() == 0 {
					t.Fatal("old IPv6 failure not reproduced")
				}
				return
			}
			if err != nil {
				snapshot, _ := coreSnapshot(api, control, token)
				contents, _ := os.ReadFile(coreLogPath)
				t.Logf("core connections: %+v; expected paths: %+v\n%s", snapshot, paths, contents)
				if len(paths) > 0 && strings.Contains(string(contents), "find process error") {
					t.Skip("当前用户态 Mihomo 无法读取进程路径；精确路径需由显式 root TUN 验收覆盖")
				}
				t.Fatal(err)
			}
			// Synthetic POST models the OAuth method, with no real credentials.
			fmt.Fprint(secure, "POST /token HTTP/1.1\r\nHost: example.com\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			body, err := io.ReadAll(secure)
			if err != nil || !strings.Contains(string(body), "oauth-transport-ok") {
				t.Fatal("TLS POST failed", err)
			}
			direct := name == "direct-domain" || name == "direct-app"
			if direct != (upstream.calls.Load() == 0) {
				t.Fatal("wrong egress", upstream.calls.Load())
			}
			t.Log("IPv6 ingress + certificate-verified TLS POST succeeded; direct=", direct)
		})
	}
}
