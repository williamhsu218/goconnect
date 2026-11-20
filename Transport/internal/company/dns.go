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
	dnsPrimaryCooldown   = 15 * time.Second
	maxCNAMEHops         = 8
	minDNSWireBytes      = 12
	maxDNSWireBytes      = 4096
	synthesizedAnswerTTL = 5
)

var errDNSUnavailable = errors.New("company DNS resolver unavailable")

type dnsNegativeAnswer struct {
	code    dnsmessage.RCode
	aliases []dnsmessage.Resource
}

func (d *dnsNegativeAnswer) Error() string {
	return fmt.Sprintf("negative company DNS answer: %v", d.code)
}

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

	started := time.Now()
	primary := p.dnsAddrs[0]
	servers := p.dnsPreference.order(p.dnsAddrs, started)
	deadline, _ := ctx.Deadline()
	var lastErr error
	primaryUnavailable := false
	for i, server := range servers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// A stalled primary must leave time for the other approved resolvers.
		// Keep a complete CNAME chain on the same resolver.
		budget := time.Until(deadline) / time.Duration(len(servers)-i)
		attempt, stop := context.WithTimeout(ctx, budget)
		addrs, retry, err := p.resolveOnServer(attempt, d, server, qname)
		stop()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !retry {
			var negative *dnsNegativeAnswer
			if server == primary && (err == nil || errors.As(err, &negative)) {
				p.dnsPreference.primaryAnswered(started)
			} else if primaryUnavailable && err == nil {
				p.dnsPreference.useBackup(server, started, time.Now())
			}
			return addrs, err
		}
		if server == primary {
			primaryUnavailable = errors.Is(err, errDNSUnavailable)
		}
		lastErr = err
	}
	return nil, lastErr
}

func (p *Policy) resolveOnServer(ctx context.Context, d Dialer, server netip.Addr, host string) ([]netip.Addr, bool, error) {
	currentName := host
	visited := map[string]bool{host: true}
	var aliases []dnsmessage.Resource
	for {
		resp, err := queryTCPDNS(ctx, d, server, currentName)
		if err != nil {
			return nil, true, err
		}

		addrs, nextName, links, err := p.evaluateAnswerChain(currentName, resp.Answers, visited)
		if err != nil {
			return nil, false, err
		}
		aliases = append(aliases, links...)
		if resp.RCode == dnsmessage.RCodeNameError {
			if len(addrs) != 0 {
				return nil, false, errors.New("DNS name error contains address records")
			}
			return nil, false, &dnsNegativeAnswer{code: resp.RCode, aliases: aliases}
		}
		if len(addrs) > 0 {
			return addrs, false, nil
		}
		if nextName == currentName {
			if dnsReferral(resp.Authorities) {
				return nil, true, errors.New("company DNS returned a referral without an answer")
			}
			return nil, false, &dnsNegativeAnswer{code: dnsmessage.RCodeSuccess, aliases: aliases}
		}
		currentName = nextName
	}
}

// NOERROR with a delegation and no SOA is a referral, not a negative answer.
func dnsReferral(authorities []dnsmessage.Resource) bool {
	hasDelegation := false
	for _, record := range authorities {
		switch record.Body.(type) {
		case *dnsmessage.SOAResource:
			return false
		case *dnsmessage.NSResource:
			hasDelegation = true
		}
	}
	return hasDelegation
}

