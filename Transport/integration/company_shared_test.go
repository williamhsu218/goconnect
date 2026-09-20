package integration

import (
	"bufio"
	"context"
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
)

type sharedFixtureDialer struct {
	target string
	calls  atomic.Int64
}

func (d *sharedFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !strings.HasPrefix(address, "10.254.252.253:") {
		return nil, fmt.Errorf("unexpected fixture target")
	}
	d.calls.Add(1)
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

// Opt-in only: creates one synthetic private host route through the installed
// authenticated service. No real profile, default route, DNS or PF mutation.
func TestInstalledSharedCompanyRouteAndProxy(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_SHARED_COMPANY") != "1" {
		t.Skip("explicit installed-service route test required")
	}
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Fatal("runtime required")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "shared-company-fixture") }))
	defer target.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("shared-fixture-", 4)
	dialer := &sharedFixtureDialer{target: target.Listener.Addr().String()}
	go (&bridge.SOCKSServer{Dialer: dialer, Token: token}).Serve(ctx, upstream)
	dir, err := os.MkdirTemp("", "GoConnect-shared-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	lease := filepath.Join(dir, "lease")
	if err = os.WriteFile(lease, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				// Refresh only; never recreate the lease after disconnect.
				_ = os.Chtimes(lease, now, now)
			}
		}
	}()
	request := map[string]any{"ownerPID": os.Getpid(), "controlDirectory": dir, "token": token, "companyShared": true, "socksPort": upstream.Addr().(*net.TCPAddr).Port, "gateway": "203.0.113.5", "vpnAddresses": []string{"192.0.2.2"}, "remoteNetworks": []string{"10.254.252.253/32"}}
	data, _ := json.Marshal(request)
	file := filepath.Join(dir, "session.json")
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	service := exec.Command(filepath.Join(runtime, "bin/GoConnectTransport"), "service-route", file)
	service.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	if err = service.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- service.Wait() }()
	stopped := false
	defer func() {
		os.Remove(lease)
		if !stopped {
			select {
			case <-ended:
			case <-time.After(18 * time.Second):
				service.Process.Kill()
				t.Error("service shutdown timeout")
			}
		}
	}()
	var status struct {
		State        string
		Message      string
		SystemDevice string
	}
	ready := false
	for start := time.Now(); time.Since(start) < 25*time.Second; {
		now := time.Now()
		os.Chtimes(lease, now, now)
		data, _ := os.ReadFile(filepath.Join(dir, "status"))
		_ = json.Unmarshal(data, &status)
		if status.State == "ready" {
			ready = true
			break
		}
		if status.State == "failed" {
			t.Fatalf("company fixture failed: %s", status.Message)
		}
		select {
		case err = <-ended:
			stopped = true
			t.Fatalf("service ended before ready: %v", err)
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("company fixture did not become ready")
	}
	route, err := exec.Command("/sbin/route", "-n", "get", "10.254.252.253").Output()
	if err != nil || !strings.Contains(string(route), status.SystemDevice) {
		t.Fatal("synthetic host is not routed through owned TUN")
	}
	_, port, _ := net.SplitHostPort(target.Listener.Addr().String())
	address := "http://10.254.252.253:" + port
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, Timeout: 5 * time.Second}
	check := func() {
		t.Helper()
		res, e := client.Get(address)
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(res.Body)
		res.Body.Close()
		if e != nil || string(data) != "shared-company-fixture" {
			t.Fatalf("fixture reply: %v", e)
		}
	}
	check()
	if dialer.calls.Load() != 1 {
		t.Fatal("system request did not use shared upstream")
	}
	front := exec.Command(filepath.Join(runtime, "bin/GoConnectTransport"), "proxy-front")
	front.Env = service.Env
	input, _ := front.StdinPipe()
	stdout, _ := front.StdoutPipe()
	if err = front.Start(); err != nil {
		t.Fatal(err)
	}
	frontDone := make(chan error, 1)
	go func() { frontDone <- front.Wait() }()
	defer func() {
		input.Close()
		select {
		case <-frontDone:
		case <-time.After(7 * time.Second):
			front.Process.Kill()
			t.Error("proxy cleanup timeout")
		}
	}()
	json.NewEncoder(input).Encode(map[string]any{"port": upstream.Addr().(*net.TCPAddr).Port, "token": token, "directDomains": []any{}})
	ev := make(chan map[string]any, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			var e map[string]any
			json.Unmarshal(scan.Bytes(), &e)
			ev <- e
		}
	}()
	select {
	case e := <-ev:
		if e["event"] != "ready" {
			t.Fatal("proxy not ready")
		}
		proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", int(e["port"].(float64))))
		client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	case <-time.After(10 * time.Second):
		t.Fatal("proxy readiness timeout")
	}
	check()
	if dialer.calls.Load() != 2 {
		t.Fatal("proxy and company routing did not share upstream")
	}
	os.Remove(lease)
	select {
	case err = <-ended:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(18 * time.Second):
		t.Fatal("route lease cleanup timeout")
	}
	route, _ = exec.Command("/sbin/route", "-n", "get", "10.254.252.253").Output()
	if strings.Contains(string(route), status.SystemDevice) {
		t.Fatal("company route survived disconnect")
	}
	t.Log("System host route and explicit HTTP proxy shared one authenticated SOCKS upstream; lease removed owned route.")
}
