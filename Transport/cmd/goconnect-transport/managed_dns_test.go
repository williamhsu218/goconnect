package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
)

func TestManagedDNSOriginalResolvers(t *testing.T) {
	data := `DNS configuration
resolver #1
  nameserver[0] : fe80::1%en0
  nameserver[1] : 192.168.1.1
  if_index : 6 (en0)
resolver #2
  domain : corp.example
  nameserver[0] : 100.100.100.100
  if_index : 18 (utun4)
  port : 5353
  order : 100
DNS configuration (for scoped queries)
resolver #1
  nameserver[0] : 9.9.9.9
  if_index : 6 (en0)
`
	resolvers, err := parseNativeResolvers(data)
	if err != nil {
		t.Fatal(err)
	}
	p := managedDNSPlan{port: 15353, resolvers: resolvers}
	base, err := p.defaultResolver()
	if err != nil || len(base.servers) != 2 || base.servers[1].String() != "192.168.1.1" {
		t.Fatal("wrong default resolver", base, err)
	}
	if got := base.upstreams("en0")[0]; got != "udp://[fe80::1%25en0]:53#en0" {
		t.Fatal(got)
	}
	if got := resolvers[1].upstreams("en0")[0]; got != "udp://100.100.100.100:5353#utun4" {
		t.Fatal(got)
	}
	r := routeRequest{managedDNS: &p, DirectDomains: []domainRule{{Domain: "taobao.com", IncludeSubdomains: true}, {Domain: "192.168.2.0/24"}}}
	cfg := makeTransparentConfig(r, "utun99", 19090, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})
	dns := cfg["dns"].(map[string]any)
	policy := dns["nameserver-policy"].(map[string]any)
	if policy["+.corp.example"].([]string)[0] != "udp://100.100.100.100:5353#utun4" || policy["+.taobao.com"] == nil || policy["192.168.2.0/24"] != nil {
		t.Fatal(policy)
	}
	wire, _ := json.Marshal(dns)
	if strings.Contains(string(wire), `"system"`) || dns["fallback"] != nil {
		t.Fatal("system fallback could recurse or leak", string(wire))
	}
	for _, malformed := range []string{strings.Replace(data, "192.168.1.1", "not-an-ip", 1), strings.Replace(data, "5353", "99999", 1)} {
		if _, err := parseNativeResolvers(malformed); err == nil {
			t.Fatal("accepted malformed DNS state")
		}
	}
	if _, err := (managedDNSPlan{}).defaultResolver(); err == nil {
		t.Fatal("missing original resolver silently accepted")
	}
}

func dnsWireAnswer(query []byte, ip [4]byte) []byte {
	var msg dnsmessage.Message
	if msg.Unpack(query) != nil || len(msg.Questions) != 1 {
		return nil
	}
	msg.Response, msg.RecursionAvailable = true, true
	if msg.Questions[0].Type == dnsmessage.TypeA {
		msg.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: msg.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}, Body: &dnsmessage.AResource{A: ip}}}
	}
	wire, _ := msg.Pack()
	return wire
}

func dnsWireDualStackAnswer(query []byte, ip [4]byte, ip6 [16]byte) []byte {
	var msg dnsmessage.Message
	if msg.Unpack(dnsWireAnswer(query, ip)) != nil || len(msg.Questions) != 1 {
		return nil
	}
	if msg.Questions[0].Type == dnsmessage.TypeAAAA {
		msg.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: msg.Questions[0].Name, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET, TTL: 0}, Body: &dnsmessage.AAAAResource{AAAA: ip6}}}
	}
	wire, _ := msg.Pack()
	return wire
}

func queryManagedDNS(port uint16, name, network string) (dnsmessage.Message, error) {
	return queryManagedDNSType(port, name, network, dnsmessage.TypeA)
}

