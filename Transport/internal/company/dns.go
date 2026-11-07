package company

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	dnsPort              = "53"
	maxResolveTimeout    = 8 * time.Second
	maxCNAMEHops         = 8
	minDNSWireBytes      = 12
	maxDNSWireBytes      = 4096
	synthesizedAnswerTTL = 5
)

// Dialer abstracts connection establishment so company DNS resolution uses only injected TCP transport.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Resolve resolves an approved hostname to approved RFC1918 IPv4 addresses using framed DNS over TCP only.
func (p *Policy) Resolve(ctx context.Context, d Dialer, host string) ([]netip.Addr, error) {
	if p == nil {
		return nil, errors.New("nil company policy")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if d == nil {
		return nil, errors.New("nil dialer")
	}
	if len(p.dnsAddrs) == 0 {
		return nil, errors.New("no company DNS servers configured")
	}

	qname, err := normalizeQueryHostname(host)
	if err != nil || !p.AllowsDomain(qname) {
		return nil, fmt.Errorf("hostname %q is not approved by company policy", host)
	}

	ctx, cancel := context.WithTimeout(ctx, maxResolveTimeout)
	defer cancel()

	resolverAddr := p.dnsAddrs[0]
	if !resolverAddr.IsValid() || !resolverAddr.Is4() {
		return nil, errors.New("invalid configured DNS server address")
	}

	currentName := qname
	visited := map[string]bool{
		currentName: true,
	}
	cnameHops := 0

	for {
		resp, err := queryTCPDNS(ctx, d, resolverAddr, currentName)
		if err != nil {
			return nil, err
		}

		addrs, nextName, hops, err := p.evaluateAnswerChain(currentName, resp.Answers, visited, cnameHops)
		if err != nil {
			return nil, err
		}
		cnameHops = hops

		if len(addrs) > 0 {
			return addrs, nil
		}
		if nextName == "" || nextName == currentName {
			return nil, errors.New("empty DNS answer")
		}
		currentName = nextName
	}
}

// Answer handles a raw DNS query packet for local UDP/TCP listeners without global caches or upstream fallback.
func (p *Policy) Answer(ctx context.Context, d Dialer, wire []byte) []byte {
	if len(wire) < 2 {
		return nil
	}
	reqID := binary.BigEndian.Uint16(wire[:2])

	if len(wire) < minDNSWireBytes || len(wire) > maxDNSWireBytes {
		return packReply(reqID, false, dnsmessage.RCodeRefused, nil, nil)
	}

	var req dnsmessage.Message
	if err := req.Unpack(wire); err != nil {
		return packReply(reqID, false, dnsmessage.RCodeRefused, nil, nil)
	}

	if req.Header.Response || req.Header.OpCode != 0 || len(req.Questions) != 1 || len(req.Answers) != 0 || len(req.Authorities) != 0 {
		var qs []dnsmessage.Question
		if len(req.Questions) == 1 {
			qs = req.Questions[:1]
		}
		return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeRefused, qs, nil)
	}

	q := req.Questions[0]
	qs := []dnsmessage.Question{q}

	if p == nil || q.Class != dnsmessage.ClassINET {
		return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeRefused, qs, nil)
	}

	qname := q.Name.String()
	if !p.AllowsDomain(qname) {
		return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeRefused, qs, nil)
	}

	switch q.Type {
	case dnsmessage.TypeAAAA:
		// Approved hostname AAAA returns immediate NODATA (NOERROR with empty answer) without upstream query.
		return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeSuccess, qs, nil)

	case dnsmessage.TypeA:
		addrs, err := p.Resolve(ctx, d, qname)
		if err != nil || len(addrs) == 0 {
			return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeServerFailure, qs, nil)
		}
		answers := make([]dnsmessage.Resource, 0, len(addrs))
		for _, addr := range addrs {
			answers = append(answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{
					Name:  q.Name,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   synthesizedAnswerTTL,
				},
				Body: &dnsmessage.AResource{
					A: addr.As4(),
				},
			})
		}
		return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeSuccess, qs, answers)

	default:
		return packReply(req.Header.ID, req.Header.RecursionDesired, dnsmessage.RCodeRefused, qs, nil)
	}
}

