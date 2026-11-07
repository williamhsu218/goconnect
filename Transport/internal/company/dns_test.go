package company

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

type dialCall struct {
	network string
	address string
}

type memoryDNSServer struct {
	mu      sync.Mutex
	calls   []dialCall
	handler func(callIndex int, req *dnsmessage.Message, rawFrame []byte) ([]byte, error)
}

func (m *memoryDNSServer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	m.mu.Lock()
	idx := len(m.calls)
	m.calls = append(m.calls, dialCall{network: network, address: address})
	handler := m.handler
	m.mu.Unlock()

	if network != "tcp" {
		return nil, fmt.Errorf("forbidden non-tcp network %q", network)
	}
	if handler == nil {
		return nil, errors.New("no handler configured")
	}

	clientConn, serverConn := net.Pipe()
	go func() {
		defer serverConn.Close()

		var lenBuf [2]byte
		if _, err := io.ReadFull(serverConn, lenBuf[:]); err != nil {
			return
		}
		qLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		qBuf := make([]byte, qLen)
		if _, err := io.ReadFull(serverConn, qBuf); err != nil {
			return
		}

		var req dnsmessage.Message
		_ = req.Unpack(qBuf)

		respFrame, err := handler(idx, &req, qBuf)
		if err != nil || len(respFrame) == 0 {
			return
		}
		_, _ = serverConn.Write(respFrame)
	}()

	return clientConn, nil
}

func (m *memoryDNSServer) recordedCalls() []dialCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]dialCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func frameDNSMessage(t *testing.T, msg dnsmessage.Message) []byte {
	t.Helper()
	packed, err := msg.Pack()
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}
	frame := make([]byte, 2+len(packed))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(packed)))
	copy(frame[2:], packed)
	return frame
}

func mustName(name string) dnsmessage.Name {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		panic(err)
	}
	return n
}

func TestResolveTCPOnlyUniqueAndDNSExceptionNotBusinessAccess(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(Policy{
		Networks:   []string{"10.10.0.0/16"},
		Domains:    []string{"corp.example.com"},
		DNSServers: []string{"198.51.100.53"},
	})
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}

	// Verify DNS server outside approved CIDRs is never allowed as a business destination IP.
	dnsIP := netip.MustParseAddr("198.51.100.53")
	if p.AllowsIP(dnsIP) {
		t.Fatal("DNS server IP outside CIDR must not be allowed by AllowsIP")
	}

	srv := &memoryDNSServer{
		handler: func(_ int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			resp := dnsmessage.Message{
				Header: dnsmessage.Header{
					ID:       req.Header.ID,
					Response: true,
					RCode:    dnsmessage.RCodeSuccess,
				},
				Questions: req.Questions,
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeA,
							Class: dnsmessage.ClassINET,
							TTL:   120,
						},
						Body: &dnsmessage.AResource{A: [4]byte{10, 10, 1, 20}},
					},
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeA,
							Class: dnsmessage.ClassINET,
							TTL:   120,
						},
						Body: &dnsmessage.AResource{A: [4]byte{10, 10, 1, 20}},
					},
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeA,
							Class: dnsmessage.ClassINET,
							TTL:   120,
						},
						Body: &dnsmessage.AResource{A: [4]byte{10, 10, 1, 21}},
					},
				},
			}
			return frameDNSMessage(t, resp), nil
		},
	}

	addrs, err := p.Resolve(context.Background(), srv, "App.Corp.Example.COM.")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(addrs) != 2 || addrs[0] != netip.MustParseAddr("10.10.1.20") || addrs[1] != netip.MustParseAddr("10.10.1.21") {
		t.Fatalf("unexpected resolved addresses: %v", addrs)
	}

	calls := srv.recordedCalls()
	if len(calls) != 1 || calls[0].network != "tcp" || calls[0].address != "198.51.100.53:53" {
		t.Fatalf("expected exactly 1 tcp dial to 198.51.100.53:53, got %+v", calls)
	}

	// If an A record resolves to the external DNS server's own IP (198.51.100.53), Resolve must reject it.
	srvDNSIPRecord := &memoryDNSServer{
		handler: func(_ int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
				Questions: req.Questions,
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeA,
							Class: dnsmessage.ClassINET,
							TTL:   60,
						},
						Body: &dnsmessage.AResource{A: [4]byte{198, 51, 100, 53}},
					},
				},
			}
			return frameDNSMessage(t, resp), nil
		},
	}
	if _, err := p.Resolve(context.Background(), srvDNSIPRecord, "dns.corp.example.com"); err == nil {
		t.Fatal("expected A record pointing to external DNS server IP to be rejected")
	}
}