// Answer handles a raw DNS query packet for local UDP/TCP listeners using only the configured company resolvers.
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
		var negative *dnsNegativeAnswer
		if errors.As(err, &negative) {
			reply := packReply(req.Header.ID, req.Header.RecursionDesired, negative.code, qs, negative.aliases)
			if len(reply) <= maxDNSWireBytes {
				return reply
			}
		}
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
		return nil, fmt.Errorf("%w: dial TCP DNS %s: %w", errDNSUnavailable, target, err)
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
		return nil, fmt.Errorf("%w: write TCP DNS query: %w", errDNSUnavailable, err)
	}

	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("%w: read TCP DNS length prefix: %w", errDNSUnavailable, err)
	}
	respLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	if respLen < minDNSWireBytes || respLen > maxDNSWireBytes {
		return nil, fmt.Errorf("invalid or oversize TCP DNS response length %d", respLen)
	}

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, fmt.Errorf("%w: read TCP DNS response body: %w", errDNSUnavailable, err)
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

	switch resp.Header.RCode {
	case dnsmessage.RCodeSuccess, dnsmessage.RCodeNameError:
		return &resp, nil
	case dnsmessage.RCodeServerFailure:
		return nil, fmt.Errorf("%w: DNS response rcode %v", errDNSUnavailable, resp.Header.RCode)
	default:
		return nil, fmt.Errorf("DNS response rcode %v", resp.Header.RCode)
	}
}

func (p *Policy) evaluateAnswerChain(
	startName string,
	answers []dnsmessage.Resource,
	visited map[string]bool,
) ([]netip.Addr, string, []dnsmessage.Resource, error) {
	type cnameRecord struct {
		target string
		record dnsmessage.Resource
	}
	cnameMap := make(map[string]cnameRecord)
	type aEntry struct {
		owner string
		addr  netip.Addr
	}
	var aRecords []aEntry

	for _, ans := range answers {
		if ans.Header.Class != dnsmessage.ClassINET {
			return nil, "", nil, errors.New("non-IN resource record in DNS answer")
		}
		owner, err := normalizeQueryHostname(ans.Header.Name.String())
		if err != nil || !p.AllowsDomain(owner) {
			return nil, "", nil, fmt.Errorf("unapproved answer owner domain %q", ans.Header.Name.String())
		}

		switch body := ans.Body.(type) {
		case *dnsmessage.CNAMEResource:
			target, err := normalizeQueryHostname(body.CNAME.String())
			if err != nil || !p.AllowsDomain(target) {
				return nil, "", nil, fmt.Errorf("CNAME target %q is outside approved domain scope", body.CNAME.String())
			}
			if _, exists := cnameMap[owner]; exists {
				return nil, "", nil, fmt.Errorf("duplicate CNAME record for %q", owner)
			}
			ans.Header.TTL = min(ans.Header.TTL, synthesizedAnswerTTL)
			cnameMap[owner] = cnameRecord{target, ans}

		case *dnsmessage.AResource:
			ip := netip.AddrFrom4(body.A)
			if !p.AllowsIP(ip) {
				return nil, "", nil, fmt.Errorf("resolved IPv4 address %s is outside approved company networks", ip)
			}
			aRecords = append(aRecords, aEntry{owner: owner, addr: ip})

		default:
			return nil, "", nil, fmt.Errorf("unsupported DNS answer record type %v", ans.Header.Type)
		}
	}

	expectedName := startName
	var aliases []dnsmessage.Resource
	for {
		link, ok := cnameMap[expectedName]
		if !ok {
			break
		}
		if len(visited) > maxCNAMEHops {
			return nil, "", nil, fmt.Errorf("CNAME chain exceeds maximum depth of %d", maxCNAMEHops)
		}
		if visited[link.target] {
			return nil, "", nil, fmt.Errorf("CNAME loop detected at %q", link.target)
		}
		visited[link.target] = true
		aliases = append(aliases, link.record)
		expectedName = link.target
	}

	if len(aliases) != len(cnameMap) {
		return nil, "", nil, errors.New("disconnected or unchained CNAME record in DNS answer")
	}

	uniqueAddrs := make([]netip.Addr, 0, len(aRecords))
	seenAddrs := make(map[netip.Addr]struct{}, len(aRecords))
	for _, rec := range aRecords {
		if rec.owner != expectedName {
			return nil, "", nil, fmt.Errorf("A record owner %q does not match expected target %q", rec.owner, expectedName)
		}
		if _, exists := seenAddrs[rec.addr]; !exists {
			seenAddrs[rec.addr] = struct{}{}
			uniqueAddrs = append(uniqueAddrs, rec.addr)
		}
	}

	return uniqueAddrs, expectedName, aliases, nil
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