func queryTCPDNS(ctx context.Context, d Dialer, dnsAddr netip.Addr, qname string) (*dnsmessage.Message, error) {
	reqID, err := randomUint16()
	if err != nil {
		return nil, err
	}

	fqdn, err := dnsmessage.NewName(qname + ".")
	if err != nil {
		return nil, fmt.Errorf("invalid DNS query name %q: %w", qname, err)
	}

	req := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:               reqID,
			RecursionDesired: true,
		},
		Questions: []dnsmessage.Question{
			{
				Name:  fqdn,
				Type:  dnsmessage.TypeA,
				Class: dnsmessage.ClassINET,
			},
		},
	}

	payload, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack DNS query: %w", err)
	}
	if len(payload) > maxDNSWireBytes {
		return nil, errors.New("DNS query exceeds maximum wire size")
	}

	target := net.JoinHostPort(dnsAddr.String(), dnsPort)
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("dial TCP DNS %s: %w", target, err)
	}
	defer conn.Close()

	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stopCancel()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set TCP DNS deadline: %w", err)
		}
	}

	frame := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(payload)))
	copy(frame[2:], payload)

	if err := writeFull(conn, frame); err != nil {
		return nil, fmt.Errorf("write TCP DNS query: %w", err)
	}

	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("read TCP DNS length prefix: %w", err)
	}
	respLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	if respLen < minDNSWireBytes || respLen > maxDNSWireBytes {
		return nil, fmt.Errorf("invalid or oversize TCP DNS response length %d", respLen)
	}

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, fmt.Errorf("read TCP DNS response body: %w", err)
	}

	var resp dnsmessage.Message
	if err := resp.Unpack(respBuf); err != nil {
		return nil, fmt.Errorf("malformed TCP DNS response: %w", err)
	}

	if !resp.Header.Response {
		return nil, errors.New("DNS packet is not a response")
	}
	if resp.Header.OpCode != 0 {
		return nil, fmt.Errorf("unexpected DNS opcode %d", resp.Header.OpCode)
	}
	if resp.Header.Truncated {
		return nil, errors.New("truncated DNS response rejected")
	}
	if resp.Header.ID != reqID {
		return nil, fmt.Errorf("mismatched DNS response ID: got %d, want %d", resp.Header.ID, reqID)
	}
	if resp.Header.RCode != dnsmessage.RCodeSuccess {
		return nil, fmt.Errorf("DNS response rcode %v", resp.Header.RCode)
	}
	if len(resp.Questions) != 1 {
		return nil, fmt.Errorf("DNS response must contain exactly 1 question, got %d", len(resp.Questions))
	}

	rq := resp.Questions[0]
	if rq.Type != dnsmessage.TypeA || rq.Class != dnsmessage.ClassINET {
		return nil, errors.New("mismatched DNS response question type or class")
	}
	rqName, err := normalizeQueryHostname(rq.Name.String())
	if err != nil || rqName != qname {
		return nil, fmt.Errorf("mismatched DNS response question name %q, want %q", rq.Name.String(), qname)
	}

	if len(resp.Answers) == 0 {
		return nil, errors.New("empty DNS answer section")
	}

	return &resp, nil
}

