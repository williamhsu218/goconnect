package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// This exercises the bundled core's real IP-to-domain sniffer using SOCKS IP
// destinations. It does not alter system DNS/routes or need a privileged TUN.
func TestBundledDomainSnifferDirectAndFallback(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("set packaged Runtime")
	}
	port, _ := freePort()
	control, _ := freePort()
	upstream, _ := freePort()
	r := routeRequest{RoutingMode: modeGlobal, SOCKSPort: upstream, Token: "domain-test-token-012345678901234567890", DirectDomains: []domainRule{{"direct.test", true}, {"exact.test", false}}}
	config := makeConfig(r, runtime, "", control)
	config["tun"] = map[string]any{"enable": false}
	config["socks-port"] = port
	dir := t.TempDir()
	data, _ := json.Marshal(config)
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, data, 0600)
	cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ready := false
	for i := 0; i < 100; i++ {
		c, e := net.DialTimeout("tcp", addr, time.Millisecond*50)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(time.Millisecond * 50)
	}
	if !ready {
		t.Fatal("core not ready")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-ok") }))
	defer server.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tls-direct-ok") }))
	defer secure.Close()
	dialer, _ := proxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: time.Second * 4})
	for _, tc := range []struct {
		host            string
		secure, allowed bool
	}{{"direct.test", false, true}, {"sub.direct.test", false, true}, {"exact.test", false, true}, {"notdirect.test", false, false}, {"sub.exact.test", false, false}, {"other.test", false, false}, {"direct.test", true, true}, {"other.test", true, false}} {
		t.Run(fmt.Sprintf("%s-tls-%v", tc.host, tc.secure), func(t *testing.T) {
			destination := server.Listener.Addr().String()
			if tc.secure {
				destination = secure.Listener.Addr().String()
			}
			c, err := dialer.Dial("tcp", destination)
			if err != nil {
				if tc.allowed {
					t.Fatal(err)
				}
				return
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(4 * time.Second))
			if tc.secure {
				tlsConn := tls.Client(c, &tls.Config{ServerName: tc.host, InsecureSkipVerify: true})
				c = tlsConn
				if err = tlsConn.Handshake(); err != nil {
					if tc.allowed {
						t.Fatal(err)
					}
					return
				}
			}
			fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", tc.host)
			response, err := io.ReadAll(c)
			success := len(response) > 0 && string(response[:min(12, len(response))]) == "HTTP/1.1 200"
			if success != tc.allowed {
				t.Fatalf("direct=%v expected=%v err=%v", success, tc.allowed, err)
			}
		})
	}
}
