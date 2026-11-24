package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/sys/unix"
)

type pathCheck struct {
	Result     string              `json:"result"`
	DurationMS int64               `json:"durationMs"`
	Attempts   []diagnosticDetails `json:"attempts,omitempty"`
}

type pathHealth struct {
	State       string    `json:"state"`
	SampledAt   int64     `json:"sampledAt"`
	ProxyHTTPS  pathCheck `json:"proxyHTTPS"`
	ProxyDoH    pathCheck `json:"proxyDoH"`
	DirectHTTPS pathCheck `json:"directHTTPS"`
}

const pathHealthInterval = 45 * time.Second

type pathHealthTracker struct{ failures int }

func (s *pathHealthTracker) observe(sample pathHealth) pathHealth {
	if sample.ProxyHTTPS.Result == "ok" && sample.ProxyDoH.Result == "ok" {
		s.failures = 0
		sample.State = "reachable"
	} else {
		s.failures++
		sample.State = "checking"
		if s.failures >= 2 {
			sample.State = "degraded"
		}
	}
	return sample
}

type contextDial func(context.Context, string, string) (net.Conn, error)

type pathProbeEndpoint struct {
	url, provider         string
	family                int
	additionalHTTPSStatus int
}

var probeIPv4 = []pathProbeEndpoint{
	{url: "https://1.1.1.1", provider: "cloudflare", family: 4},
	{url: "https://8.8.8.8", provider: "google", family: 4},
}
var probeIPv6 = []pathProbeEndpoint{
	{url: "https://[2606:4700:4700::1111]", provider: "cloudflare", family: 6},
	{url: "https://[2001:4860:4860::8888]", provider: "google", family: 6},
}

const aliDNSProbeHost = "dns.alidns.com"

var pathProbeQuestion = dnsmessage.Question{Name: dnsmessage.MustNewName("example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}

func probeDNSMessage(id uint16) []byte {
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{pathProbeQuestion},
	}
	wire, _ := query.Pack()
	return wire
}

func validPathProbeAnswer(wire []byte, id uint16) bool {
	var answer dnsmessage.Message
	if answer.Unpack(wire) != nil || !answer.Response || answer.Truncated || answer.ID != id || answer.RCode != dnsmessage.RCodeSuccess ||
		len(answer.Questions) != 1 || answer.Questions[0] != pathProbeQuestion {
		return false
	}
	for _, rr := range answer.Answers {
		if rr.Header.Name == pathProbeQuestion.Name && rr.Header.Class == pathProbeQuestion.Class {
			if _, ok := rr.Body.(*dnsmessage.AResource); ok {
				return true
			}
		}
	}
	return false
}

func physicalProbeDial(network originalNetwork) (contextDial, []pathProbeEndpoint) {
	gateway, err := netip.ParseAddr(network.Gateway)
	if err != nil || !physicalInterface.MatchString(network.Interface) {
		return nil, nil
	}
	nic, err := net.InterfaceByName(network.Interface)
	if err != nil || nic.Index <= 0 || nic.Flags&net.FlagUp == 0 {
		return nil, nil
	}
	family, endpoints := "tcp4", probeIPv4
	aliDNSIP, ipVersion := "223.5.5.5", 4
	if gateway.Is6() {
		family, endpoints = "tcp6", probeIPv6
		aliDNSIP, ipVersion = "2400:3200::1", 6
	}
	// An empty HEAD request receives HTTP 400 from AliDNS without sending a DNS question.
	aliDNS := pathProbeEndpoint{
		url: "https://" + aliDNSProbeHost + "/dns-query", provider: "alidns", family: ipVersion,
		additionalHTTPSStatus: http.StatusBadRequest,
	}
	dialer := physicalSocketDialer(nic.Index, gateway.Is6())
	return func(ctx context.Context, _, address string) (net.Conn, error) {
		// Dial the literal IP on the physical interface while TLS validates the hostname.
		if address == aliDNSProbeHost+":443" {
			address = net.JoinHostPort(aliDNSIP, "443")
		}
		return dialer.DialContext(ctx, family, address)
	}, append([]pathProbeEndpoint{aliDNS}, endpoints...)
}

func physicalSocketDialer(index int, ipv6 bool) *net.Dialer {
	level, option := unix.IPPROTO_IP, unix.IP_BOUND_IF
	if ipv6 {
		level, option = unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF
	}
	return &net.Dialer{ControlContext: func(_ context.Context, _, _ string, conn syscall.RawConn) error {
		var bindErr error
		if err := conn.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptInt(int(fd), level, option, index)
		}); err != nil {
			return err
		}
		return bindErr
	}}
}

func newPathProbeClient(dial contextDial) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: dial, DisableKeepAlives: true, ForceAttemptHTTP2: true,
			MaxResponseHeaderBytes: 16 << 10,
		},
		Timeout:       4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type probeTrace struct {
	started    time.Time
	firstByte  atomic.Pointer[time.Time]
	tlsStarted atomic.Pointer[time.Time]
	tlsDone    atomic.Pointer[probeTLSCompletion]
}

type probeTLSCompletion struct {
	at        time.Time
	connected bool
}

func (s *probeTrace) hooks() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GotFirstResponseByte: func() { now := time.Now(); s.firstByte.Store(&now) },
		TLSHandshakeStart: func() {
			now := time.Now()
			s.tlsStarted.Store(&now)
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			s.tlsDone.Store(&probeTLSCompletion{at: time.Now(), connected: err == nil})
		},
	}
}

