package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"goconnect.local/transport/internal/bridge"
	"goconnect.local/transport/internal/company"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
	"golang.org/x/sys/unix"
)

type companyReservation struct {
	Networks    []string `json:"networks"`
	Domains     []string `json:"domains"`
	DNSServers  []string `json:"dnsServers"`
	GatewayHost string   `json:"gatewayHost"`
	GatewayPort uint16   `json:"gatewayPort"`
	GatewayIPs  []string `json:"gatewayIPs"`
}
type companyBackendClaim struct {
	SocketPath     string   `json:"socketPath"`
	PID            int      `json:"pid"`
	StartTime      uint64   `json:"startTime"`
	Token          string   `json:"token"`
	RemoteNetworks []string `json:"remoteNetworks"`
	RemoteExcluded []string `json:"remoteExcluded"`
	DNSServers     []string `json:"dnsServers"`
}
type companyRouteState struct {
	Revision   uint64   `json:"revision"`
	Generation string   `json:"generation"`
	State      string   `json:"state"`
	Message    string   `json:"message,omitempty"`
	ProxyPort  uint16   `json:"proxyPort,omitempty"`
	ProxyToken string   `json:"proxyToken,omitempty"`
	DNSServers []string `json:"dnsServers,omitempty"`
}
type companyCommand struct {
	ExpectedRevision uint64               `json:"expectedRevision"`
	Generation       string               `json:"generation"`
	Action           string               `json:"action"`
	Backend          *companyBackendClaim `json:"backend,omitempty"`
}

