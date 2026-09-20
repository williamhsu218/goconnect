package integration

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
)

type fixtureProxyDialer struct {
	target string
	calls  atomic.Int64
}

func (d *fixtureProxyDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.calls.Add(1)
	if !strings.HasPrefix(address, "example.com:") {
		return nil, fmt.Errorf("unexpected upstream destination")
	}
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func TestExplicitProxyRoutesAndLease(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("packaged runtime required")
	}
	tlsTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tunnel-fixture") }))
	defer tlsTarget.Close()
	directTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-fixture") }))
	defer directTarget.Close()
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := &fixtureProxyDialer{target: tlsTarget.Listener.Addr().String()}
	token := strings.Repeat("test-token-", 4)
	go (&bridge.SOCKSServer{Dialer: dialer, Token: token}).Serve(ctx, upstream)
	cmd := exec.Command(filepath.Join(runtime, "bin/GoConnectTransport"), "proxy-front")
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	stopped := false
	defer func() {
		input.Close()
		if !stopped {
			select {
			case <-ended:
			case <-time.After(6 * time.Second):
				cmd.Process.Kill()
				t.Error("front did not exit")
			}
		}
	}()
	request := map[string]any{"port": upstream.Addr().(*net.TCPAddr).Port, "token": token, "directDomains": []map[string]any{{"domain": "direct.example.invalid", "includeSubdomains": false}}}
	if err = json.NewEncoder(input).Encode(request); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 8)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			var e map[string]any
			if json.Unmarshal(scan.Bytes(), &e) == nil {
				events <- e
			}
		}
		close(events)
	}()
	port := 0
	select {
	case event := <-events:
		if event["event"] != "ready" {
			t.Fatalf("front not ready: %v", event)
		}
		port = int(event["port"].(float64))
	case <-time.After(10 * time.Second):
		t.Fatal("readiness timeout")
	}
	endpoint := fmt.Sprintf("127.0.0.1:%d", port)
	proxyURL, _ := url.Parse("http://" + endpoint)
	roots := x509.NewCertPool()
	roots.AddCert(tlsTarget.Certificate())
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	get := func(address, want string) {
		t.Helper()
		r, e := client.Get(address)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r.Body)
		r.Body.Close()
		if e != nil || string(b) != want {
			t.Fatalf("%q %v", b, e)
		}
	}
	_, tlsPort, _ := net.SplitHostPort(tlsTarget.Listener.Addr().String())
	get("https://example.com:"+tlsPort, "tunnel-fixture")
	if dialer.calls.Load() != 1 {
		t.Fatal("HTTPS did not use authenticated upstream")
	}
	_, directPort, _ := net.SplitHostPort(directTarget.Listener.Addr().String())
	get("http://localhost:"+directPort, "direct-fixture")
	if dialer.calls.Load() != 1 {
		t.Fatal("direct domain incorrectly used upstream")
	}
	response, directErr := client.Get("http://direct.example.invalid:" + directPort)
	if directErr == nil {
		response.Body.Close()
	}
	if dialer.calls.Load() != 1 {
		t.Fatal("explicit direct domain incorrectly reached upstream")
	}

	// SOCKS ingress exercises the same policy engine with an ordinary TCP client.
	socks, err := proxy.SOCKS5("tcp", endpoint, nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := socks.Dial("tcp", directTarget.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(conn, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
	data, err := io.ReadAll(conn)
	conn.Close()
	if err != nil || !strings.Contains(string(data), "direct-fixture") {
		t.Fatalf("SOCKS failed: %v", err)
	}
	cancel() // Loss of backend must never turn unmatched traffic into DIRECT.
	upstream.Close()
	response, err = client.Get("https://example.com:" + tlsPort)
	if err == nil {
		response.Body.Close()
		t.Fatal("unexpected success after upstream loss")
	}
	get("http://localhost:"+directPort, "direct-fixture")
	input.Close()
	select {
	case err = <-ended:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("lease close timeout")
	}
	conn, err = net.DialTimeout("tcp", endpoint, 300*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("listener survived lease shutdown")
	}
}