func TestResolveCNAMEChainBoundsScopeAndLoops(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(Policy{
		Networks:   []string{"10.0.0.0/24"},
		Domains:    []string{"corp.example.com"},
		DNSServers: []string{"10.0.0.53"},
	})
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}

	// 1. Valid 8-hop CNAME chain across multiple TCP queries to the SAME resolver succeeds.
	srvMax8 := &memoryDNSServer{
		handler: func(callIdx int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			qname := req.Questions[0].Name
			var answers []dnsmessage.Resource
			if callIdx < 8 {
				next := mustName(fmt.Sprintf("hop%d.corp.example.com.", callIdx+1))
				answers = append(answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{
						Name:  qname,
						Type:  dnsmessage.TypeCNAME,
						Class: dnsmessage.ClassINET,
						TTL:   60,
					},
					Body: &dnsmessage.CNAMEResource{CNAME: next},
				})
			} else {
				answers = append(answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{
						Name:  qname,
						Type:  dnsmessage.TypeA,
						Class: dnsmessage.ClassINET,
						TTL:   60,
					},
					Body: &dnsmessage.AResource{A: [4]byte{10, 0, 0, 42}},
				})
			}
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
				Questions: req.Questions,
				Answers:   answers,
			}
			return frameDNSMessage(t, resp), nil
		},
	}

	addrs, err := p.Resolve(context.Background(), srvMax8, "hop0.corp.example.com")
	if err != nil {
		t.Fatalf("expected 8-hop CNAME chain to succeed, got: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != netip.MustParseAddr("10.0.0.42") {
		t.Fatalf("unexpected addresses: %v", addrs)
	}
	for _, c := range srvMax8.recordedCalls() {
		if c.network != "tcp" || c.address != "10.0.0.53:53" {
			t.Fatalf("CNAME hop did not use same TCP resolver: %+v", c)
		}
	}

	// 2. 9-hop CNAME chain exceeds max8 and must fail.
	srvOver8 := &memoryDNSServer{
		handler: func(callIdx int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			next := mustName(fmt.Sprintf("over%d.corp.example.com.", callIdx+1))
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
				Questions: req.Questions,
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeCNAME,
							Class: dnsmessage.ClassINET,
							TTL:   60,
						},
						Body: &dnsmessage.CNAMEResource{CNAME: next},
					},
				},
			}
			return frameDNSMessage(t, resp), nil
		},
	}
	if _, err := p.Resolve(context.Background(), srvOver8, "over0.corp.example.com"); err == nil {
		t.Fatal("expected >8 CNAME hops to be rejected")
	}

	// 3. CNAME target leaving approved domain scope must be rejected even if its IP is RFC1918 approved.
	srvOutScopeCNAME := &memoryDNSServer{
		handler: func(_ int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			externalName := mustName("cdn.external.org.")
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
				Questions: req.Questions,
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeCNAME,
							Class: dnsmessage.ClassINET,
							TTL:   60,
						},
						Body: &dnsmessage.CNAMEResource{CNAME: externalName},
					},
					{
						Header: dnsmessage.ResourceHeader{
							Name:  externalName,
							Type:  dnsmessage.TypeA,
							Class: dnsmessage.ClassINET,
							TTL:   60,
						},
						Body: &dnsmessage.AResource{A: [4]byte{10, 0, 0, 10}},
					},
				},
			}
			return frameDNSMessage(t, resp), nil
		},
	}
	if _, err := p.Resolve(context.Background(), srvOutScopeCNAME, "web.corp.example.com"); err == nil {
		t.Fatal("expected out-of-scope CNAME target to be rejected even with private IP")
	}

	// 4. CNAME loop must be rejected.
	srvLoop := &memoryDNSServer{
		handler: func(_ int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			a := mustName("a.corp.example.com.")
			b := mustName("b.corp.example.com.")
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
				Questions: req.Questions,
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{Name: a, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60},
						Body:   &dnsmessage.CNAMEResource{CNAME: b},
					},
					{
						Header: dnsmessage.ResourceHeader{Name: b, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60},
						Body:   &dnsmessage.CNAMEResource{CNAME: a},
					},
				},
			}
			return frameDNSMessage(t, resp), nil
		},
	}
	if _, err := p.Resolve(context.Background(), srvLoop, "a.corp.example.com"); err == nil {
		t.Fatal("expected CNAME loop to be rejected")
	}
}