func (r *companyReservation) validate() error {
	if _, err := company.NewPolicy(company.Policy{Networks: r.Networks, Domains: r.Domains, DNSServers: r.DNSServers}); err != nil {
		return err
	}
	if r.GatewayHost == "" || r.GatewayPort == 0 || len(r.GatewayIPs) == 0 || len(r.GatewayIPs) > 4 || len(r.GatewayHost) > 253 || strings.ContainsAny(r.GatewayHost, "\x00\r\n/@ ") {
		return errors.New("invalid company gateway")
	}
	for _, raw := range r.GatewayIPs {
		a, e := netip.ParseAddr(raw)
		if e != nil || !a.Is4() || a.IsLoopback() || a.IsLinkLocalUnicast() || !a.IsGlobalUnicast() {
			return errors.New("invalid company gateway address")
		}
	}
	return nil
}
func validateCompanyReservation(r routeRequest, local []localNetworkRoute, ts tailscaleBypass) error {
	if r.Company == nil {
		return nil
	}
	p := r.Company
	if !r.TransparentRouting || r.ProbeOnly || len(r.RemoteNetworks) != 0 {
		return errors.New("company reservation requires ordinary transparent primary")
	}
	if err := p.validate(); err != nil {
		return err
	}
	if _, err := companyPlan(p.Networks, local, ts); err != nil {
		return err
	}
	for _, scope := range p.Networks {
		q, _ := netip.ParsePrefix(scope)
		for _, reserved := range []string{systemCaptureAddress + "/30", systemCaptureIPv6 + "/126"} {
			rp, _ := netip.ParsePrefix(reserved)
			if q.Overlaps(rp.Masked()) {
				return errors.New("company scope overlaps managed TUN")
			}
		}
	}
	for _, raw := range append(append([]string{r.Gateway}, p.GatewayIPs...), p.DNSServers...) {
		a, _ := netip.ParseAddr(raw)
		for _, scope := range p.Networks {
			cidr, _ := netip.ParsePrefix(scope)
			if cidr.Contains(a) && (raw == r.Gateway || containsString(p.GatewayIPs, raw)) {
				return errors.New("company network contains VPN gateway")
			}
		}
	}
	for _, d := range r.DirectDomains {
		if prefix, ok := directIPPrefix(d.Domain); ok {
			for _, s := range p.Networks {
				q, _ := netip.ParsePrefix(s)
				if prefix.Overlaps(q) {
					return errors.New("company scope conflicts with direct address")
				}
			}
			continue
		}
		for _, s := range p.Domains {
			if domainOverlap(s, d.Domain) {
				return errors.New("company scope conflicts with direct domain")
			}
		}
	}
	if r.managedDNS != nil {
		for _, resolver := range r.managedDNS.resolvers {
			for _, s := range p.Domains {
				if resolver.domain != "" && domainOverlap(s, resolver.domain) {
					return errors.New("company scope conflicts with native scoped resolver")
				}
			}
		}
	}
	return nil
}
func containsString(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func domainOverlap(a, b string) bool {
	a = strings.TrimSuffix(strings.ToLower(a), ".")
	b = strings.TrimSuffix(strings.ToLower(b), ".")
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

type companyBackend struct {
	claim      companyBackendClaim
	policy     *company.Policy
	generation string
	ctx        context.Context
	cancel     context.CancelFunc
	flows      map[*companyFlow]bool
}
type companyGate struct {
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	listener         net.Listener
	dnsTCP           net.Listener
	dnsUDP           *net.UDPConn
	port, dnsPort    uint16
	token            string
	uid              uint32
	directory, self  string
	reservation      companyReservation
	state            companyRouteState
	prepared, active *companyBackend
	outer            *companyOuterProxy
	dnsLeases        []managedDNSLease
	verify           func(*net.UnixConn, companyBackendClaim) error
}

func newCompanyGate(parent context.Context, c *controlFiles, original originalNetwork) (*companyGate, error) {
	ctx, cancel := context.WithCancel(parent)
	g := &companyGate{ctx: ctx, cancel: cancel, uid: c.uid, directory: c.directory.Name(), reservation: *c.request.Company, token: ""}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		cancel()
		return nil, err
	}
	g.token = fmt.Sprintf("%x", nonce)
	var executableErr error
	g.self, executableErr = os.Executable()
	if executableErr != nil || g.self == "" {
		cancel()
		return nil, errors.New("cannot verify company backend code identity")
	}
	g.verify = g.verifyBackend
	var err error
	defer func() {
		if err != nil {
			g.Close()
		}
	}()
	g.listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	g.port = uint16(g.listener.Addr().(*net.TCPAddr).Port)
	g.dnsTCP, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	g.dnsPort = uint16(g.dnsTCP.Addr().(*net.TCPAddr).Port)
	g.dnsUDP, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(g.dnsPort)})
	if err != nil {
		return nil, err
	}
	g.outer, err = newCompanyOuterProxy(ctx, g.reservation.GatewayHost, g.reservation.GatewayPort, g.reservation.GatewayIPs, original.Interface, g.token)
	if err != nil {
		return nil, err
	}
	g.state = companyRouteState{State: "blocked", ProxyPort: g.outer.Port, ProxyToken: g.token}
	go func() {
		_ = (&bridge.SOCKSServer{Token: g.token, Dialer: g}).Serve(ctx, companySOCKSListener{g.listener})
	}()
	go g.serveDNSUDP()
	go g.serveDNSTCP()
	return g, nil
}
func (g *companyGate) verifyBackend(conn *net.UnixConn, claim companyBackendClaim) error {
	id, err := verifyServicePeer(conn, g.self)
	if err != nil {
		return err
	}
	if id.UID != g.uid || id.PID != claim.PID {
		return errors.New("company backend peer changed")
	}
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", id.PID)
	if err != nil {
		return err
	}
	birth := uint64(proc.Proc.P_starttime.Sec)*1_000_000 + uint64(proc.Proc.P_starttime.Usec)
	if proc.Eproc.Ucred.Uid != g.uid || birth != claim.StartTime {
		return errors.New("company backend process changed")
	}
	return nil
}

type companyUnixDialer struct {
	g *companyGate
	b *companyBackend
}

