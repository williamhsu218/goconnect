package main

// Standalone, unprivileged diagnostics. This entry point never calls the
// installed service, acquires a capture lease, installs TUN routes or DNS leases.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type companyTestRequest struct {
	Server            string   `json:"server"`
	Username          string   `json:"username"`
	Group             string   `json:"group"`
	Password          string   `json:"password"`
	OpenConnectPath   string   `json:"openconnectPath"`
	Interface         string   `json:"interface"`
	GatewayIPs        []string `json:"gatewayIPs"`
	Network           string   `json:"network"`
	DNSName           string   `json:"dnsName"`
	EvidenceDirectory string   `json:"evidenceDirectory"`
}
type companyTestDNSResult struct {
	Server            string   `json:"server"`
	Type              string   `json:"type"`
	RCode             string   `json:"rcode,omitempty"`
	Addresses         []string `json:"addresses,omitempty"`
	Aliases           []string `json:"aliases,omitempty"`
	Error             string   `json:"error,omitempty"`
	ResponseValidated bool     `json:"responseValidated"`
	Resolved          bool     `json:"resolved"`
}
type companyTestProbe struct {
	Target     string `json:"target"`
	Kind       string `json:"kind"`
	Success    bool   `json:"success"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Error      string `json:"error,omitempty"`
}
type companyTestReport struct {
	Started                 string                 `json:"started"`
	Finished                string                 `json:"finished,omitempty"`
	PhysicalInterface       string                 `json:"physicalInterface"`
	PhysicalSource          string                 `json:"physicalSource,omitempty"`
	Gateway                 string                 `json:"gateway,omitempty"`
	Authenticated           bool                   `json:"authenticated"`
	BackendIdentityVerified bool                   `json:"backendIdentityVerified"`
	DNSServers              []string               `json:"dnsServers,omitempty"`
	DNS                     []companyTestDNSResult `json:"dns,omitempty"`
	Probes                  []companyTestProbe     `json:"probes,omitempty"`
	Error                   string                 `json:"error,omitempty"`
	CleanupConfirmed        bool                   `json:"cleanupConfirmed"`
	Received                uint64                 `json:"received"`
	Sent                    uint64                 `json:"sent"`
}

func companyTestEvent(event, message string) {
	emit(map[string]any{"event": event, "message": message})
}
func (r companyTestRequest) validate(preflight bool) error {
	u, e := url.Parse(r.Server)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid test gateway")
	}
	port := uint16(443)
	if u.Port() != "" {
		var n int
		if _, e = fmt.Sscan(u.Port(), &n); e != nil || n < 1 || n > 65535 {
			return errors.New("invalid test gateway port")
		}
		port = uint16(n)
	}
	reservation := companyReservation{Networks: []string{r.Network}, GatewayHost: u.Hostname(), GatewayPort: port, GatewayIPs: r.GatewayIPs}
	if e = reservation.validate(); e != nil {
		return e
	}
	prefix, e := netip.ParsePrefix(r.Network)
	if e != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return errors.New("tester requires one explicit RFC1918 IPv4 /32")
	}
	if r.DNSName != "" {
		if !validDirectDomain(r.DNSName) || strings.ToLower(r.DNSName) != r.DNSName {
			return errors.New("invalid test DNS hostname")
		}
		if _, e = netip.ParseAddr(r.DNSName); e == nil {
			return errors.New("DNS test requires hostname")
		}
	}
	if !physicalInterface.MatchString(r.Interface) {
		return errors.New("invalid physical test interface")
	}
	if !preflight {
		if r.Username == "" || r.Password == "" || len(r.Password) > 4096 || strings.ContainsAny(r.Username+r.Group+r.Password, "\x00\r\n") {
			return errors.New("invalid test credential")
		}
		if !filepath.IsAbs(r.OpenConnectPath) || filepath.Base(r.OpenConnectPath) != "openconnect" {
			return errors.New("invalid test executable")
		}
		info, e := os.Stat(r.OpenConnectPath)
		if e != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return errors.New("test executable unavailable")
		}
		canonical, e := filepath.EvalSymlinks(r.EvidenceDirectory)
		if e != nil || canonical != r.EvidenceDirectory {
			return errors.New("invalid test evidence directory")
		}
		info, e = os.Lstat(canonical)
		if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			return errors.New("test evidence directory must be private")
		}
	}
	return nil
}
func companyTestPhysicalProbe(ctx context.Context, r companyTestRequest) (string, string, error) {
	nic, e := net.InterfaceByName(r.Interface)
	if e != nil || nic.Flags&net.FlagLoopback != 0 || nic.Flags&net.FlagUp == 0 {
		return "", "", errors.New("physical interface unavailable")
	}
	u, _ := url.Parse(r.Server)
	port := u.Port()
	if port == "" {
		port = "443"
	}
	d := &net.Dialer{Timeout: 5 * time.Second, Control: func(network, address string, raw syscall.RawConn) error {
		var inner error
		e := raw.Control(func(fd uintptr) {
			inner = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, nic.Index)
			if inner == nil {
				bound, e := unix.GetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF)
				if e != nil {
					inner = e
				} else if bound != nic.Index {
					inner = errors.New("physical socket binding not confirmed")
				}
			}
		})
		if e != nil {
			return e
		}
		return inner
	}}
	for _, ip := range r.GatewayIPs {
		conn, e := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip, port))
		if e != nil {
			continue
		}
		source := conn.LocalAddr().(*net.TCPAddr).IP.String()
		conn.Close()
		found := false
		addresses, _ := nic.Addrs()
		for _, address := range addresses {
			if p, e := netip.ParsePrefix(address.String()); e == nil && p.Addr().String() == source {
				found = true
			}
		}
		if !found {
			return "", "", errors.New("outer socket did not use physical source address")
		}
		return ip, source, nil
	}
	return "", "", errors.New("physical company gateway TCP connection failed; no primary-VPN fallback")
}
func companyTestCommand(parent context.Context, preflight bool) (result error) {
	defer func() {
		if result != nil {
			companyTestEvent("error", result.Error())
		}
	}()
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 65537))
	line, e := reader.ReadBytes('\n')
	if e != nil {
		return errors.New("test request unavailable")
	}
	var r companyTestRequest
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&r); e != nil {
		return errors.New("invalid test request")
	}
	line = nil
	if e = r.validate(preflight); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	defer cancel()
	if !preflight {
		go func() { _, _ = io.Copy(io.Discard, reader); cancel() }()
	}
	report := companyTestReport{Started: time.Now().UTC().Format(time.RFC3339), PhysicalInterface: r.Interface}
	report.Gateway, report.PhysicalSource, e = companyTestPhysicalProbe(ctx, r)
	if e != nil {
		return e
	}
	companyTestEvent("status", "物理网卡绑定及公司网关 TCP 连通性已确认；未修改系统路由。")
	if preflight {
		emit(map[string]any{"event": "preflight", "gateway": report.Gateway, "source": report.PhysicalSource, "interface": r.Interface})
		return nil
	}
	dir, e := os.MkdirTemp("/private/tmp", "gcc-live-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	if e = os.Chmod(dir, 0700); e != nil {
		return e
	}
	directory, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer directory.Close()
	u, _ := url.Parse(r.Server)
	port := uint16(443)
	if u.Port() != "" {
		fmt.Sscan(u.Port(), &port)
	}
	reservation := &companyReservation{Networks: []string{r.Network}, GatewayHost: u.Hostname(), GatewayPort: port, GatewayIPs: r.GatewayIPs}
	control := &controlFiles{directory: directory, uid: uint32(os.Getuid()), request: routeRequest{Company: reservation}}
	gate, e := newCompanyGate(ctx, control, originalNetwork{Interface: r.Interface})
	if e != nil {
		return e
	}
	defer gate.Close()
	self, e := os.Executable()
	if e != nil {
		return e
	}
	command := exec.Command(self, "session")
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, e := command.StdinPipe()
	if e != nil {
		return e
	}
	output, writer, e := os.Pipe()
	if e != nil {
		return e
	}
	defer output.Close()
	defer writer.Close()
	command.Stdout = writer
	null, e := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if e != nil {
		return e
	}
	defer null.Close()
	command.Stderr = null
	command.WaitDelay = 3 * time.Second
	tokenBytes := make([]byte, 32)
	if _, e = rand.Read(tokenBytes); e != nil {
		return e
	}
	token := fmt.Sprintf("%x", tokenBytes)
	if e = command.Start(); e != nil {
		return e
	}
	writer.Close()
	ended := make(chan error, 1)
	go func() { ended <- command.Wait() }()
	generation := "00000000-0000-4000-8000-000000000001"
	var received, sent atomic.Uint64
	var sessionLost atomic.Bool
	// Cleanup is owned solely by this invocation. No global recovery or service call.
	defer func() {
		state := gate.snapshot()
		if state.Generation == generation {
			_ = gate.command(context.Background(), companyCommand{ExpectedRevision: state.Revision, Generation: generation, Action: "block"})
		}
		input.Close()
		stopErr := stopChild(command, ended)
		report.CleanupConfirmed = stopErr == nil
		report.Finished = time.Now().UTC().Format(time.RFC3339)
		if result != nil {
			report.Error = result.Error()
		}
		if stopErr != nil {
			result = errors.Join(result, errors.New("temporary child cleanup incomplete"))
			report.Error = "temporary child cleanup incomplete"
		}
		report.Received = received.Load()
		report.Sent = sent.Load()

		data, _ := json.MarshalIndent(report, "", "  ")
		name := filepath.Join(r.EvidenceDirectory, "attempt-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
		fd, e := unix.Open(name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0600)
		if e == nil {
			f := os.NewFile(uintptr(fd), name)
			f.Write(data)
			f.Close()
		}
		if stopErr == nil {
			companyTestEvent("stopped", "临时公司通道已结束；没有操作正式网络服务、系统路由或 DNS。")
		} else {
			companyTestEvent("quarantined", "临时组件清理未确认，已锁定本轮测试；请保留诊断，不要反复连接。")
		}
	}()
	req := request{Server: r.Server, Username: r.Username, Group: r.Group, Password: r.Password, Token: token, OpenConnectPath: r.OpenConnectPath, CompanySocket: filepath.Join(dir, "company.sock"), CompanyProxyPort: gate.outer.Port, CompanyProxyToken: gate.token}
	e = json.NewEncoder(input).Encode(req)
	r.Password = ""
	req.Password = ""
	if e != nil {
		return errors.New("temporary session dispatch failed")
	}
	events := make(chan map[string]json.RawMessage, 32)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			var event map[string]json.RawMessage
			if json.Unmarshal(scanner.Bytes(), &event) == nil {
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	companyTestEvent("status", "正在验证公司线路身份；主 VPN 保持运行。")
	var ready map[string]json.RawMessage
	for ready == nil {
		select {
		case <-ctx.Done():
			return errors.New("company authentication timed out or cancelled")
		case event, ok := <-events:
			if !ok {
				return errors.New("company transport exited before authentication")
			}
			var kind string
			json.Unmarshal(event["event"], &kind)
			if kind == "ready" {
				ready = event
			} else if kind == "error" {
				var message string
				json.Unmarshal(event["message"], &message)
				companyTestEvent("error", message)
				return errors.New("company tunnel did not reach ready; inspect classified component event")
			}
		}
	}
	claim := companyBackendClaim{Token: token}
	json.Unmarshal(ready["companySocket"], &claim.SocketPath)
	json.Unmarshal(ready["companyPID"], &claim.PID)
	json.Unmarshal(ready["companyStartTime"], &claim.StartTime)
	json.Unmarshal(ready["dns"], &claim.DNSServers)
	json.Unmarshal(ready["remoteNetworks"], &claim.RemoteNetworks)
	json.Unmarshal(ready["remoteExcluded"], &claim.RemoteExcluded)
	report.Authenticated = true
	report.DNSServers = claim.DNSServers
	if e = gate.command(ctx, companyCommand{Generation: generation, Action: "prepare", Backend: &claim}); e != nil {
		return fmt.Errorf("company scope/peer validation failed: %w", e)
	}
	if e = gate.command(ctx, companyCommand{ExpectedRevision: gate.snapshot().Revision, Generation: generation, Action: "commit"}); e != nil {
		return e
	}
	report.BackendIdentityVerified = true
	companyTestEvent("status", "副隧道已认证，Unix 后端 UID/PID/start-time/CDHash 已核对；开始只读探测。")
	// Keep draining structured metrics; never write a raw credential/request log.
	statsDone := make(chan struct{})
	defer close(statsDone)
	go func() {
		for {
			select {
			case <-statsDone:
				return
			case event, ok := <-events:
				if !ok {
					sessionLost.Store(true)
					cancel()
					return
				}
				var kind string
				json.Unmarshal(event["event"], &kind)
				if kind == "error" || kind == "stopped" || kind == "quarantined" {
					sessionLost.Store(true)
					cancel()
					return
				}
				if kind == "stats" {
					var down, up uint64
					json.Unmarshal(event["received"], &down)
					json.Unmarshal(event["sent"], &up)
					received.Store(down)
					sent.Store(up)
				}
			}
		}
	}()
	target, _ := netip.ParsePrefix(r.Network)
	for _, port := range []string{"443", "80", "445"} {
		if e = ctx.Err(); e != nil {
			return e
		}
		p := companyTestProbe{Target: net.JoinHostPort(target.Addr().String(), port), Kind: "tcp"}
		probeCtx, done := context.WithTimeout(ctx, 5*time.Second)
		conn, err := gate.DialContext(probeCtx, "tcp", p.Target)
		done()
		p.Success = err == nil
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			p.Error = err.Error()
		}
		report.Probes = append(report.Probes, p)
		companyTestEvent("result", fmt.Sprintf("公司 IP TCP %s：%v", port, p.Success))
	}
	if len(claim.DNSServers) == 0 {
		companyTestEvent("result", "服务器未下发 DNS，未使用系统 DNS 代替公司 DNS。")
	}
	for _, server := range claim.DNSServers {
		for _, kind := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
			if e = ctx.Err(); e != nil {
				return e
			}
			reply := companyTestDNSQuery(ctx, gate, server, r.DNSName, kind)
			report.DNS = append(report.DNS, reply)
			companyTestEvent("result", fmt.Sprintf("公司 DNS %s %s：rcode=%s 地址=%s 错误=%s", server, reply.Type, reply.RCode, strings.Join(reply.Addresses, ","), reply.Error))
		}
	}
	// Only connect the explicitly approved /32. A DNS answer cannot widen scope.
	matched := false
	for _, reply := range report.DNS {
		if reply.Type == "A" && reply.Error == "" {
			for _, ip := range reply.Addresses {
				if ip == target.Addr().String() {
					matched = true
				}
			}
		}
	}
	if matched && r.DNSName != "" {
		for _, scheme := range []string{"https", "http"} {
			p := companyTestWeb(ctx, gate, r.DNSName, target.Addr(), scheme)
			report.Probes = append(report.Probes, p)
			companyTestEvent("result", fmt.Sprintf("公司网页 %s：HTTP %d，收到响应=%v（不代表业务登录通过），错误=%s", scheme, p.HTTPStatus, p.Success, p.Error))
		}
	} else {
		companyTestEvent("result", "域名解析未匹配批准的公司 /32，未向其它地址发起网页连接。")
	}
	if sessionLost.Load() {
		return errors.New("company transport ended during test")
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	companyTestEvent("complete", "本轮只读探测已结束，业务与 DNS 结果以上述明细为准；正在撤销临时通道。")
	return nil
}
func companyTestDNSQuery(parent context.Context, g *companyGate, server, hostname string, kind dnsmessage.Type) (out companyTestDNSResult) {
	out.Server = server
	out.Type = kind.String()
	if kind == dnsmessage.TypeA {
		out.Type = "A"
	} else if kind == dnsmessage.TypeAAAA {
		out.Type = "AAAA"
	}
	if hostname == "" {
		out.Error = "no company DNS hostname supplied"
		return
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	g.mu.Lock()
	backend := g.active
	g.mu.Unlock()
	if backend == nil {
		out.Error = "company backend blocked"
		return
	}
	name, e := dnsmessage.NewName(strings.TrimSuffix(hostname, ".") + ".")
	if e != nil {
		out.Error = "invalid query"
		return
	}
	var random [2]byte
	if _, e = rand.Read(random[:]); e != nil {
		out.Error = "DNS transaction ID unavailable"
		return
	}
	id := binary.BigEndian.Uint16(random[:])
	question := dnsmessage.Question{Name: name, Type: kind, Class: dnsmessage.ClassINET}
	wire, packErr := (&dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{question}}).Pack()
	if packErr != nil {
		out.Error = "DNS query packing failed"
		return
	}
	conn, e := (companyDNSDialer{g, backend}).DialContext(ctx, "tcp", net.JoinHostPort(server, "53"))
	if e != nil {
		out.Error = e.Error()
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	frame := append([]byte{byte(len(wire) >> 8), byte(len(wire))}, wire...)
	if _, e = conn.Write(frame); e != nil {
		out.Error = e.Error()
		return
	}
	var h [2]byte
	if _, e = io.ReadFull(conn, h[:]); e != nil {
		out.Error = e.Error()
		return
	}
	n := int(binary.BigEndian.Uint16(h[:]))
	if n < 12 || n > 4096 {
		out.Error = "invalid DNS frame"
		return
	}
	answer := make([]byte, n)
	if _, e = io.ReadFull(conn, answer); e != nil {
		out.Error = e.Error()
		return
	}
	out.RCode, out.Addresses, out.Aliases, e = companyTestParseDNS(answer, id, question)
	out.ResponseValidated = out.RCode != ""
	out.Resolved = e == nil && len(out.Addresses) > 0
	if e != nil {
		out.Error = e.Error()
	}
	return
}
func companyTestWeb(ctx context.Context, g *companyGate, host string, ip netip.Addr, scheme string) companyTestProbe {
	p := companyTestProbe{Target: scheme + "://" + host + "/", Kind: "http"}
	if (scheme != "http" && scheme != "https") || !validDirectDomain(host) {
		p.Error = "invalid test web URL"
		return p
	}
	port := "443"
	if scheme == "http" {
		port = "80"
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		h, p, e := net.SplitHostPort(address)
		if e != nil || h != host || p != port {
			return nil, errors.New("unapproved web destination")
		}
		return g.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
	}, TLSHandshakeTimeout: 6 * time.Second, ResponseHeaderTimeout: 6 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "HEAD", p.Target, nil)
	if e != nil {
		p.Error = e.Error()
		return p
	}
	req.Header.Set("User-Agent", "GoConnect-Temporary-ReadOnly-Test")
	response, e := client.Do(req)
	if e != nil {
		p.Error = e.Error()
		return p
	}
	response.Body.Close()
	p.Success = true
	p.HTTPStatus = response.StatusCode
	return p
}

func companyTestParseDNS(wire []byte, id uint16, question dnsmessage.Question) (rcode string, addresses, aliases []string, result error) {
	var response dnsmessage.Message
	normalize := func(n dnsmessage.Name) string { return strings.ToLower(strings.TrimSuffix(n.String(), ".")) }
	if e := response.Unpack(wire); e != nil || !response.Response || response.ID != id || response.Truncated || response.OpCode != 0 || len(response.Questions) != 1 {
		return "", nil, nil, errors.New("invalid or mismatched DNS response")
	}
	actual := response.Questions[0]
	if normalize(actual.Name) != normalize(question.Name) || actual.Class != question.Class || actual.Type != question.Type {
		return "", nil, nil, errors.New("mismatched DNS question")
	}
	rcode = response.RCode.String()
	if response.RCode != dnsmessage.RCodeSuccess {
		return rcode, nil, nil, fmt.Errorf("DNS response %s", rcode)
	}
	cnames := map[string]string{}
	records := map[string][]string{}
	for _, record := range response.Answers {
		owner := normalize(record.Header.Name)
		if !validDirectDomain(owner) || record.Header.Class != dnsmessage.ClassINET {
			return rcode, nil, nil, errors.New("invalid DNS answer owner or class")
		}
		switch body := record.Body.(type) {
		case *dnsmessage.CNAMEResource:
			target := normalize(body.CNAME)
			if record.Header.Type != dnsmessage.TypeCNAME || !validDirectDomain(target) || cnames[owner] != "" {
				return rcode, nil, nil, errors.New("invalid or duplicate DNS CNAME")
			}
			cnames[owner] = target
		case *dnsmessage.AResource:
			if record.Header.Type != dnsmessage.TypeA || question.Type != dnsmessage.TypeA {
				return rcode, nil, nil, errors.New("wrong DNS record type")
			}
			records[owner] = append(records[owner], netip.AddrFrom4(body.A).String())
		case *dnsmessage.AAAAResource:
			if record.Header.Type != dnsmessage.TypeAAAA || question.Type != dnsmessage.TypeAAAA {
				return rcode, nil, nil, errors.New("wrong DNS record type")
			}
			records[owner] = append(records[owner], netip.AddrFrom16(body.AAAA).String())
		default:
			return rcode, nil, nil, errors.New("unsupported DNS answer record")
		}
	}
	current := normalize(question.Name)
	seen := map[string]bool{current: true}
	consumed := 0
	for cnames[current] != "" {
		next := cnames[current]
		consumed++
		if consumed > 8 || seen[next] || len(records[current]) > 0 {
			return rcode, nil, nil, errors.New("DNS CNAME loop, ambiguity or excessive depth")
		}
		seen[next] = true
		aliases = append(aliases, next)
		current = next
	}
	if consumed != len(cnames) {
		return rcode, nil, nil, errors.New("unchained DNS CNAME")
	}
	for owner, values := range records {
		if owner != current {
			return rcode, nil, nil, errors.New("unrelated DNS address record")
		}
		for _, ip := range values {
			if !containsString(addresses, ip) {
				addresses = append(addresses, ip)
			}
		}
	}
	return rcode, addresses, aliases, nil
}
