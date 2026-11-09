package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// Explicitly enabled, scoped only to a unique .invalid fixture. Does not change
// the default resolver, network preferences, VPN, helper, TUN or routes.
func TestNativeManagedDNSScoped(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_NATIVE_DNS") != "1" {
		t.Skip("explicit isolated native DNS test required")
	}
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	port := uint16(server.LocalAddr().(*net.UDPAddr).Port)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, e := server.ReadFrom(buf)
			if e != nil {
				return
			}
			server.WriteTo(dnsWireAnswer(buf[:n], [4]byte{192, 0, 2, 173}), peer)
		}
	}()
	scope := fmt.Sprintf("qa-%d.goconnect-dns.invalid", time.Now().UnixNano())
	lease, err := acquireManagedDNS(port, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	if err = waitManagedDNS(port, scope, true); err != nil {
		t.Fatal(err)
	}
	result, err := exec.Command("/usr/bin/dscacheutil", "-q", "host", "-a", "name", "first."+scope).CombinedOutput()
	if err != nil || !strings.Contains(string(result), "192.0.2.173") {
		t.Fatalf("native system DNS did not reach candidate: %v %s", err, result)
	}
	lease.close()
	if err = waitManagedDNS(port, scope, false); err != nil {
		t.Fatal(err)
	}
	// A separate native store session must be removed on SIGKILL as well.
	child := exec.Command(os.Args[0], "-test.run=^TestNativeManagedDNSChild$", "-test.v")
	child.Env = append(os.Environ(), "GOCONNECT_NATIVE_DNS_CHILD="+scope, "GOCONNECT_NATIVE_DNS_PORT="+strconv.Itoa(int(port)))
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	scanner := bufio.NewScanner(output)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "DNS_LEASE_READY" {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("child DNS lease not ready")
	}
	if err = waitManagedDNS(port, scope, true); err != nil {
		t.Fatal(err)
	}
	result, err = exec.Command("/usr/bin/dscacheutil", "-q", "host", "-a", "name", "second."+scope).CombinedOutput()
	if err != nil || !strings.Contains(string(result), "192.0.2.173") {
		t.Fatalf("native child DNS did not answer: %v %s", err, result)
	}
	child.Process.Kill()
	child.Wait()
	if err = waitManagedDNS(port, scope, false); err != nil {
		t.Fatal("crashed worker left DNS installed", err)
	}
	t.Log("native macOS lookup, normal removal and SIGKILL removal passed; only the unique test suffix was registered")
}

