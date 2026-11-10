package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
)

func TestManagedDNSAddressPolicyConfiguration(t *testing.T) {
	r := fakeIPConflictRequest()
	r.AppPaths = []string{"/Applications/Selected.app"}
	r.DirectDomains = []domainRule{{Domain: "192.0.2.0/24"}}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r.RoutingMode = mode
		cfg := makeTransparentConfig(r, "utun99", 19090, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})
		sub := cfg["sub-rules"].(map[string][]string)
		_, ok := sub[nativeAddressRules]
		if ok != (mode == modeWhitelist) {
			t.Fatal("global mode gained policy DNS, or whitelist lost it", cfg)
		}
		if !ok {
			continue
		}
		if sub[nativeAddressRules][len(sub[nativeAddressRules])-1] != "MATCH,OriginalNetwork" ||
			sub[proxyAddressRules][len(sub[proxyAddressRules])-1] != "MATCH,GoConnect" {
			t.Fatal("resolver roles no longer have independent defaults", sub)
		}
		for _, rule := range sub[nativeAddressRules] {
			if strings.HasPrefix(rule, "PROCESS-") {
				t.Fatal("recursive process selection", rule)
			}
		}
	}
}

func TestManagedDNSSearchDomainsRemainNative(t *testing.T) {
	raw := "DNS configuration\nresolver #1\n  search domain[0] : Office.Example.\n  search domain[1] : *\n  search domain[2] : com\n  nameserver[0] : 192.0.2.53\n"
	resolvers, err := parseNativeResolvers(raw)
	if err != nil || len(resolvers) != 1 || len(resolvers[0].searchDomains) != 1 || resolvers[0].searchDomains[0] != "office.example" {
		t.Fatal("native search suffixes were lost or broadened", resolvers, err)
	}
	r := fakeIPConflictRequest()
	r.managedDNS.resolvers = resolvers
	cfg := makeTransparentConfig(r, "utun99", 19090, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})
	dns := cfg["dns"].(map[string]any)
	if dns["nameserver-policy"].(map[string]any)["+.office.example"] == nil ||
		!strings.Contains(strings.Join(dns["fake-ip-filter"].([]string), ","), "+.office.example") {
		t.Fatal("native suffix was sent through deferred public resolution", dns)
	}
}

type addressPolicyDNS struct {
	queries atomic.Int32
	fail    atomic.Bool
	address netip.Addr
	dual    atomic.Bool
	port    int
}

func newAddressPolicyDNS(t *testing.T, address string) *addressPolicyDNS {
	t.Helper()
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tcp.Close() })
	listener, err := net.ListenPacket("udp4", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	f := &addressPolicyDNS{address: netip.MustParseAddr(address), port: listener.LocalAddr().(*net.UDPAddr).Port}
	answer := func(wire []byte) []byte {
		f.queries.Add(1)
		var q dnsmessage.Message
		if q.Unpack(wire) != nil || len(q.Questions) != 1 {
			return nil
		}
		q.Response, q.RecursionAvailable = true, true
		if f.fail.Load() {
			q.RCode = dnsmessage.RCodeServerFailure
		} else if q.Questions[0].Type == dnsmessage.TypeA && f.address.Is4() {
			q.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: f.address.As4()}}}
		} else if q.Questions[0].Type == dnsmessage.TypeAAAA && (f.address.Is6() || f.dual.Load()) {
			ip := f.address
			if f.dual.Load() {
				ip = netip.MustParseAddr("2001:db8:23::42")
			}
			q.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AAAAResource{AAAA: ip.As16()}}}
		}
		wire, _ = q.Pack()
		return wire
	}
	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				for {
					var header [2]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil {
						return
					}
					wire := make([]byte, binary.BigEndian.Uint16(header[:]))
					if _, err := io.ReadFull(conn, wire); err != nil {
						return
					}
					reply := answer(wire)
					conn.Write(binary.BigEndian.AppendUint16(nil, uint16(len(reply))))
					conn.Write(reply)
				}
			}()
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := listener.ReadFrom(buf)
			if err != nil {
				return
			}
			listener.WriteTo(answer(buf[:n]), peer)
		}
	}()
	return f
}