func queryManagedDNSType(port uint16, name, network string, questionType dnsmessage.Type) (dnsmessage.Message, error) {
	var answer dnsmessage.Message
	q := dnsmessage.Message{Header: dnsmessage.Header{ID: 1347, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(name + "."), Type: questionType, Class: dnsmessage.ClassINET}}}
	wire, _ := q.Pack()
	conn, err := net.DialTimeout(network, fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return answer, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	if strings.HasPrefix(network, "tcp") {
		wire = append(binary.BigEndian.AppendUint16(nil, uint16(len(wire))), wire...)
	}
	if _, err = conn.Write(wire); err != nil {
		return answer, err
	}
	var response []byte
	if strings.HasPrefix(network, "tcp") {
		header := make([]byte, 2)
		if _, err = io.ReadFull(conn, header); err != nil {
			return answer, err
		}
		response = make([]byte, binary.BigEndian.Uint16(header))
		_, err = io.ReadFull(conn, response)
	} else {
		response = make([]byte, 4096)
		var n int
		n, err = conn.Read(response)
		response = response[:n]
	}
	if err != nil {
		return answer, err
	}
	if err = answer.Unpack(response); err != nil {
		return answer, err
	}
	if answer.ID != q.ID || !answer.Response || len(answer.Questions) != 1 || answer.Questions[0] != q.Questions[0] {
		return answer, fmt.Errorf("mismatched DNS response")
	}
	return answer, nil
}

func startManagedDNSCore(t *testing.T, cfg map[string]any, port uint16) {
	t.Helper()
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("packaged runtime required")
	}
	cfg["tun"].(map[string]any)["enable"] = false
	dir := t.TempDir()
	wire, _ := json.Marshal(cfg)
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, wire, 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", file)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		logFile.Close()
		if t.Failed() {
			log, _ := os.ReadFile(logFile.Name())
			t.Log(string(log))
		}
	})
	for i := 0; i < 80; i++ {
		if managedDNSReady(port) == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("candidate DNS listener did not answer UDP and TCP")
}

// Real bundled-core test: different answers and counters prove the upstream
// path, including proxy failure. No DNS settings, TUN or routes are changed.
func TestLiveManagedDNSSplitAndFailure(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("packaged runtime required")
	}
	var directQueries, proxyQueries, connects atomic.Int32
	var unavailable atomic.Bool
	direct, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, e := direct.ReadFrom(buf)
			if e != nil {
				return
			}
			directQueries.Add(1)
			direct.WriteTo(dnsWireDualStackAnswer(buf[:n], [4]byte{203, 0, 113, 88}, netip.MustParseAddr("fd12::88").As16()), peer)
		}
	}()
	resolver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyQueries.Add(1)
		if unavailable.Load() {
			http.Error(w, "fixture unavailable", 503)
			return
		}
		var body []byte
		if r.Method == "GET" {
			body, _ = base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
		} else {
			body, _ = io.ReadAll(io.LimitReader(r.Body, 4096))
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(dnsWireDualStackAnswer(body, [4]byte{198, 51, 100, 44}, netip.MustParseAddr("2001:db8::44").As16()))
	}))
	defer resolver.Close()
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&bridge.SOCKSServer{Token: "managed-dns-fixture", Dialer: &dnsFixtureDialer{target: resolver.Listener.Addr().String(), connects: &connects}}).Serve(ctx, upstream)
	port, _ := freePort()
	apiPort, _ := freePort()
	p := &managedDNSPlan{port: port, resolvers: []nativeResolver{{servers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port: direct.LocalAddr().(*net.UDPAddr).Port}}}
	r := routeRequest{RoutingMode: modeGlobal, managedDNS: p, Token: "managed-dns-fixture", SOCKSPort: uint16(upstream.Addr().(*net.TCPAddr).Port), DirectDomains: []domainRule{{Domain: "taobao.com", IncludeSubdomains: true}, {Domain: "exact.example"}}}
	cfg := makeTransparentConfig(r, "utun99", apiPort, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})
	dns := cfg["dns"].(map[string]any)
	dns["nameserver"] = []string{resolver.URL + "/dns-query#GoConnect&skip-cert-verify=true"}
	dns["direct-nameserver"] = dns["nameserver"]
	startManagedDNSCore(t, cfg, port)
	for i, name := range []string{"api.anthropic.com", "api.openai.com", "generativelanguage.googleapis.com", "random.d.ip.net.coffee", "x.exact.example", "not-taobao.com"} {
		network := "udp4"
		if i%2 == 1 {
			network = "tcp4"
		}
		answer, e := queryManagedDNS(port, name, network)
		if e != nil || len(answer.Answers) != 1 {
			t.Fatalf("protected %s: %v %+v", name, e, answer)
		}
		if a, ok := answer.Answers[0].Body.(*dnsmessage.AResource); !ok || a.A != [4]byte{198, 51, 100, 44} {
			t.Fatal("protected name used local DNS", name, answer)
		}
	}
	if directQueries.Load() != 0 || connects.Load() == 0 {
		t.Fatal("public DNS bypassed proxy", directQueries.Load(), connects.Load())
	}
	for _, network := range []string{"udp4", "tcp4"} {
		answer, err := queryManagedDNSType(port, "api.openai.com", network, dnsmessage.TypeAAAA)
		if err != nil || len(answer.Answers) != 1 {
			t.Fatal("global mode lost protected IPv6 DNS", answer, err)
		}
		if a, ok := answer.Answers[0].Body.(*dnsmessage.AAAAResource); !ok || a.AAAA != netip.MustParseAddr("2001:db8::44").As16() || directQueries.Load() != 0 {
			t.Fatal("global AAAA did not use proxied DoH", answer, directQueries.Load())
		}
	}
	for _, name := range []string{"www.taobao.com", "exact.example", "nas.lan", "router.home.arpa", "printer", "10.1.168.192.in-addr.arpa"} {
		answer, e := queryManagedDNS(port, name, "udp4")
		if e != nil || len(answer.Answers) != 1 {
			t.Fatalf("direct %s: %v %+v", name, e, answer)
		}
		if a, ok := answer.Answers[0].Body.(*dnsmessage.AResource); !ok || a.A != [4]byte{203, 0, 113, 88} {
			t.Fatal("local name used proxy", name, answer)
		}
	}
	before := directQueries.Load()
	unavailable.Store(true)
	answer, e := queryManagedDNS(port, "fresh.failure.anthropic.com", "udp4")
	if e == nil && answer.RCode == dnsmessage.RCodeSuccess {
		t.Fatal("protected name succeeded despite failed DoH")
	}
	if directQueries.Load() != before {
		t.Fatal("protected name fell back to local DNS")
	}
	answer, e = queryManagedDNS(port, "still-works.taobao.com", "tcp4")
	if e != nil || len(answer.Answers) != 1 {
		t.Fatal("domestic DNS depends on proxy health", e)
	}
	t.Logf("proxy DNS queries=%d, direct DNS queries=%d, proxy connections=%d; protected failure did not fall back", proxyQueries.Load(), directQueries.Load(), connects.Load())
}