// Explicit acceptance with an already-running proxy. Only a temporary DNS
// resolver is installed; the production app, helper and route table are not
// changed. The core starts and answers before native resolver registration.
func TestNativeManagedDNSLive(t *testing.T) {
	fixture := os.Getenv("GOCONNECT_TEST_DNS_LIVE_FIXTURE")
	if fixture == "" {
		t.Skip("explicit live DNS acceptance required")
	}
	var r routeRequest
	wire, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(wire, &r); err != nil {
		t.Fatal(err)
	}
	if r.SOCKSPort == 0 || len(r.Token) < 32 {
		t.Fatal("invalid fixture")
	}
	// This test exercises device-wide protected resolution, not the whitelist
	// mode's deferred application-connection resolution.
	r.RoutingMode = modeGlobal
	original, err := originalPhysicalNetwork(true)
	if err != nil {
		t.Fatal(err)
	}
	p, err := discoverManagedDNS()
	if err != nil {
		t.Fatal(err)
	}
	r.managedDNS = p
	apiPort, _ := freePort()
	cfg := makeTransparentConfig(r, "utun99", apiPort, original, nil, tailscaleBypass{})
	// Only a fresh test hostname receives a deliberately failing upstream.
	// This proves native macOS does not retry protected names via router DNS.
	failureToken := fmt.Sprintf("gcf%d", time.Now().UnixNano())
	failureName := failureToken + "-1.d.ip.net.coffee"
	failure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "test failure", 503) }))
	defer failure.Close()
	cfg["dns"].(map[string]any)["nameserver-policy"].(map[string]any)[failureName] = []string{failure.URL + "/dns-query#DIRECT&skip-cert-verify=true"}
	startManagedDNSCore(t, cfg, p.port)
	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", r.SOCKSPort), &proxy.Auth{User: "goconnect", Password: r.Token}, &net.Dialer{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DialContext: dialer.(proxy.ContextDialer).DialContext}}
	lookup := func(name string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, "/usr/bin/dscacheutil", "-q", "host", "-a", "name", name).CombinedOutput()
		if e != nil {
			t.Fatalf("native lookup %s failed: %v", name, e)
		}
		return string(out)
	}
	observed := func(token string, expect bool) []string {
		for i := 0; i < 8; i++ {
			response, e := client.Get("https://ip.net.coffee/api/dns/result/" + token)
			if e != nil {
				t.Fatal(e)
			}
			var result struct {
				Servers []string `json:"dns_servers"`
			}
			e = json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			if len(result.Servers) > 0 {
				return result.Servers
			}
			if !expect && i == 3 {
				return nil
			}
			time.Sleep(300 * time.Millisecond)
		}
		return nil
	}
	directToken := fmt.Sprintf("gcd%d", time.Now().UnixNano())
	lookup(directToken + "-1.d.ip.net.coffee")
	directResolvers := observed(directToken, true)
	if len(directResolvers) == 0 {
		t.Fatal("baseline authoritative observation unavailable")
	}
	lease, err := installManagedDNS(p)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	for _, name := range []string{"api.anthropic.com", "api.openai.com", "generativelanguage.googleapis.com", "www.baidu.com", "www.taobao.com", "www.qq.com"} {
		if out := lookup(name); !strings.Contains(out, "ip_address:") && !strings.Contains(out, "ipv6_address:") {
			t.Fatalf("native lookup returned no address for %s", name)
		}
	}
	protectedToken := fmt.Sprintf("gcp%d", time.Now().UnixNano())
	if out := lookup(protectedToken + "-1.d.ip.net.coffee"); !strings.Contains(out, "ip_address:") {
		t.Fatal("native proxied test domain did not resolve")
	}
	protectedResolvers := observed(protectedToken, true)
	if len(protectedResolvers) == 0 {
		t.Fatal("proxy authoritative observation unavailable")
	}
	for _, a := range directResolvers {
		for _, b := range protectedResolvers {
			if a == b {
				t.Fatal("protected query reached original resolver", a)
			}
		}
	}
	if out := lookup(failureName); strings.Contains(out, "ip_address:") {
		t.Fatal("failed protected query unexpectedly resolved")
	}
	leakedFailure := observed(failureToken, false)
	if len(leakedFailure) > 0 {
		t.Fatal("native resolver fell back after protected DoH failure", leakedFailure)
	}
	if out := lookup("www.taobao.com"); !strings.Contains(out, "ip_address:") {
		t.Fatal("domestic DNS failed")
	}
	lease.close()
	if err = waitManagedDNS(p.port, "", false); err != nil {
		t.Fatal(err)
	}
	t.Logf("native AI/domestic lookups passed; original resolvers=%v; protected resolvers=%v; failure did not fall back; DNS lease removed", directResolvers, protectedResolvers)
}

func TestNativeManagedDNSChild(t *testing.T) {
	scope := os.Getenv("GOCONNECT_NATIVE_DNS_CHILD")
	if scope == "" {
		t.Skip("child only")
	}
	port, _ := strconv.Atoi(os.Getenv("GOCONNECT_NATIVE_DNS_PORT"))
	lease, err := acquireManagedDNS(uint16(port), scope)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	fmt.Println("DNS_LEASE_READY")
	time.Sleep(20 * time.Second)
}
