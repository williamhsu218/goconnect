package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
)

type companyFixtureDialer struct{ tcp, udp, dns, business atomic.Int32 }

func (f *companyFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		f.udp.Add(1)
		return nil, errors.New("UDP forbidden")
	}
	f.tcp.Add(1)
	left, right := net.Pipe()
	if address == "10.201.0.53:53" {
		f.dns.Add(1)
		go func() {
			defer right.Close()
			var header [2]byte
			if _, err := io.ReadFull(right, header[:]); err != nil {
				return
			}
			wire := make([]byte, int(binary.BigEndian.Uint16(header[:])))
			if _, err := io.ReadFull(right, wire); err != nil {
				return
			}
			var request dnsmessage.Message
			if request.Unpack(wire) != nil || len(request.Questions) != 1 {
				return
			}
			answer := dnsmessage.Message{Header: dnsmessage.Header{ID: request.ID, Response: true, RecursionAvailable: true}, Questions: request.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: request.Questions[0].Name, Class: dnsmessage.ClassINET, Type: dnsmessage.TypeA, TTL: 5}, Body: &dnsmessage.AResource{A: [4]byte{10, 201, 0, 9}}}}}
			if request.Questions[0].Name.String() == "corp.example.invalid." {
				answer.Answers = nil
				answer.RCode = dnsmessage.RCodeNameError
			}
			reply, _ := answer.Pack()
			binary.BigEndian.PutUint16(header[:], uint16(len(reply)))
			right.Write(header[:])
			right.Write(reply)
		}()
	} else if address == "10.201.0.9:443" {
		f.business.Add(1)
		go func() { defer right.Close(); _, _ = io.Copy(right, right) }()
	} else {
		left.Close()
		right.Close()
		return nil, errors.New("fixture target rejected")
	}
	return left, nil
}
func fixtureCompanyGate(t *testing.T) (*companyGate, companyBackendClaim, *companyFixtureDialer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dir, err := os.MkdirTemp("/private/tmp", "gcc-t-")
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	backend, err := companyTransportListener(filepath.Join(dir, "company.sock"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &companyFixtureDialer{}
	token := strings.Repeat("b", 64)
	go func() {
		_ = (&bridge.SOCKSServer{Token: token, Dialer: companyLiteralDialer{fixture}}).Serve(ctx, backend)
	}()
	started, err := companyProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	claim := companyBackendClaim{SocketPath: filepath.Join(dir, "company.sock"), PID: os.Getpid(), StartTime: started, Token: token, DNSServers: []string{"10.201.0.53"}, RemoteNetworks: []string{"10.201.0.0/16"}}
	gate := &companyGate{ctx: ctx, cancel: cancel, directory: dir, uid: uint32(os.Getuid()), token: strings.Repeat("g", 64), reservation: companyReservation{Networks: []string{"10.201.0.0/16"}, Domains: []string{"corp.example.invalid"}}, state: companyRouteState{State: "blocked"}}
	// Tests use the kernel's UID/PID/start time. Exact signed helper CDHash is an
	// additional production check; this fixture does not claim its acceptance.
	gate.verify = gate.verifyBackend
	gate.listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gate.port = uint16(gate.listener.Addr().(*net.TCPAddr).Port)
	gate.dnsTCP, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gate.dnsPort = uint16(gate.dnsTCP.Addr().(*net.TCPAddr).Port)
	gate.dnsUDP, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(gate.dnsPort)})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = (&bridge.SOCKSServer{Token: gate.token, Dialer: gate}).Serve(ctx, companySOCKSListener{gate.listener})
	}()
	go gate.serveDNSUDP()
	go gate.serveDNSTCP()
	t.Cleanup(func() { gate.Close(); backend.Close(); os.RemoveAll(dir) })
	return gate, claim, fixture
}

const companyTestGeneration = "00000000-0000-4000-8000-000000000001"

