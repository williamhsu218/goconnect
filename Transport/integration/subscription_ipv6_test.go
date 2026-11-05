package integration

import (
	"bufio"
	"context"
	"encoding/base64"
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

	"golang.org/x/net/proxy"
)

// Exercise the packaged resolver, reduced node config and actual Mihomo socket
// over IPv6 loopback. No TUN, privileged service, real subscription or DNS used.
func TestSubscriptionIPv6NodeAndLease(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		name := "anonymous"
		if authenticated {
			name = "authenticated"
		}
		t.Run(name, func(t *testing.T) { testSubscriptionIPv6NodeAndLease(t, authenticated) })
	}
}
func testSubscriptionIPv6NodeAndLease(t *testing.T, authenticated bool) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("packaged runtime required")
	}
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal("IPv6 loopback unavailable", err)
	}
	observed := make(chan bool, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authenticated && r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("isolated-user:isolated-password")) {
			http.Error(w, "proxy authentication required", 407)
			return
		}
		if r.Method != "CONNECT" || r.Host != "192.0.2.1:80" {
			http.Error(w, "unexpected destination", 400)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		request, err := http.ReadRequest(rw.Reader)
		if err != nil {
			return
		}
		request.Body.Close()
		observed <- request.URL.Path == "/ipv6-fixture"
		fmt.Fprint(rw, "HTTP/1.1 200 OK\r\nContent-Length: 9\r\nConnection: close\r\n\r\nipv6-node")
		rw.Flush()
	}))
	upstream.Listener.Close()
	upstream.Listener = listener
	upstream.Start()
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime, "bin/GoConnectTransport"), "subscription-session")
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
				t.Error("node did not stop")
			}
		}
	}()
	token := strings.Repeat("isolated-node-", 4)
	node := map[string]any{"name": "IPv6 fixture", "type": "http", "proxy": map[string]any{"name": "IPv6 fixture", "type": "http", "server": "::1", "port": listener.Addr().(*net.TCPAddr).Port}}
	if authenticated {
		config := node["proxy"].(map[string]any)
		config["username"] = "isolated-user"
		config["password"] = "isolated-password"
	}
	if err = json.NewEncoder(input).Encode(map[string]any{"node": node, "token": token}); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 16)
	go func() {
		decoder := json.NewDecoder(stdout)
		for {
			var event map[string]any
			if decoder.Decode(&event) != nil {
				return
			}
			select {
			case events <- event:
			default:
			}
		}
	}()
	var event map[string]any
	select {
	case event = <-events:
	case <-ctx.Done():
		t.Fatal("node readiness timeout")
	}
	if event["event"] != "ready" || event["gateway"] != "::1" {
		t.Fatal("IPv6 node was rejected", event)
	}
	endpoint := fmt.Sprintf("127.0.0.1:%d", int(event["port"].(float64)))
	socks, err := proxy.SOCKS5("tcp", endpoint, &proxy.Auth{User: "goconnect", Password: token}, &net.Dialer{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := socks.Dial("tcp", "192.0.2.1:80")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(conn, "GET /ipv6-fixture HTTP/1.0\r\nHost: 192.0.2.1\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "ipv6-node" {
		t.Fatal("IPv6 upstream not used", string(body), err)
	}
	select {
	case ok := <-observed:
		if !ok {
			t.Fatal("wrong request")
		}
	default:
		t.Fatal("upstream saw no request")
	}
	// Closed connections still contribute to the session totals; an idle
	// sample must not reset the counters or be mistaken for unavailable data.
	conn.Close()
	var received, sent float64
	for received == 0 || sent == 0 {
		select {
		case sample := <-events:
			if sample["event"] == "stats" {
				received, _ = sample["received"].(float64)
				sent, _ = sample["sent"].(float64)
			}
		case <-ctx.Done():
			t.Fatal("node did not report bidirectional totals")
		}
	}
	for {
		select {
		case sample := <-events:
			if sample["event"] != "stats" {
				continue
			}
			if sample["received"].(float64) < received || sample["sent"].(float64) < sent {
				t.Fatal("closed connection totals reset")
			}
			goto statisticsVerified
		case <-ctx.Done():
			t.Fatal("idle statistics timeout")
		}
	}
statisticsVerified:
	input.Close()
	select {
	case err = <-ended:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("lease shutdown timeout")
	}
}