func (f *addressPolicyDNS) upstream() []string {
	return []string{fmt.Sprintf("udp://127.0.0.1:%d", f.port)}
}

type addressPolicyCore struct {
	listed, unlisted, dns uint16
	native, proxied       *addressPolicyDNS
	seen                  map[string]chan string
}

type addressPolicyDialer struct {
	tcp, udp string
	dns      string
	seen     chan string
}

func (d *addressPolicyDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.seen <- address
	target := d.tcp
	if network == "udp" {
		target = d.udp
	} else if address == d.dns {
		target = d.dns
	}
	return (&net.Dialer{}).DialContext(ctx, network, target)
}

// Real sealed-core rules; only process attribution and remote transports are
// represented by deterministic ingress roles and loopback recorders. No TUN.
type addressPolicyOptions struct {
	searchDomains           []string
	proxyDNSOverSOCKS, dual bool
	configure               func(map[string]any)
	nativeProcesses         bool // Native process ownership; never substitute IN-PORT roles.
}

func newAddressPolicyCore(t *testing.T, mode string, r routeRequest, local []localNetworkRoute, nativeIP, proxyIP string, options ...addressPolicyOptions) *addressPolicyCore {
	t.Helper()
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("packaged runtime required")
	}
	f := &addressPolicyCore{native: newAddressPolicyDNS(t, nativeIP), proxied: newAddressPolicyDNS(t, proxyIP), seen: map[string]chan string{}}
	var option addressPolicyOptions
	if len(options) > 0 {
		option = options[0]
	}
	f.native.dual.Store(option.dual)
	f.proxied.dual.Store(option.dual)
	f.dns, _ = freePort()
	f.listed, _ = freePort()
	f.unlisted, _ = freePort()
	api, _ := freePort()
	r.RoutingMode = mode
	r.Token = "address-policy-fixture"
	if !option.nativeProcesses {
		r.AppPaths = []string{"/Applications/PolicyRole.app"}
	}
	r.managedDNS = &managedDNSPlan{port: f.dns, resolvers: []nativeResolver{{servers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port: f.native.port, searchDomains: option.searchDomains}}}
	cfg := makeTransparentConfig(r, "utun99", api, originalNetwork{Interface: "lo0"}, local, tailscaleBypass{})
	dns := cfg["dns"].(map[string]any)
	dns["nameserver"] = f.proxied.upstream()
	if option.proxyDNSOverSOCKS {
		dns["nameserver"] = []string{fmt.Sprintf("tcp://127.0.0.1:%d#GoConnect", f.proxied.port)}
	}
	if mode == modeWhitelist {
		dns["direct-nameserver"] = f.native.upstream()
	} else {
		dns["direct-nameserver"] = f.proxied.upstream()
	}
	var proxies []map[string]any
	for _, name := range []string{"GoConnect", "OriginalNetwork", "Local0"} {
		name := name
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, name) }))
		t.Cleanup(backend.Close)
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { udp.Close() })
		go func() {
			buf := make([]byte, 4096)
			for {
				n, peer, err := udp.ReadFrom(buf)
				if err != nil {
					return
				}
				udp.WriteTo(append([]byte(name+":"), buf[:n]...), peer)
			}
		}()
		dialer := &addressPolicyDialer{tcp: backend.Listener.Addr().String(), udp: udp.LocalAddr().String(), seen: make(chan string, 16)}
		if name == "GoConnect" && option.proxyDNSOverSOCKS {
			dialer.dns = fmt.Sprintf("127.0.0.1:%d", f.proxied.port)
		}
		f.seen[name] = dialer.seen
		go (&bridge.SOCKSServer{Token: r.Token, Dialer: dialer}).Serve(ctx, listener)
		proxies = append(proxies, map[string]any{"name": name, "type": "socks5", "server": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "username": "goconnect", "password": r.Token, "udp": true})
	}
	for _, p := range cfg["proxies"].([]map[string]any) {
		if p["type"] == "rematch" {
			proxies = append(proxies, p)
		}
	}
	cfg["proxies"] = proxies
	cfg["socks-port"] = f.listed
	cfg["mixed-port"] = f.unlisted
	rules := cfg["rules"].([]string)
	if option.nativeProcesses {
		cfg["log-level"] = "debug"
	}
	for i, rule := range rules {
		if option.nativeProcesses {
			break
		}
		if strings.HasPrefix(rule, "PROCESS-PATH-REGEX,") {
			_, payload, _ := strings.Cut(rule, ",")
			_, target, _ := strings.Cut(payload, ",")
			port := f.listed
			if mode == modeGlobal {
				port = f.unlisted
			}
			rules[i] = fmt.Sprintf("IN-PORT,%d,%s", port, target)
		}
	}
	if option.configure != nil {
		option.configure(cfg)
	}
	startManagedDNSCore(t, cfg, f.dns)
	return f
}