// Run the exact packaged core without changing system DNS or adding routes.
// Synthetic public names must never acquire a local-network IPv6 identity;
// private/native zones must retain real dual-stack answers.
func TestLiveManagedDNSWhitelistBrowserCompatibility(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("packaged runtime required")
	}
	var nativeQueries atomic.Int32
	native, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	private6 := netip.MustParseAddr("fd12:3456::51").As16()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := native.ReadFrom(buf)
			if err != nil {
				return
			}
			nativeQueries.Add(1)
			native.WriteTo(dnsWireDualStackAnswer(buf[:n], [4]byte{10, 0, 0, 51}, private6), peer)
		}
	}()
	resolver := nativeResolver{servers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port: native.LocalAddr().(*net.UDPAddr).Port}
	supplemental := resolver
	supplemental.domain = "private.example"
	port, _ := freePort()
	api, _ := freePort()
	p := &managedDNSPlan{port: port, resolvers: []nativeResolver{resolver, supplemental}}
	r := routeRequest{RoutingMode: modeWhitelist, managedDNS: p, DirectDomains: []domainRule{{Domain: "taobao.com", IncludeSubdomains: true}, {Domain: "exact.example"}}}
	cfg := makeTransparentConfig(r, "utun99", api, originalNetwork{Interface: "lo0"}, nil, tailscaleBypass{})
	// Any accidental upstream request is answered and counted, even when the
	// real proxy is unavailable; NODATA must come from the mapping layer.
	cfg["dns"].(map[string]any)["nameserver"] = resolver.upstreams("lo0")
	startManagedDNSCore(t, cfg, port)
	for _, network := range []string{"udp4", "tcp4"} {
		for _, name := range []string{"yunyoo.cc", "api.openai.com", "cdn.jsdelivr.net", "ipv6-only.public.example"} {
			before := nativeQueries.Load()
			for _, qtype := range []dnsmessage.Type{dnsmessage.TypeAAAA, dnsmessage.TypeHTTPS, dnsmessage.TypeA} {
				answer, err := queryManagedDNSType(port, name, network, qtype)
				if err != nil || answer.RCode != dnsmessage.RCodeSuccess || nativeQueries.Load() != before {
					t.Fatal("shared query escaped deferred DNS", network, name, qtype, answer, err)
				}
				if qtype != dnsmessage.TypeA {
					if len(answer.Answers) != 0 {
						t.Fatal("public name exposed synthetic IPv6 or HTTPS address hints", name, answer)
					}
				} else if len(answer.Answers) != 1 {
					t.Fatal("public name lost deferred IPv4 mapping", name, answer)
				} else if a, ok := answer.Answers[0].Body.(*dnsmessage.AResource); !ok || !netip.MustParsePrefix("198.18.0.0/16").Contains(netip.AddrFrom4(a.A)) {
					t.Fatal("public A record was resolved too early", name, answer)
				}
			}
		}
		for _, name := range []string{"nas.lan", "device.local", "router.home.arpa", "printer", "www.taobao.com", "exact.example", "server.private.example"} {
			answer, err := queryManagedDNSType(port, name, network, dnsmessage.TypeAAAA)
			if err != nil || len(answer.Answers) != 1 {
				t.Fatal("native zone lost IPv6", name, network, answer, err)
			}
			if a, ok := answer.Answers[0].Body.(*dnsmessage.AAAAResource); !ok || a.AAAA != private6 {
				t.Fatal("native zone received a virtual IPv6 address", name, answer)
			}
		}
	}
}