func activateCompanyGate(t *testing.T, g *companyGate, claim companyBackendClaim, generation string) {
	t.Helper()
	if err := g.command(context.Background(), companyCommand{ExpectedRevision: g.snapshot().Revision, Generation: generation, Action: "prepare", Backend: &claim}); err != nil {
		t.Fatal(err)
	}
	if err := g.command(context.Background(), companyCommand{ExpectedRevision: g.snapshot().Revision, Generation: generation, Action: "commit"}); err != nil {
		t.Fatal(err)
	}
}
func echoCompany(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	var reply [4]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil || string(reply[:]) != "ping" {
		t.Fatalf("echo: %q %v", reply, err)
	}
}
func TestCompanyGateG0StableAttachBlockReconnect(t *testing.T) {
	g, claim, fixture := fixtureCompanyGate(t)
	// A long-lived primary stream is independent of all company transactions.
	primary, server := net.Pipe()
	defer primary.Close()
	go func() { defer server.Close(); io.Copy(server, server) }()
	front, _ := proxy.SOCKS5("tcp", g.listener.Addr().String(), &proxy.Auth{User: "goconnect", Password: g.token}, nil)
	if _, err := front.Dial("tcp", "10.201.0.9:443"); err == nil {
		t.Fatal("unattached scope escaped")
	}
	tcpAddress, dnsAddress := g.listener.Addr().String(), g.dnsTCP.Addr().String()
	for i := 0; i < 3; i++ {
		generation := companyTestGeneration[:35] + string(rune('1'+i))
		activateCompanyGate(t, g, claim, generation)
		conn, err := front.Dial("tcp", "app.corp.example.invalid:443")
		if err != nil {
			t.Fatal(err)
		}
		echoCompany(t, conn)
		echoCompany(t, primary)
		if _, err = g.DialContext(context.Background(), "tcp", "8.8.8.8:443"); err == nil {
			t.Fatal("public business target escaped")
		}
		if _, err = g.DialContext(context.Background(), "tcp", "10.202.0.1:443"); err == nil {
			t.Fatal("unapproved private target escaped")
		}
		if err = g.command(context.Background(), companyCommand{ExpectedRevision: g.snapshot().Revision, Generation: generation, Action: "block"}); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(time.Second))
		var buf [1]byte
		if _, err = conn.Read(buf[:]); err == nil {
			t.Fatal("old generation retained")
		}
		conn.Close()
		if _, err = g.DialContext(context.Background(), "tcp", "10.201.0.9:443"); err == nil {
			t.Fatal("blocked scope fell back")
		}
		echoCompany(t, primary)
		if g.listener.Addr().String() != tcpAddress || g.dnsTCP.Addr().String() != dnsAddress {
			t.Fatal("stable listener replaced")
		}
	}
	if fixture.udp.Load() != 0 || fixture.dns.Load() < 3 {
		t.Fatal("DNS did not use TCP-only company backend")
	}
}
func TestCompanyGateTransactionsAndEndpointIdentity(t *testing.T) {
	g, claim, _ := fixtureCompanyGate(t)
	bad := claim
	bad.PID++
	if err := g.command(context.Background(), companyCommand{Generation: companyTestGeneration, Action: "prepare", Backend: &bad}); err == nil {
		t.Fatal("wrong process accepted")
	}
	if g.snapshot().State == "active" {
		t.Fatal("failed identity activated")
	}
	activateCompanyGate(t, g, claim, companyTestGeneration)
	state := g.snapshot()
	if err := g.command(context.Background(), companyCommand{ExpectedRevision: state.Revision - 1, Generation: companyTestGeneration, Action: "block"}); err == nil {
		t.Fatal("stale revision accepted")
	}
	if err := g.command(context.Background(), companyCommand{ExpectedRevision: state.Revision, Generation: "00000000-0000-4000-8000-000000000099", Action: "block"}); err == nil {
		t.Fatal("foreign generation accepted")
	}
	if g.snapshot().State != "active" {
		t.Fatal("invalid transaction mutated active backend")
	}
	if err := g.command(context.Background(), companyCommand{ExpectedRevision: state.Revision, Generation: companyTestGeneration, Action: "commit"}); err == nil {
		t.Fatal("duplicate commit accepted")
	}
}
func TestCompanyGateDNSLocalProtocolsAndNoFallback(t *testing.T) {
	g, claim, f := fixtureCompanyGate(t)
	activateCompanyGate(t, g, claim, companyTestGeneration)
	name, _ := dnsmessage.NewName("app.corp.example.invalid.")
	wire, _ := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 44, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}).Pack()
	for _, network := range []string{"udp", "tcp"} {
		conn, err := net.DialTimeout(network, g.dnsTCP.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		request := wire
		if network == "tcp" {
			request = append([]byte{byte(len(wire) >> 8), byte(len(wire))}, wire...)
		}
		conn.Write(request)
		reply := make([]byte, 4096)
		var n int
		if network == "tcp" {
			var h [2]byte
			_, err = io.ReadFull(conn, h[:])
			if err == nil {
				n = int(binary.BigEndian.Uint16(h[:]))
				_, err = io.ReadFull(conn, reply[:n])
			}
		} else {
			n, err = conn.Read(reply)
		}
		conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		var answer dnsmessage.Message
		if answer.Unpack(reply[:n]) != nil || len(answer.Answers) != 1 {
			t.Fatal("missing company answer")
		}
	}
	if err := g.command(context.Background(), companyCommand{ExpectedRevision: g.snapshot().Revision, Generation: companyTestGeneration, Action: "block"}); err != nil {
		t.Fatal(err)
	}
	before := f.dns.Load()
	var blocked dnsmessage.Message
	if blocked.Unpack(g.dnsAnswer(wire)) != nil || blocked.RCode != dnsmessage.RCodeServerFailure {
		t.Fatal("blocked DNS must SERVFAIL")
	}
	if f.dns.Load() != before || f.udp.Load() != 0 {
		t.Fatal("blocked DNS escaped")
	}
}
func TestCompanyReservationPreflight(t *testing.T) {
	reservation := &companyReservation{Networks: []string{"10.201.0.0/16"}, Domains: []string{"corp.example.invalid"}, GatewayHost: "vpn.example.invalid", GatewayPort: 443, GatewayIPs: []string{"198.51.100.4"}}
	r := routeRequest{TransparentRouting: true, Gateway: "198.51.100.3", Company: reservation}
	if err := validateCompanyReservation(r, nil, tailscaleBypass{}); err != nil {
		t.Fatal(err)
	}
	if err := validateCompanyReservation(r, []localNetworkRoute{{Prefix: netip.MustParsePrefix("10.201.1.0/24"), Interface: "en0"}}, tailscaleBypass{}); err == nil {
		t.Fatal("LAN overlap accepted")
	}
	r.DirectDomains = []domainRule{{Domain: "example.invalid", IncludeSubdomains: true}}
	if err := validateCompanyReservation(r, nil, tailscaleBypass{}); err == nil {
		t.Fatal("direct-domain conflict accepted")
	}
}

func TestCompanyGateRejectsServerExclusionsAndRestrictedIncludes(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		g, claim, _ := fixtureCompanyGate(t)
		if restricted {
			claim.RemoteNetworks = nil
		} else {
			claim.RemoteExcluded = []string{"10.201.1.0/24"}
		}
		if err := g.command(context.Background(), companyCommand{Generation: companyTestGeneration, Action: "prepare", Backend: &claim}); err == nil {
			t.Fatal("server split restriction widened")
		}
		if g.snapshot().State != "failed" {
			t.Fatal("rejected scope should stay blocked")
		}
	}
}