func (f *addressPolicyCore) query(t *testing.T, name string) string {
	t.Helper()
	answer, err := queryManagedDNS(f.dns, name, "udp4")
	if err != nil || len(answer.Answers) != 1 {
		t.Fatal("DNS mapping failed", answer, err)
	}
	a, ok := answer.Answers[0].Body.(*dnsmessage.AResource)
	if !ok {
		t.Fatal("expected A answer")
	}
	return netip.AddrFrom4(a.A).String()
}

func (f *addressPolicyCore) request(port uint16, address string) (string, error) {
	d, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, &net.Dialer{Timeout: time.Second})
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DialContext: d.(proxy.ContextDialer).DialContext, DisableKeepAlives: true}}
	r, err := client.Get("http://" + net.JoinHostPort(address, "8080"))
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	return string(b), err
}

func TestLiveManagedDNSAddressPolicies(t *testing.T) {
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		for _, scenario := range []string{"cidr-domain", "cidr-literal", "direct-domain", "lan-domain", "lan-literal", "remote-domain", "remote-literal", "listed-public", "unlisted-public"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				r := routeRequest{DirectDomains: []domainRule{{Domain: "198.51.100.0/24"}}}
				var local []localNetworkRoute
				nativeIP, proxyIP := "198.51.100.42", "198.51.100.42"
				expected := "OriginalNetwork"
				if strings.HasPrefix(scenario, "lan-") {
					local = []localNetworkRoute{{Interface: "lo0", Prefix: netip.MustParsePrefix("10.23.0.0/24")}}
					nativeIP, proxyIP = "10.23.0.42", "10.23.0.42"
					expected = "Local0"
				}
				if strings.HasPrefix(scenario, "remote-") {
					r.RemoteNetworks = []string{"10.23.0.0/24"}
					nativeIP, proxyIP = "10.23.0.42", "10.23.0.42"
					expected = "GoConnect"
				}
				if scenario == "direct-domain" {
					r.DirectDomains = append(r.DirectDomains, domainRule{Domain: "service.policy.example"})
				}
				if strings.HasSuffix(scenario, "public") {
					nativeIP, proxyIP = "192.0.2.42", "203.0.113.42"
					if scenario == "listed-public" {
						expected = "GoConnect"
					}
				}
				f := newAddressPolicyCore(t, mode, r, local, nativeIP, proxyIP)
				port := f.listed
				unlisted := scenario == "unlisted-public" || strings.HasPrefix(scenario, "remote-")
				if unlisted {
					port = f.unlisted
				}
				address := proxyIP
				literal := strings.HasSuffix(scenario, "literal")
				if !literal {
					address = f.query(t, "service.policy.example")
				}
				body, err := f.request(port, address)
				if err != nil || body != expected {
					t.Fatal("wrong policy exit", body, expected, err)
				}
				if literal {
					if f.native.queries.Load() != 0 || f.proxied.queries.Load() != 0 {
						t.Fatal("literal IP triggered DNS")
					}
					return
				}
				nativeDNS := scenario == "direct-domain" || (mode == modeWhitelist && unlisted)
				if nativeDNS {
					if f.native.queries.Load() == 0 || f.proxied.queries.Load() != 0 {
						t.Fatal("native role escaped to proxy DNS", f.native.queries.Load(), f.proxied.queries.Load())
					}
				} else if f.proxied.queries.Load() == 0 || f.native.queries.Load() != 0 {
					t.Fatal("selected/global DNS escaped to native", f.native.queries.Load(), f.proxied.queries.Load())
				}
				if mode == modeWhitelist && scenario != "direct-domain" {
					select {
					case target := <-f.seen[body]:
						ip := proxyIP
						if nativeDNS {
							ip = nativeIP
						}
						if target != net.JoinHostPort(ip, "8080") {
							t.Fatal("outbound re-resolved instead of pinning matched address", target, ip)
						}
					case <-time.After(time.Second):
						t.Fatal("missing outbound receipt")
					}
				}
			})
		}
	}
}