func TestManagedDNSWhitelistAndGlobalPoliciesDiffer(t *testing.T) {
	p := &managedDNSPlan{port: 15353, resolvers: []nativeResolver{
		{servers: []netip.Addr{netip.MustParseAddr("192.0.2.53")}, port: 53},
		{domain: "private.example", servers: []netip.Addr{netip.MustParseAddr("192.0.2.54")}, port: 53},
	}}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r := routeRequest{RoutingMode: mode, managedDNS: p}
		cfg := makeTransparentConfig(r, "utun99", 19090, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})
		dns := cfg["dns"].(map[string]any)
		if mode == modeWhitelist {
			if dns["enhanced-mode"] != "fake-ip" || dns["direct-nameserver"].([]string)[0] != "udp://192.0.2.53:53#en0" {
				t.Fatal("whitelist shares global proxy resolution", dns)
			}
			if dns["fake-ip-range6"] != "" || dns["ipv6"] != true || cfg["ipv6"] != true {
				t.Fatal("disable synthetic AAAA without disabling native IPv6", dns)
			}
			filters := strings.Join(dns["fake-ip-filter"].([]string), ",")
			if !strings.Contains(filters, "+.private.example") || !strings.Contains(filters, "+.local") {
				t.Fatal("native zones lost real DNS answers", filters)
			}
		} else if dns["enhanced-mode"] != "normal" || dns["direct-nameserver"].([]string)[0] != "https://1.1.1.1/dns-query#GoConnect" {
			t.Fatal("global protection changed", dns)
		}
	}
}

type whitelistDNSProxyFixture struct {
	target string
	seen   chan string
}

func (d *whitelistDNSProxyFixture) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.seen <- address
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