func (d companyUnixDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(d.b.ctx, network, address)
}
func (d companyUnixDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "unix", d.b.claim.SocketPath)
	if err != nil {
		return nil, err
	}
	conn := raw.(*net.UnixConn)
	if err = d.g.verify(conn, d.b.claim); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
func (g *companyGate) backendDial(ctx context.Context, b *companyBackend, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, errors.New("company UDP unsupported")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if a, e := netip.ParseAddr(host); e != nil || !a.Is4() {
		return nil, errors.New("company backend requires literal IPv4")
	}
	d, err := proxy.SOCKS5("unix", b.claim.SocketPath, &proxy.Auth{User: "goconnect", Password: b.claim.Token}, companyUnixDialer{g, b})
	if err != nil {
		return nil, err
	}
	return d.(proxy.ContextDialer).DialContext(ctx, "tcp", address)
}

type companyDNSDialer struct {
	g *companyGate
	b *companyBackend
}

func (d companyDNSDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	h, p, e := net.SplitHostPort(address)
	if e != nil || network != "tcp" || p != "53" || !containsString(d.b.policy.DNSServers, h) {
		return nil, errors.New("unapproved company DNS destination")
	}
	return d.g.backendDial(ctx, d.b, network, address)
}

type companyFlow struct {
	net.Conn
	gate    *companyGate
	backend *companyBackend
	once    sync.Once
}