func TestLiveManagedDNSAddressResolverCacheIsolationAndFailure(t *testing.T) {
	r := routeRequest{DirectDomains: []domainRule{{Domain: "198.51.100.0/24"}}}
	f := newAddressPolicyCore(t, modeWhitelist, r, nil, "198.51.100.42", "203.0.113.42")
	address := f.query(t, "same.policy.example")
	if body, err := f.request(f.listed, address); err != nil || body != "GoConnect" || f.native.queries.Load() != 0 {
		t.Fatal("listed DNS used native source", body, err)
	}
	before := f.proxied.queries.Load()
	if body, err := f.request(f.unlisted, address); err != nil || body != "OriginalNetwork" || f.native.queries.Load() == 0 || f.proxied.queries.Load() != before {
		t.Fatal("native resolver reused proxy cache", body, err)
	}
	f.proxied.fail.Store(true)
	address = f.query(t, "proxy-failure.policy.example")
	nativeBefore := f.native.queries.Load()
	if body, err := f.request(f.listed, address); err == nil || body != "" || f.native.queries.Load() != nativeBefore {
		t.Fatal("proxy DNS failure fell back", body, err)
	}
	f.native.fail.Store(true)
	address = f.query(t, "native-failure.policy.example")
	proxyBefore := f.proxied.queries.Load()
	if body, err := f.request(f.unlisted, address); err == nil || body != "" || f.proxied.queries.Load() != proxyBefore {
		t.Fatal("native DNS failure changed resolver", body, err)
	}
}