// Real core, no TUN: deterministic ingress roles stand for the outcome of the
// existing process rules. This verifies DNS ordering and outbound behavior,
// not macOS process attribution, which requires separate installed-TUN testing.
func TestLiveManagedDNSWhitelistDefersUntilOutboundSelection(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("packaged runtime required")
	}
	var nativeQueries atomic.Int32
	native, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := native.ReadFrom(buf)
			if err != nil {
				return
			}
			nativeQueries.Add(1)
			var answer dnsmessage.Message
			answer.Unpack(dnsWireDualStackAnswer(buf[:n], [4]byte{127, 0, 0, 1}, netip.IPv6Loopback().As16()))
			if answer.Questions[0].Name.String() == "ipv6-only.corp.example." && answer.Questions[0].Type == dnsmessage.TypeA {
				answer.Answers = nil
			}
			wire, _ := answer.Pack()
			native.WriteTo(wire, peer)
		}
	}()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "native") }))
	defer local.Close()
	local6Listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	local6 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "native-ipv6-only") }))
	local6.Listener.Close()
	local6.Listener = local6Listener
	local6.Start()
	defer local6.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "proxy") }))
	defer remote.Close()
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := &whitelistDNSProxyFixture{target: remote.Listener.Addr().String(), seen: make(chan string, 8)}
	go (&bridge.SOCKSServer{Token: "managed-dns-fixture", Dialer: dialer}).Serve(ctx, upstream)
	port, _ := freePort()
	listed, _ := freePort()
	unlisted, _ := freePort()
	api, _ := freePort()
	p := &managedDNSPlan{port: port, resolvers: []nativeResolver{{servers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port: native.LocalAddr().(*net.UDPAddr).Port}}}
	r := routeRequest{RoutingMode: modeWhitelist, managedDNS: p, Token: "managed-dns-fixture", SOCKSPort: uint16(upstream.Addr().(*net.TCPAddr).Port)}
	cfg := makeTransparentConfig(r, "utun99", api, originalNetwork{Interface: "lo0"}, nil, tailscaleBypass{})
	cfg["socks-port"], cfg["mixed-port"] = listed, unlisted
	cfg["rules"] = []string{fmt.Sprintf("IN-PORT,%d,GoConnect", listed), "MATCH,OriginalNetwork"}
	startManagedDNSCore(t, cfg, port)
	answer, err := queryManagedDNS(port, "unconfigured.corp.example", "udp4")
	if err != nil || len(answer.Answers) != 1 {
		t.Fatal("local mapping failed", answer, err)
	}
	address := netip.AddrFrom4(answer.Answers[0].Body.(*dnsmessage.AResource).A)
	if !netip.MustParsePrefix("198.18.0.0/16").Contains(address) || nativeQueries.Load() != 0 {
		t.Fatal("shared lookup went upstream before application routing", address, nativeQueries.Load())
	}
	_, destinationPort, _ := net.SplitHostPort(local.Listener.Addr().String())
	requestAddress := func(ingress uint16, address netip.Addr, destinationPort string) (string, error) {
		d, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", ingress), nil, &net.Dialer{Timeout: time.Second})
		if err != nil {
			return "", err
		}
		client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: d.(proxy.ContextDialer).DialContext, DisableKeepAlives: true}}
		response, err := client.Get("http://" + net.JoinHostPort(address.String(), destinationPort))
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		return string(body), err
	}
	request := func(ingress uint16) (string, error) {
		return requestAddress(ingress, address, destinationPort)
	}
	if body, err := request(listed); err != nil || body != "proxy" || nativeQueries.Load() != 0 {
		t.Fatal("selected application used native DNS", body, err, nativeQueries.Load())
	}
	select {
	case target := <-dialer.seen:
		if target != "unconfigured.corp.example:"+destinationPort {
			t.Fatal("proxy received a fake IP instead of the hostname", target)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not receive the selected connection")
	}
	if body, err := request(unlisted); err != nil || body != "native" || nativeQueries.Load() == 0 {
		t.Fatal("unlisted application did not use original DNS", body, err, nativeQueries.Load())
	}
	// Public AAAA is suppressed only at the shared mapping layer. An unlisted
	// connection must still resolve and reach a native IPv6-only destination.
	answer6, err := queryManagedDNS(port, "ipv6-only.corp.example", "udp4")
	if err != nil || len(answer6.Answers) != 1 {
		t.Fatal("IPv6-only name lost its deferred mapping", answer6, err)
	}
	address6 := netip.AddrFrom4(answer6.Answers[0].Body.(*dnsmessage.AResource).A)
	_, destinationPort6, _ := net.SplitHostPort(local6.Listener.Addr().String())
	if body, err := requestAddress(unlisted, address6, destinationPort6); err != nil || body != "native-ipv6-only" {
		t.Fatal("outbound lost real IPv6 resolution or dialing", body, err)
	}
	before := nativeQueries.Load()
	upstream.Close()
	if body, err := request(listed); err == nil || body == "native" || nativeQueries.Load() != before {
		t.Fatal("failed selected path fell back to native resolution", body, err)
	}
}