func (c *companyFlow) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.gate.mu.Lock(); delete(c.backend.flows, c); c.gate.mu.Unlock() })
	return err
}
func (g *companyGate) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, errors.New("company UDP unsupported")
	}
	g.mu.Lock()
	b := g.active
	g.mu.Unlock()
	if b == nil {
		return nil, errors.New("company connection blocked")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(b.ctx, cancel)
	defer stop()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	a, e := netip.ParseAddr(host)
	if e != nil {
		ips, err := b.policy.Resolve(ctx, companyDNSDialer{g, b}, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("company name rejected")
		}
		a = ips[0]
	}
	if !b.policy.AllowsIP(a) {
		return nil, errors.New("company target outside approved scope")
	}
	remote, err := g.backendDial(ctx, b, network, net.JoinHostPort(a.String(), port))
	if err != nil {
		return nil, err
	}
	flow := &companyFlow{Conn: remote, gate: g, backend: b}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != b || b.ctx.Err() != nil {
		remote.Close()
		return nil, errors.New("company generation revoked")
	}
	if len(b.flows) >= 256 {
		remote.Close()
		return nil, errors.New("company flow limit")
	}
	b.flows[flow] = true
	return flow, nil
}
func (g *companyGate) blockLocked() {
	for _, b := range []*companyBackend{g.active, g.prepared} {
		if b != nil {
			b.cancel()
			for f := range b.flows {
				_ = f.Conn.Close()
			}
			b.flows = map[*companyFlow]bool{}
		}
	}
	g.active = nil
	g.prepared = nil
	g.state.State = "blocked"
	g.state.DNSServers = nil
}
func (g *companyGate) Close() error {
	dnsErr := g.releaseDNS()
	g.cancel()
	g.mu.Lock()
	g.blockLocked()
	g.mu.Unlock()
	if g.listener != nil {
		g.listener.Close()
	}
	if g.dnsTCP != nil {
		g.dnsTCP.Close()
	}
	if g.dnsUDP != nil {
		g.dnsUDP.Close()
	}
	if g.outer != nil {
		g.outer.Close()
	}
	return dnsErr
}
func (g *companyGate) snapshot() companyRouteState { g.mu.Lock(); defer g.mu.Unlock(); return g.state }
func (g *companyGate) command(ctx context.Context, cmd companyCommand) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cmd.ExpectedRevision != g.state.Revision || len(cmd.Generation) != 36 {
		return errors.New("stale company transaction")
	}
	if cmd.Action != "prepare" && cmd.Action != "commit" && cmd.Action != "block" {
		return errors.New("invalid company transaction")
	}
	if cmd.Action != "prepare" && cmd.Backend != nil {
		return errors.New("unexpected company backend")
	}
	if cmd.Action != "prepare" && g.state.Generation != "" && cmd.Generation != g.state.Generation {
		return errors.New("company generation mismatch")
	}
	if cmd.Action == "commit" && (g.prepared == nil || g.prepared.generation != cmd.Generation) {
		return errors.New("company backend not prepared")
	}
	if cmd.Action == "prepare" && (g.active != nil || g.prepared != nil) {
		return errors.New("block old company generation first")
	}
	g.state.Revision++
	g.state.Generation = cmd.Generation
	g.state.Message = ""
	if cmd.Action == "block" {
		g.blockLocked()
		return nil
	}
	if cmd.Action == "commit" {
		if g.prepared == nil || g.prepared.generation != cmd.Generation {
			return errors.New("company backend not prepared")
		}
		g.active = g.prepared
		g.prepared = nil
		g.state.State = "active"
		g.state.DNSServers = append([]string{}, g.active.policy.DNSServers...)
		return nil
	}
	g.blockLocked()
	fail := func(err error) error {
		g.state.State = "failed"
		g.state.Message = "公司出口未通过验证，保持阻断。"
		return err
	}
	if cmd.Backend == nil {
		return fail(errors.New("missing company backend"))
	}
	claim := *cmd.Backend
	if claim.SocketPath != filepath.Join(g.directory, "company.sock") || claim.PID <= 1 || claim.StartTime == 0 || len(claim.Token) < 32 || len(claim.Token) > 200 {
		return fail(errors.New("invalid company endpoint claim"))
	}
	st, err := os.Lstat(claim.SocketPath)
	if err != nil {
		return fail(err)
	}
	raw := st.Sys().(*syscall.Stat_t)
	if st.Mode()&os.ModeSocket == 0 || st.Mode().Perm() != 0600 || raw.Uid != g.uid {
		return fail(errors.New("unsafe company socket"))
	}
	if len(claim.RemoteNetworks) == 0 || len(claim.RemoteNetworks) > 256 || len(claim.RemoteExcluded) > 256 {
		return fail(errors.New("missing or excessive server split scope"))
	}
	for _, scope := range g.reservation.Networks {
		q, _ := netip.ParsePrefix(scope)
		covered := false
		for _, raw := range claim.RemoteNetworks {
			p, ok := remotePrefix(raw)
			if !ok {
				return fail(errors.New("invalid server include scope"))
			}
			if p.Bits() <= q.Bits() && p.Contains(q.Addr()) {
				covered = true
			}
		}
		if !covered {
			return fail(errors.New("approved scope exceeds unrestricted server include"))
		}
		for _, raw := range claim.RemoteExcluded {
			p, ok := remotePrefix(raw)
			if !ok || p.Overlaps(q) {
				return fail(errors.New("company scope conflicts with server exclusion"))
			}
		}
	}
	dns := claim.DNSServers
	if len(g.reservation.DNSServers) > 0 {
		dns = g.reservation.DNSServers
	}
	for _, raw := range claim.RemoteExcluded {
		prefix, ok := remotePrefix(raw)
		if !ok {
			return fail(errors.New("invalid server exclusion"))
		}
		for _, server := range dns {
			ip, _ := netip.ParseAddr(server)
			if prefix.Contains(ip) {
				return fail(errors.New("company DNS conflicts with server exclusion"))
			}
		}
	}
	policy, err := company.NewPolicy(company.Policy{Networks: g.reservation.Networks, Domains: g.reservation.Domains, DNSServers: dns})
	if err != nil {
		return fail(err)
	}
	if len(policy.Domains) > 0 && len(policy.DNSServers) == 0 {
		return fail(errors.New("company DNS unavailable"))
	}
	bctx, cancel := context.WithCancel(g.ctx)
	b := &companyBackend{claim: claim, policy: policy, generation: cmd.Generation, ctx: bctx, cancel: cancel, flows: map[*companyFlow]bool{}}
	readyctx, done := context.WithTimeout(ctx, 6*time.Second)
	defer done()
	// Verify the server's kernel identity even for an IP-only scope.
	rawConn, err := (companyUnixDialer{g, b}).DialContext(readyctx, "unix", claim.SocketPath)
	if err != nil {
		cancel()
		return fail(err)
	}
	rawConn.Close()
	if len(policy.Domains) > 0 {
		if err = g.probeDNS(readyctx, b); err != nil {
			cancel()
			return fail(err)
		}
	}
	g.prepared = b
	g.state.State = "prepared"
	return nil
}
func (g *companyGate) poll(c *controlFiles) {
	data, _, err := readOwned(c.directory, "company-command.json", c.uid)
	if err == nil {
		var cmd companyCommand
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cmd) == nil && decoder.Decode(new(any)) == io.EOF {
			_ = g.command(g.ctx, cmd)
		}
	}
	state := g.snapshot()
	c.snapshot.Company = &state
}
func (g *companyGate) dnsAnswer(wire []byte) []byte {
	g.mu.Lock()
	b := g.active
	g.mu.Unlock()
	if b == nil { // A synthetic SERVFAIL preserves the question but never resolves elsewhere.
		if len(wire) < 12 {
			return nil
		}
		out := append([]byte{}, wire...)
		out[2] = 0x81
		out[3] = 0x82
		for i := 6; i < 12; i++ {
			out[i] = 0
		}
		return out
	}
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	return b.policy.Answer(ctx, companyDNSDialer{g, b}, wire)
}
func (g *companyGate) serveDNSUDP() {
	limit := make(chan struct{}, 64)
	for {
		buf := make([]byte, 4096)
		n, addr, e := g.dnsUDP.ReadFromUDP(buf)
		if e != nil {
			return
		}
		select {
		case limit <- struct{}{}:
			go func() {
				defer func() { <-limit }()
				reply := g.dnsAnswer(buf[:n])
				if len(reply) > 0 {
					g.dnsUDP.WriteToUDP(reply, addr)
				}
			}()
		default:
		}
	}
}
func (g *companyGate) serveDNSTCP() {
	limit := make(chan struct{}, 64)
	for {
		conn, e := g.dnsTCP.Accept()
		if e != nil {
			return
		}
		select {
		case limit <- struct{}{}:
			go func() {
				defer func() { <-limit }()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(9 * time.Second))
				var h [2]byte
				if _, e := io.ReadFull(conn, h[:]); e != nil {
					return
				}
				n := int(binary.BigEndian.Uint16(h[:]))
				if n < 12 || n > 4096 {
					return
				}
				wire := make([]byte, n)
				if _, e := io.ReadFull(conn, wire); e != nil {
					return
				}
				reply := g.dnsAnswer(wire)
				if len(reply) == 0 {
					return
				}
				binary.BigEndian.PutUint16(h[:], uint16(len(reply)))
				conn.Write(h[:])
				conn.Write(reply)
			}()
		default:
			conn.Close()
		}
	}
}
func (g *companyGate) configure(config map[string]any) {
	proxies := config["proxies"].([]map[string]any)
	proxies = append(proxies, map[string]any{"name": "CompanyGate", "type": "socks5", "server": "127.0.0.1", "port": g.port, "username": "goconnect", "password": g.token, "udp": false})
	config["proxies"] = proxies
	rules := config["rules"].([]string)
	companyRules := []string{}
	for _, scope := range g.reservation.Networks {
		companyRules = append(companyRules, "IP-CIDR,"+scope+",CompanyGate,no-resolve")
	}
	for _, domain := range g.reservation.Domains {
		companyRules = append(companyRules, "DOMAIN-SUFFIX,"+domain+",CompanyGate")
	}
	config["rules"] = append(companyRules, rules...)
	sniff := config["sniffer"].(map[string]any)
	sniff["skip-dst-address"] = g.reservation.Networks
	skipped := make([]string, 0, len(g.reservation.Domains))
	for _, domain := range g.reservation.Domains {
		skipped = append(skipped, "+."+domain)
	}
	sniff["skip-domain"] = skipped
	dns := config["dns"].(map[string]any)
	policies := dns["nameserver-policy"].(map[string]any)
	for _, d := range g.reservation.Domains {
		policies["+."+d] = []string{fmt.Sprintf("tcp://127.0.0.1:%d", g.dnsPort)}
	}
}
func companyGatewayDNS(value string) (string, uint16, error) {
	h, p, e := net.SplitHostPort(value)
	if e != nil {
		return "", 0, e
	}
	v, e := strconv.ParseUint(p, 10, 16)
	return h, uint16(v), e
}