func (f *addressPolicyCore) requestUDP(t *testing.T, port uint16, address string, secondAddress ...string) (string, error) {
	t.Helper()
	control, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return "", err
	}
	defer control.Close()
	control.SetDeadline(time.Now().Add(3 * time.Second))
	control.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err = io.ReadFull(control, reply); err != nil || reply[1] != 0 {
		return "", fmt.Errorf("UDP auth %v %v", reply, err)
	}
	control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	header := make([]byte, 4)
	if _, err = io.ReadFull(control, header); err != nil {
		return "", err
	}
	if header[1] != 0 {
		return "", fmt.Errorf("UDP associate rejected: %v", header)
	}
	length := 6
	if header[3] == 4 {
		length = 18
	} else if header[3] != 1 {
		return "", fmt.Errorf("unexpected UDP relay address type: %d", header[3])
	}
	bound := make([]byte, length)
	if _, err = io.ReadFull(control, bound); err != nil {
		return "", err
	}
	// The test relay is always loopback even if it advertises a wildcard bind.
	relay := net.JoinHostPort("127.0.0.1", fmt.Sprint(binary.BigEndian.Uint16(bound[len(bound)-2:])))
	packet, err := net.DialTimeout("udp4", relay, time.Second)
	if err != nil {
		return "", err
	}
	defer packet.Close()
	packet.SetDeadline(time.Now().Add(3 * time.Second))
	ip := netip.MustParseAddr(address)
	frame := []byte{0, 0, 0, 1}
	frame = append(frame, ip.AsSlice()...)
	frame = binary.BigEndian.AppendUint16(frame, 8080)
	frame = append(frame, []byte("policy-datagram")...)
	buf := make([]byte, 4096)
	var body, firstBody string
	// Exercise natTable's subsequent-packet path within the SAME association.
	for i := 0; i < 8; i++ {
		if i == 4 && len(secondAddress) != 0 {
			copy(frame[4:8], netip.MustParseAddr(secondAddress[0]).AsSlice())
			firstBody, body = body, ""
		}
		if _, err = packet.Write(frame); err != nil {
			return "", err
		}
		n, err := packet.Read(buf)
		if err != nil {
			return "", err
		}
		if n < 10 || buf[2] != 0 {
			return "", fmt.Errorf("invalid UDP response")
		}
		offset := 10
		if buf[3] == 4 {
			offset = 22
		} else if buf[3] != 1 {
			return "", fmt.Errorf("unexpected UDP response type")
		}
		if n < offset {
			return "", io.ErrUnexpectedEOF
		}
		if body != "" && body != string(buf[offset:n]) {
			return "", fmt.Errorf("UDP mapping changed mid-session")
		}
		body = string(buf[offset:n])
	}
	if firstBody != "" {
		return firstBody + "|" + body, nil
	}
	return body, nil
}

func TestLiveManagedDNSAddressPolicyUDPDestinationIsolation(t *testing.T) {
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		t.Run(mode, func(t *testing.T) {
			r := routeRequest{DirectDomains: []domainRule{{Domain: "198.51.100.0/24"}}}
			f := newAddressPolicyCore(t, mode, r, nil, "203.0.113.42", "203.0.113.42")
			address := f.query(t, "udp.public.example")
			body, err := f.requestUDP(t, f.listed, address, "198.51.100.42")
			if err != nil || body != "GoConnect:policy-datagram|OriginalNetwork:policy-datagram" {
				t.Fatal("UDP association reused previous destination's policy", body, err)
			}
		})
	}
}

func TestLiveManagedDNSAddressPoliciesUDP(t *testing.T) {
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		t.Run(mode, func(t *testing.T) {
			r := routeRequest{RemoteNetworks: []string{"10.23.0.0/24"}}
			f := newAddressPolicyCore(t, mode, r, nil, "10.23.0.42", "10.23.0.42")
			address := f.query(t, "udp.policy.example")
			body, err := f.requestUDP(t, f.unlisted, address)
			if err != nil || body != "GoConnect:policy-datagram" {
				t.Fatal("UDP policy exit", body, err)
			}
			if mode == modeWhitelist {
				if f.native.queries.Load() == 0 || f.proxied.queries.Load() != 0 {
					t.Fatal("UDP native DNS boundary")
				}
				select {
				case target := <-f.seen["GoConnect"]:
					if target != "10.23.0.42:8080" {
						t.Fatal("UDP did not pin matched IP", target)
					}
				case <-time.After(time.Second):
					t.Fatal("missing UDP exit")
				}
			} else if f.proxied.queries.Load() == 0 || f.native.queries.Load() != 0 {
				t.Fatal("UDP global DNS boundary")
			}
		})
	}
}

