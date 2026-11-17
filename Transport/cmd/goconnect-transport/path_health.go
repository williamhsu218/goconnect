package main

import (
	"bytes"
	"context"
	"crypto/tls"
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
	Result     string `json:"result"`
	DurationMS int64  `json:"durationMs"`
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

var probeIPv4 = []string{"https://1.1.1.1", "https://8.8.8.8"}
var probeIPv6 = []string{"https://[2606:4700:4700::1111]", "https://[2001:4860:4860::8888]"}

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

func physicalProbeDial(network originalNetwork) (contextDial, []string) {
	gateway, err := netip.ParseAddr(network.Gateway)
	if err != nil || !physicalInterface.MatchString(network.Interface) {
		return nil, nil
	}
	nic, err := net.InterfaceByName(network.Interface)
	if err != nil || nic.Index <= 0 || nic.Flags&net.FlagUp == 0 {
		return nil, nil
	}
	family, level, option, endpoints := "tcp4", unix.IPPROTO_IP, unix.IP_BOUND_IF, probeIPv4
	if gateway.Is6() {
		family, level, option, endpoints = "tcp6", unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, probeIPv6
	}
	dialer := &net.Dialer{ControlContext: func(_ context.Context, _, _ string, conn syscall.RawConn) error {
		var bindErr error
		if err := conn.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptInt(int(fd), level, option, nic.Index)
		}); err != nil {
			return err
		}
		return bindErr
	}}
	return func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, family, address)
	}, endpoints
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
	tlsStarted atomic.Bool
	tlsReady   atomic.Bool
}

func (s *probeTrace) hooks() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		TLSHandshakeStart: func() { s.tlsStarted.Store(true) },
		TLSHandshakeDone:  func(_ tls.ConnectionState, err error) { s.tlsReady.Store(err == nil) },
	}
}

func checkProbeEndpoint(ctx context.Context, client *http.Client, endpoint string, dns bool) string {
	var id uint16
	method := http.MethodHead
	var body io.Reader
	if dns {
		id = uint16(time.Now().UnixNano())
		method = http.MethodPost
		endpoint += "/dns-query"
		body = bytes.NewReader(probeDNSMessage(id))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return "request"
	}
	if dns {
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
	}
	// httptrace callbacks may arrive on transport goroutines. Only the
	// final trace stage is read by the caller after Client.Do returns.
	trace := new(probeTrace)
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace.hooks()))
	resp, err := client.Do(req)
	if err != nil {
		if trace.tlsReady.Load() {
			return "response"
		}
		if trace.tlsStarted.Load() {
			return "tls"
		}
		return "connect"
	}
	defer resp.Body.Close()
	if !dns {
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return "ok"
		}
		return "http_status"
	}
	if resp.StatusCode != http.StatusOK {
		return "http_status"
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	wire, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if mediaType != "application/dns-message" || err != nil || len(wire) > 4096 || !validPathProbeAnswer(wire, id) {
		return "dns_response"
	}
	return "ok"
}

func checkPath(ctx context.Context, dial contextDial, endpoints []string, dns bool) pathCheck {
	if dial == nil {
		return pathCheck{Result: "unavailable"}
	}
	started := time.Now()
	client := newPathProbeClient(dial)
	defer client.CloseIdleConnections()
	result := "unavailable"
	for _, endpoint := range endpoints {
		if ctx.Err() != nil {
			break
		}
		result = checkProbeEndpoint(ctx, client, endpoint, dns)
		if result == "ok" {
			break
		}
	}
	return pathCheck{Result: result, DurationMS: time.Since(started).Milliseconds()}
}

func runPathHealthSample(ctx context.Context, viaProxy, viaDirect contextDial, proxyEndpoints, directEndpoints []string) pathHealth {
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
	proxyEndpoints := append(append([]string{}, probeIPv4...), probeIPv6...)
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