func (p *Policy) evaluateAnswerChain(
	startName string,
	answers []dnsmessage.Resource,
	visited map[string]bool,
	cnameHops int,
) ([]netip.Addr, string, int, error) {
	cnameMap := make(map[string]string)
	type aEntry struct {
		owner string
		addr  netip.Addr
	}
	var aRecords []aEntry

	for _, ans := range answers {
		if ans.Header.Class != dnsmessage.ClassINET {
			return nil, "", cnameHops, errors.New("non-IN resource record in DNS answer")
		}
		owner, err := normalizeQueryHostname(ans.Header.Name.String())
		if err != nil || !p.AllowsDomain(owner) {
			return nil, "", cnameHops, fmt.Errorf("unapproved answer owner domain %q", ans.Header.Name.String())
		}

		switch body := ans.Body.(type) {
		case *dnsmessage.CNAMEResource:
			target, err := normalizeQueryHostname(body.CNAME.String())
			if err != nil || !p.AllowsDomain(target) {
				return nil, "", cnameHops, fmt.Errorf("CNAME target %q is outside approved domain scope", body.CNAME.String())
			}
			if _, exists := cnameMap[owner]; exists {
				return nil, "", cnameHops, fmt.Errorf("duplicate CNAME record for %q", owner)
			}
			cnameMap[owner] = target

		case *dnsmessage.AResource:
			ip := netip.AddrFrom4(body.A)
			if !p.AllowsIP(ip) {
				return nil, "", cnameHops, fmt.Errorf("resolved IPv4 address %s is outside approved company networks", ip)
			}
			aRecords = append(aRecords, aEntry{owner: owner, addr: ip})

		default:
			return nil, "", cnameHops, fmt.Errorf("unsupported DNS answer record type %v", ans.Header.Type)
		}
	}

	expectedName := startName
	consumedCNAMEs := 0
	for {
		target, ok := cnameMap[expectedName]
		if !ok {
			break
		}
		consumedCNAMEs++
		cnameHops++
		if cnameHops > maxCNAMEHops {
			return nil, "", cnameHops, fmt.Errorf("CNAME chain exceeds maximum depth of %d", maxCNAMEHops)
		}
		if visited[target] {
			return nil, "", cnameHops, fmt.Errorf("CNAME loop detected at %q", target)
		}
		visited[target] = true
		expectedName = target
	}

	if consumedCNAMEs != len(cnameMap) {
		return nil, "", cnameHops, errors.New("disconnected or unchained CNAME record in DNS answer")
	}

	if len(aRecords) == 0 {
		if consumedCNAMEs == 0 {
			return nil, "", cnameHops, errors.New("empty DNS answer")
		}
		return nil, expectedName, cnameHops, nil
	}

	uniqueAddrs := make([]netip.Addr, 0, len(aRecords))
	seenAddrs := make(map[netip.Addr]struct{}, len(aRecords))
	for _, rec := range aRecords {
		if rec.owner != expectedName {
			return nil, "", cnameHops, fmt.Errorf("A record owner %q does not match expected target %q", rec.owner, expectedName)
		}
		if _, exists := seenAddrs[rec.addr]; !exists {
			seenAddrs[rec.addr] = struct{}{}
			uniqueAddrs = append(uniqueAddrs, rec.addr)
		}
	}

	return uniqueAddrs, expectedName, cnameHops, nil
}

func packReply(
	id uint16,
	recursionDesired bool,
	rcode dnsmessage.RCode,
	questions []dnsmessage.Question,
	answers []dnsmessage.Resource,
) []byte {
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 id,
			Response:           true,
			RecursionDesired:   recursionDesired,
			RecursionAvailable: true,
			RCode:              rcode,
		},
		Questions: questions,
		Answers:   answers,
	}
	out, err := msg.Pack()
	if err != nil {
		fallback := make([]byte, minDNSWireBytes)
		binary.BigEndian.PutUint16(fallback[0:2], id)
		flags := uint16(0x8000) | uint16(0x0080) | (uint16(rcode) & 0x000F)
		if recursionDesired {
			flags |= 0x0100
		}
		binary.BigEndian.PutUint16(fallback[2:4], flags)
		return fallback
	}
	return out
}

func randomUint16() (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return 0, fmt.Errorf("generate DNS request ID: %w", err)
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func writeFull(w io.Writer, buf []byte) error {
	for len(buf) > 0 {
		n, err := w.Write(buf)
		buf = buf[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