func TestLiveManagedDNSAddressPolicyIPv6(t *testing.T) {
	for _, listed := range []bool{true, false} {
		t.Run(fmt.Sprint(listed), func(t *testing.T) {
			r := routeRequest{DirectDomains: []domainRule{{Domain: "2001:db8:23::/64"}}}
			f := newAddressPolicyCore(t, modeWhitelist, r, nil, "2001:db8:23::42", "2001:db8:23::42")
			address := f.query(t, "ipv6-only.policy.example")
			port := f.listed
			if !listed {
				port = f.unlisted
			}
			if body, err := f.request(port, address); err != nil || body != "OriginalNetwork" {
				t.Fatal("IPv6 policy lost", body, err)
			}
			if listed {
				if f.proxied.queries.Load() == 0 || f.native.queries.Load() != 0 {
					t.Fatal("IPv6 proxy DNS boundary")
				}
			} else if f.native.queries.Load() == 0 || f.proxied.queries.Load() != 0 {
				t.Fatal("IPv6 native DNS boundary")
			}
			select {
			case target := <-f.seen["OriginalNetwork"]:
				if target != "[2001:db8:23::42]:8080" {
					t.Fatal("IPv6 was not pinned", target)
				}
			case <-time.After(time.Second):
				t.Fatal("missing IPv6 exit")
			}
		})
	}
}

func TestLiveManagedDNSAddressPolicyPreservesTLSAndPinsDirectIP(t *testing.T) {
	r := routeRequest{DirectDomains: []domainRule{{Domain: "192.0.2.0/24"}}}
	// A wrong direct re-resolution remains loopback and is counted; no fixture
	// ever dials a real LAN/public destination when this regression fails.
	f := newAddressPolicyCore(t, modeWhitelist, r, nil, "127.0.0.2", "127.0.0.1")
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.ServerName == "" {
			http.Error(w, "missing original SNI", 400)
			return
		}
		io.WriteString(w, "tls-pinned")
	}))
	defer backend.Close()
	certificate := backend.Certificate()
	if len(certificate.DNSNames) == 0 {
		t.Fatal("test certificate has no DNS name")
	}
	name := certificate.DNSNames[0]
	address := f.query(t, name)
	_, targetPort, _ := net.SplitHostPort(backend.Listener.Addr().String())
	d, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", f.listed), nil, &net.Dialer{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: d.(proxy.ContextDialer).DialContext, DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: name}}}
	response, err := client.Get("https://" + net.JoinHostPort(address, targetPort))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "tls-pinned" || f.native.queries.Load() != 0 || f.proxied.queries.Load() == 0 {
		t.Fatal("TLS or pinned direct DNS boundary failed", string(body), err, f.native.queries.Load(), f.proxied.queries.Load())
	}
}

func TestLiveManagedDNSAddressPolicyDNSCarrierAndSearchSuffix(t *testing.T) {
	r := routeRequest{DirectDomains: []domainRule{{Domain: "198.51.100.0/24"}}}
	f := newAddressPolicyCore(t, modeWhitelist, r,
		[]localNetworkRoute{{Interface: "lo0", Prefix: netip.MustParsePrefix("10.23.0.0/24")}},
		"10.23.0.42", "203.0.113.42", addressPolicyOptions{searchDomains: []string{"office.example"}, proxyDNSOverSOCKS: true})
	address := f.query(t, "portal.office.example")
	if address != "10.23.0.42" {
		t.Fatal("native search suffix became fake/proxied", address)
	}
	if body, err := f.request(f.listed, address); err != nil || body != "Local0" || f.proxied.queries.Load() != 0 {
		t.Fatal("native suffix lost LAN path", body, err)
	}
	nativeBefore := f.native.queries.Load()
	address = f.query(t, "ai.public.example")
	if body, err := f.request(f.listed, address); err != nil || body != "GoConnect" || f.native.queries.Load() != nativeBefore || f.proxied.queries.Load() == 0 {
		t.Fatal("public selected DNS escaped its SOCKS carrier", body, err)
	}
	dnsSeen, businessSeen := false, false
	for len(f.seen["GoConnect"]) > 0 {
		target := <-f.seen["GoConnect"]
		dnsSeen = dnsSeen || target == fmt.Sprintf("127.0.0.1:%d", f.proxied.port)
		businessSeen = businessSeen || target == "203.0.113.42:8080"
	}
	if !dnsSeen || !businessSeen {
		t.Fatal("missing actual proxy DNS/business receipts", dnsSeen, businessSeen)
	}
}