func TestResolveRejectsMalformedMismatchedAndOutOfScopeResponses(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(Policy{
		Networks:   []string{"10.0.0.0/24"},
		Domains:    []string{"corp.example.com"},
		DNSServers: []string{"10.0.0.53"},
	})
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}

	cases := []struct {
		name    string
		handler func(req *dnsmessage.Message) []byte
	}{
		{
			name: "mixed approved and unapproved IP rejected entirely",
			handler: func(req *dnsmessage.Message) []byte {
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
					Questions: req.Questions,
					Answers: []dnsmessage.Resource{
						{
							Header: dnsmessage.ResourceHeader{Name: req.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30},
							Body:   &dnsmessage.AResource{A: [4]byte{10, 0, 0, 5}},
						},
						{
							Header: dnsmessage.ResourceHeader{Name: req.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30},
							Body:   &dnsmessage.AResource{A: [4]byte{8, 8, 8, 8}},
						},
					},
				})
			},
		},
		{
			name: "mismatched ID rejected",
			handler: func(req *dnsmessage.Message) []byte {
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID + 1, Response: true},
					Questions: req.Questions,
					Answers: []dnsmessage.Resource{
						{
							Header: dnsmessage.ResourceHeader{Name: req.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30},
							Body:   &dnsmessage.AResource{A: [4]byte{10, 0, 0, 5}},
						},
					},
				})
			},
		},
		{
			name: "mismatched question name rejected",
			handler: func(req *dnsmessage.Message) []byte {
				other := mustName("other.corp.example.com.")
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
					Questions: []dnsmessage.Question{{Name: other, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
					Answers: []dnsmessage.Resource{
						{
							Header: dnsmessage.ResourceHeader{Name: other, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30},
							Body:   &dnsmessage.AResource{A: [4]byte{10, 0, 0, 5}},
						},
					},
				})
			},
		},
		{
			name: "truncated flag rejected",
			handler: func(req *dnsmessage.Message) []byte {
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID, Response: true, Truncated: true},
					Questions: req.Questions,
					Answers: []dnsmessage.Resource{
						{
							Header: dnsmessage.ResourceHeader{Name: req.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30},
							Body:   &dnsmessage.AResource{A: [4]byte{10, 0, 0, 5}},
						},
					},
				})
			},
		},
		{
			name: "NXDOMAIN rejected",
			handler: func(req *dnsmessage.Message) []byte {
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID, Response: true, RCode: dnsmessage.RCodeNameError},
					Questions: req.Questions,
				})
			},
		},
		{
			name: "SERVFAIL rejected",
			handler: func(req *dnsmessage.Message) []byte {
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID, Response: true, RCode: dnsmessage.RCodeServerFailure},
					Questions: req.Questions,
				})
			},
		},
		{
			name: "multiple questions in response rejected",
			handler: func(req *dnsmessage.Message) []byte {
				return frameDNSMessage(t, dnsmessage.Message{
					Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
					Questions: []dnsmessage.Question{req.Questions[0], req.Questions[0]},
					Answers: []dnsmessage.Resource{
						{
							Header: dnsmessage.ResourceHeader{Name: req.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30},
							Body:   &dnsmessage.AResource{A: [4]byte{10, 0, 0, 5}},
						},
					},
				})
			},
		},
		{
			name: "malformed wire bytes rejected",
			handler: func(_ *dnsmessage.Message) []byte {
				return []byte{0x00, 0x0f, 0xff, 0xff, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d}
			},
		},
		{
			name: "oversize TCP frame rejected",
			handler: func(_ *dnsmessage.Message) []byte {
				buf := make([]byte, 2+5000)
				binary.BigEndian.PutUint16(buf[:2], 5000)
				return buf
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := &memoryDNSServer{
				handler: func(_ int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
					return tc.handler(req), nil
				},
			}
			if _, err := p.Resolve(context.Background(), srv, "app.corp.example.com"); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestAnswerLocalDNSListenerSemantics(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(Policy{
		Networks:   []string{"10.0.0.0/24"},
		Domains:    []string{"corp.example.com"},
		DNSServers: []string{"10.0.0.53"},
	})
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}

	srv := &memoryDNSServer{
		handler: func(_ int, req *dnsmessage.Message, _ []byte) ([]byte, error) {
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: req.Header.ID, Response: true},
				Questions: req.Questions,
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{
							Name:  req.Questions[0].Name,
							Type:  dnsmessage.TypeA,
							Class: dnsmessage.ClassINET,
							TTL:   3600,
						},
						Body: &dnsmessage.AResource{A: [4]byte{10, 0, 0, 99}},
					},
				},
			}
			return frameDNSMessage(t, resp), nil
		},
	}

	// 1. Approved AAAA query -> immediate NODATA (RCodeSuccess, 0 answers) with NO upstream dial.
	aaaaQuery, err := (&dnsmessage.Message{
		Header: dnsmessage.Header{ID: 0x1234, RecursionDesired: true},
		Questions: []dnsmessage.Question{
			{Name: mustName("app.corp.example.com."), Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET},
		},
	}).Pack()
	if err != nil {
		t.Fatalf("Pack AAAA query failed: %v", err)
	}
	aaaaWire := p.Answer(context.Background(), srv, aaaaQuery)
	var aaaaResp dnsmessage.Message
	if err := aaaaResp.Unpack(aaaaWire); err != nil {
		t.Fatalf("Unpack AAAA response failed: %v", err)
	}
	if aaaaResp.Header.ID != 0x1234 || aaaaResp.Header.RCode != dnsmessage.RCodeSuccess || len(aaaaResp.Answers) != 0 {
		t.Fatalf("expected NODATA with ID 0x1234, got %+v", aaaaResp)
	}
	if len(srv.recordedCalls()) != 0 {
		t.Fatalf("expected 0 upstream calls for AAAA, got %d", len(srv.recordedCalls()))
	}

	// 2. Unapproved domain query -> REFUSED with NO upstream dial.
	publicQuery, err := (&dnsmessage.Message{
		Header: dnsmessage.Header{ID: 0x2345, RecursionDesired: true},
		Questions: []dnsmessage.Question{
			{Name: mustName("www.example.org."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		},
	}).Pack()
	if err != nil {
		t.Fatalf("Pack public query failed: %v", err)
	}
	publicWire := p.Answer(context.Background(), srv, publicQuery)
	var publicResp dnsmessage.Message
	if err := publicResp.Unpack(publicWire); err != nil {
		t.Fatalf("Unpack public response failed: %v", err)
	}
	if publicResp.Header.ID != 0x2345 || publicResp.Header.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("expected REFUSED with ID 0x2345, got %+v", publicResp.Header)
	}
	if len(srv.recordedCalls()) != 0 {
		t.Fatalf("expected 0 upstream calls for unapproved domain, got %d", len(srv.recordedCalls()))
	}

	// 3. Approved A query -> RCodeSuccess with synthesized TTL <= 5s and preserved ID.
	aQuery, err := (&dnsmessage.Message{
		Header: dnsmessage.Header{ID: 0x3456, RecursionDesired: true},
		Questions: []dnsmessage.Question{
			{Name: mustName("app.corp.example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		},
	}).Pack()
	if err != nil {
		t.Fatalf("Pack A query failed: %v", err)
	}
	aWire := p.Answer(context.Background(), srv, aQuery)
	var aResp dnsmessage.Message
	if err := aResp.Unpack(aWire); err != nil {
		t.Fatalf("Unpack A response failed: %v", err)
	}
	if aResp.Header.ID != 0x3456 || aResp.Header.RCode != dnsmessage.RCodeSuccess || len(aResp.Answers) != 1 {
		t.Fatalf("unexpected A response: %+v", aResp)
	}
	if aResp.Answers[0].Header.TTL > 5 {
		t.Fatalf("expected synthesized TTL <= 5, got %d", aResp.Answers[0].Header.TTL)
	}
	aBody, ok := aResp.Answers[0].Body.(*dnsmessage.AResource)
	if !ok || netip.AddrFrom4(aBody.A) != netip.MustParseAddr("10.0.0.99") {
		t.Fatalf("unexpected A record body: %+v", aResp.Answers[0].Body)
	}

	// 4. Upstream failure for approved A query -> SERVFAIL with preserved ID.
	failSrv := &memoryDNSServer{
		handler: func(_ int, _ *dnsmessage.Message, _ []byte) ([]byte, error) {
			return nil, errors.New("upstream closed")
		},
	}
	failWire := p.Answer(context.Background(), failSrv, aQuery)
	var failResp dnsmessage.Message
	if err := failResp.Unpack(failWire); err != nil {
		t.Fatalf("Unpack SERVFAIL response failed: %v", err)
	}
	if failResp.Header.ID != 0x3456 || failResp.Header.RCode != dnsmessage.RCodeServerFailure {
		t.Fatalf("expected SERVFAIL with ID 0x3456, got %+v", failResp.Header)
	}
}
