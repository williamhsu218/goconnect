package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
)

// Reproduce the 0.8.0 global-mode regression with the real core: a process
// lookup miss rejects TLS even though the same upstream VPN is operational.
func TestLiveCoreGlobalLookupMissTLSRegression(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("requires bundled core")
	}
	for _, oldPolicy := range []bool{true, false} {
		t.Run(fmt.Sprintf("old080=%v", oldPolicy), func(t *testing.T) {
			dir := t.TempDir()
			token := strings.Repeat("t", 40)
			vpn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "VPN") }))
			defer vpn.Close()
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go (&bridge.SOCKSServer{Dialer: liveLoopbackDialer{address: strings.TrimPrefix(vpn.URL, "https://")}, Token: token}).Serve(ctx, listener)
			control, _ := freePort()
			inbound, _ := freePort()
			r := routeRequest{LiveRouting: true, RoutingMode: modeGlobal, SOCKSPort: uint16(listener.Addr().(*net.TCPAddr).Port), Token: token}
			config := makeLiveConfig(r, "utun999", control)
			config["tun"].(map[string]any)["enable"] = false
			config["socks-port"] = inbound
			config["find-process-mode"] = "off"
			if oldPolicy {
				config["rules"] = []string{"RULE-SET,goconnect-apps,DIRECT", "PROCESS-PATH-REGEX,^.+$,GoConnect", "MATCH,REJECT"}
			}
			if err = writeLiveRules(dir, nil); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(config)
			path := filepath.Join(dir, "config.json")
			os.WriteFile(path, data, 0600)
			cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", path)
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
			dialer, _ := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", inbound), nil, &net.Dialer{Timeout: time.Second})
			raw, err := dialer.Dial("tcp", strings.TrimPrefix(vpn.URL, "https://"))
			if err != nil {
				if oldPolicy {
					return
				}
				t.Fatal(err)
			}
			defer raw.Close()
			raw.SetDeadline(time.Now().Add(3 * time.Second))
			roots := x509.NewCertPool()
			roots.AddCert(vpn.Certificate())
			secure := tls.Client(raw, &tls.Config{RootCAs: roots, ServerName: "example.com"})
			err = secure.Handshake()
			if oldPolicy {
				if err == nil {
					t.Fatal("old policy unexpectedly permits unknown TLS")
				}
				t.Log("0.8.0 lookup miss reproduces TLS handshake failure")
				return
			}
			if err != nil {
				t.Fatal("fixed global fallback lost VPN TLS", err)
			}
			fmt.Fprint(secure, "GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
			response, err := io.ReadAll(secure)
			if err != nil || !strings.Contains(string(response), "VPN") {
				t.Fatal("VPN response missing", err)
			}
			t.Log("fixed global policy completes certificate-verified TLS over VPN despite lookup miss")
		})
	}
}