func TestLiveManagedDNSAddressPolicyHostsAndDualStack(t *testing.T) {
	for _, hosts := range []bool{true, false} {
		t.Run(fmt.Sprint(hosts), func(t *testing.T) {
			r := routeRequest{DirectDomains: []domainRule{{Domain: "198.51.100.0/24"}, {Domain: "2001:db8:23::/64"}}}
			option := addressPolicyOptions{dual: true}
			if hosts {
				option.configure = func(cfg map[string]any) { cfg["hosts"].(map[string]any)["host.policy.example"] = "198.51.100.42" }
			}
			f := newAddressPolicyCore(t, modeWhitelist, r, nil, "198.51.100.42", "198.51.100.42", option)
			for _, port := range []uint16{f.listed, f.unlisted} {
				address := f.query(t, "host.policy.example")
				if body, err := f.request(port, address); err != nil || body != "OriginalNetwork" {
					t.Fatal("hosts/dual-stack policy failed", body, err)
				}
				select {
				case target := <-f.seen["OriginalNetwork"]:
					if target != "198.51.100.42:8080" && target != "[2001:db8:23::42]:8080" {
						t.Fatal("hosts/dual-stack target was not pinned", target)
					}
				case <-time.After(time.Second):
					t.Fatal("missing hosts/dual-stack exit")
				}
			}
			if hosts && (f.native.queries.Load() != 0 || f.proxied.queries.Load() != 0) {
				t.Fatal("hosts unexpectedly queried external DNS")
			}
		})
	}
}

func TestLiveManagedDNSAddressPolicyCompanyFailClosed(t *testing.T) {
	for _, listed := range []bool{true, false} {
		t.Run(fmt.Sprint(listed), func(t *testing.T) {
			gate, _, _ := fixtureCompanyGate(t) // Private blocked gate; never installs system DNS.
			r := routeRequest{Company: &gate.reservation}
			f := newAddressPolicyCore(t, modeWhitelist, r, nil, "10.201.0.9", "10.201.0.9", addressPolicyOptions{configure: gate.configure})
			address := f.query(t, "alias.public.example")
			port := f.listed
			if !listed {
				port = f.unlisted
			}
			if body, err := f.request(port, address); err == nil || body != "" {
				t.Fatal("resolved company IP escaped blocked gate", body, err)
			}
			for name, seen := range f.seen {
				if len(seen) != 0 {
					t.Fatal("company failure fell back", name)
				}
			}
		})
	}
}

func TestLiveManagedDNSAddressPolicyUDPCompanyDestinationFailClosed(t *testing.T) {
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		t.Run(mode, func(t *testing.T) {
			gate, _, _ := fixtureCompanyGate(t)
			r := routeRequest{Company: &gate.reservation}
			f := newAddressPolicyCore(t, mode, r, nil, "203.0.113.42", "203.0.113.42", addressPolicyOptions{configure: gate.configure})
			address := f.query(t, "udp.public.example")
			if body, err := f.requestUDP(t, f.listed, address, "10.201.0.9"); err == nil {
				t.Fatal("company UDP reused prior public exit", body)
			}
			publicSeen := false
			for name, seen := range f.seen {
				for len(seen) > 0 {
					target := <-seen
					if name != "GoConnect" || target != "203.0.113.42:8080" {
						t.Fatal("company datagram escaped blocked gate", name, target)
					}
					publicSeen = true
				}
			}
			if !publicSeen {
				t.Fatal("initial public UDP control did not reach its exit")
			}
		})
	}
}