// A suffix's apex often has no address record. Readiness verifies the selected
// TCP resolver, not existence of an arbitrary business hostname. Every actual
// answer still passes the stricter company.Policy.Resolve scope checks.
func (g *companyGate) probeDNS(ctx context.Context, b *companyBackend) error {
	name, err := dnsmessage.NewName(b.policy.Domains[0] + ".")
	if err != nil {
		return err
	}
	var random [2]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	id := binary.BigEndian.Uint16(random[:])
	question := dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	query, _ := (&dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{question}}).Pack()
	for i, server := range b.policy.DNSServers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, e := (companyDNSDialer{g, b}).DialContext(ctx, "tcp", net.JoinHostPort(server, "53"))
		if e != nil {
			continue
		}
		deadline := time.Now().Add(2 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetDeadline(deadline)
		frame := append([]byte{byte(len(query) >> 8), byte(len(query))}, query...)
		_, e = conn.Write(frame)
		var header [2]byte
		if e == nil {
			_, e = io.ReadFull(conn, header[:])
		}
		var reply dnsmessage.Message
		if e == nil {
			n := int(binary.BigEndian.Uint16(header[:]))
			if n < 12 || n > 4096 {
				e = errors.New("invalid DNS readiness frame")
			} else {
				wire := make([]byte, n)
				_, e = io.ReadFull(conn, wire)
				if e == nil {
					e = reply.Unpack(wire)
				}
			}
		}
		conn.Close()
		if e != nil || !reply.Response || reply.Truncated || reply.OpCode != 0 || reply.ID != id || len(reply.Questions) != 1 || reply.Questions[0] != question || (reply.RCode != dnsmessage.RCodeSuccess && reply.RCode != dnsmessage.RCodeNameError) {
			continue
		}
		if i > 0 {
			servers := append([]string{server}, b.policy.DNSServers[:i]...)
			servers = append(servers, b.policy.DNSServers[i+1:]...)
			b.policy, err = company.NewPolicy(company.Policy{Networks: b.policy.Networks, Domains: b.policy.Domains, DNSServers: servers})
			if err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("company TCP DNS unavailable")
}

func (g *companyGate) installDNS() error {
	for _, domain := range g.reservation.Domains {
		lease, err := acquireManagedDNS(g.dnsPort, domain)
		if err != nil {
			return err
		}
		g.dnsLeases = append(g.dnsLeases, lease)
		if err = waitManagedDNS(g.dnsPort, domain, true); err != nil {
			return err
		}
	}
	return nil
}
func (g *companyGate) releaseDNS() error {
	var result error
	for i := len(g.dnsLeases) - 1; i >= 0; i-- {
		result = errors.Join(result, g.dnsLeases[i].close())
	}
	g.dnsLeases = nil
	return result
}
func (g *companyGate) dnsIntact() bool {
	for _, lease := range g.dnsLeases {
		if !lease.intact() {
			return false
		}
	}
	return true
}