func (s *probeTrace) finish(attempt *diagnosticDetails) {
	total := time.Since(s.started)
	attempt.DurationMS = total.Milliseconds()
	duration := func(d time.Duration) *int64 {
		ms := d.Milliseconds()
		return &ms
	}
	if at := s.firstByte.Load(); at != nil {
		attempt.FirstByteMS = duration(at.Sub(s.started))
	}
	begin := s.tlsStarted.Load()
	if begin == nil {
		attempt.ConnectMS = duration(total)
		return
	}
	tlsStarted := min(begin.Sub(s.started), total)
	attempt.ConnectMS = duration(tlsStarted)
	done := s.tlsDone.Load()
	if done == nil {
		attempt.TLSMS = duration(total - tlsStarted)
		return
	}
	tlsDone := max(tlsStarted, min(done.at.Sub(s.started), total))
	attempt.TLSMS = duration(tlsDone - tlsStarted)
	if done.connected {
		attempt.ResponseMS = duration(total - tlsDone)
	}
}

func probeTimedOut(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func checkProbeEndpoint(ctx context.Context, client *http.Client, endpoint pathProbeEndpoint, dns bool) (attempt diagnosticDetails) {
	attempt = diagnosticDetails{Stage: "path_health", ProbeTarget: endpoint.provider, Family: endpoint.family}
	target := endpoint.url
	var id uint16
	method := http.MethodHead
	var body io.Reader
	if dns {
		id = uint16(time.Now().UnixNano())
		method = http.MethodPost
		target += "/dns-query"
		body = bytes.NewReader(probeDNSMessage(id))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		attempt.Reason = "request"
		return
	}
	if dns {
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
	}
	// httptrace callbacks may arrive on transport goroutines. Only the
	// final trace stage is read by the caller after Client.Do returns.
	trace := &probeTrace{started: time.Now()}
	defer trace.finish(&attempt)
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace.hooks()))
	resp, err := client.Do(req)
	if err != nil {
		attempt.TimedOut = probeTimedOut(err)
		done := trace.tlsDone.Load()
		switch {
		case done != nil && done.connected:
			attempt.Reason = "response"
		case trace.tlsStarted.Load() != nil:
			attempt.Reason = "tls"
		default:
			attempt.Reason = "connect"
		}
		return
	}
	defer resp.Body.Close()
	attempt.HTTPStatus = resp.StatusCode
	if !dns {
		attempt.Reason = "http_status"
		if (resp.StatusCode >= 200 && resp.StatusCode < 400) || resp.StatusCode == endpoint.additionalHTTPSStatus {
			attempt.Reason = "ok"
		}
		return
	}
	if resp.StatusCode != http.StatusOK {
		attempt.Reason = "http_status"
		return
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	wire, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if mediaType != "application/dns-message" || err != nil || len(wire) > 4096 || !validPathProbeAnswer(wire, id) {
		attempt.Reason = "dns_response"
		attempt.TimedOut = probeTimedOut(err)
		return
	}
	attempt.Reason = "ok"
	return
}

func checkPath(ctx context.Context, dial contextDial, endpoints []pathProbeEndpoint, dns bool) pathCheck {
	if dial == nil {
		return pathCheck{Result: "unavailable"}
	}
	started := time.Now()
	client := newPathProbeClient(dial)
	defer client.CloseIdleConnections()
	result := pathCheck{Result: "unavailable"}
	for _, endpoint := range endpoints {
		if ctx.Err() != nil {
			break
		}
		attempt := checkProbeEndpoint(ctx, client, endpoint, dns)
		result.Attempts = append(result.Attempts, attempt)
		result.Result = attempt.Reason
		if result.Result == "ok" {
			break
		}
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result
}

func runPathHealthSample(ctx context.Context, viaProxy, viaDirect contextDial, proxyEndpoints, directEndpoints []pathProbeEndpoint) pathHealth {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	proxyHTTPS, proxyDoH, directHTTPS := make(chan pathCheck, 1), make(chan pathCheck, 1), make(chan pathCheck, 1)
	go func() { proxyHTTPS <- checkPath(ctx, viaProxy, proxyEndpoints, false) }()
	go func() { proxyDoH <- checkPath(ctx, viaProxy, proxyEndpoints, true) }()
	go func() { directHTTPS <- checkPath(ctx, viaDirect, directEndpoints, false) }()
	result := pathHealth{ProxyHTTPS: <-proxyHTTPS, ProxyDoH: <-proxyDoH, DirectHTTPS: <-directHTTPS}
	result.SampledAt = time.Now().UnixMilli()
	return result
}

func monitorPathHealth(ctx context.Context, r routeRequest, original originalNetwork) <-chan pathHealth {
	samples := make(chan pathHealth, 1)
	proxyDial, err := sessionSOCKSDialer(r.SOCKSPort, r.Token)
	if err != nil {
		return nil
	}
	directDial, directEndpoints := physicalProbeDial(original)
	proxyEndpoints := append(append([]pathProbeEndpoint{}, probeIPv4...), probeIPv6...)
	go func() {
		defer close(samples)
		ticker := time.NewTicker(pathHealthInterval)
		defer ticker.Stop()
		for ctx.Err() == nil {
			sample := runPathHealthSample(ctx, proxyDial, directDial, proxyEndpoints, directEndpoints)
			select {
			case <-ctx.Done():
				return
			case samples <- sample:
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return samples
}
