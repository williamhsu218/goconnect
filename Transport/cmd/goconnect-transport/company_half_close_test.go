package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"goconnect.local/transport/internal/company"
	"golang.org/x/net/proxy"
)

// Exercise both production SOCKS wrappers with real loopback TCP/UNIX sockets.
// No system routes, VPN, privileged service, or company host is involved.
type companyRelayFixtureDialer struct{ address string }

func (d companyRelayFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address != "10.201.0.9:443" {
		return nil, fmt.Errorf("unexpected fixture target: %s %s", network, address)
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp4", d.address)
}

func fixtureCompanyRelay(t *testing.T, serve func(net.Conn)) (*companyGate, proxy.Dialer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close() })
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				serve(c)
			}()
		}
	}()
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	backend, err := companyTransportListener(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	token := strings.Repeat("b", 64)
	go func() {
		_ = (&bridge.SOCKSServer{Token: token, Dialer: companyLiteralDialer{companyRelayFixtureDialer{target.Addr().String()}}}).Serve(ctx, backend)
	}()
	policy, err := company.NewPolicy(company.Policy{Networks: []string{"10.201.0.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	birth, err := companyProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	g := &companyGate{ctx: ctx, cancel: cancel, uid: uint32(os.Getuid()), token: strings.Repeat("g", 64)}
	g.verify = g.verifyBackend
	g.active = &companyBackend{
		claim:  companyBackendClaim{SocketPath: path, PID: os.Getpid(), StartTime: birth, Token: token},
		policy: policy, ctx: ctx, cancel: cancel, flows: map[*companyFlow]bool{},
	}
	g.listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	go func() {
		_ = (&bridge.SOCKSServer{Token: g.token, Dialer: g}).Serve(ctx, companySOCKSListener{g.listener})
	}()
	dialer, err := proxy.SOCKS5("tcp", g.listener.Addr().String(), &proxy.Auth{User: "goconnect", Password: g.token}, &net.Dialer{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return g, dialer
}

func companyRelayDial(t *testing.T, dialer proxy.Dialer, request int) net.Conn {
	t.Helper()
	// Dial returns the real TCP transport so the fixture client can half-close
	// it, just as a native browser's TCP peer can. DialContext wraps that socket.
	c, err := dialer.Dial("tcp", "10.201.0.9:443")
	if err != nil {
		t.Fatalf("request %d cannot acquire company flow: %v", request, err)
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	return c
}

func requireCompanyFlowsReleased(t *testing.T, g *companyGate) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		g.mu.Lock()
		n := len(g.active.flows)
		g.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("completed requests retained %d company flows", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCompanyRelayShortRequestsReleaseCapacity(t *testing.T) {
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		r, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			return
		}
		r.Body.Close()
		io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK")
	})
	// More than the unchanged 256-flow limit: sequential completed requests must
	// recycle capacity rather than eventually making every company port fail.
	for i := 1; i <= 300; i++ {
		c := companyRelayDial(t, dialer, i)
		if _, err := io.WriteString(c, "GET /history HTTP/1.1\r\nHost: fixture\r\n\r\n"); err != nil {
			c.Close()
			t.Fatalf("request %d write: %v", i, err)
		}
		response, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			c.Close()
			t.Fatalf("request %d response: %v", i, err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		c.Close()
		if err != nil || string(body) != "OK" {
			t.Fatalf("request %d body: %q %v", i, body, err)
		}
	}
	requireCompanyFlowsReleased(t, g)
}

func TestCompanyRelayHalfClosePreservesDelayedResponse(t *testing.T) {
	payload := bytes.Repeat([]byte("delayed company response\n"), 16000)
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		request, err := io.ReadAll(c)
		if err == nil && string(request) == "request until EOF" {
			time.Sleep(10 * time.Millisecond)
			c.Write(payload)
		}
	})
	c := companyRelayDial(t, dialer, 1)
	defer c.Close()
	io.WriteString(c, "request until EOF")
	if err := c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(c)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("delayed response after half-close: bytes=%d want=%d err=%v", len(got), len(payload), err)
	}
	c.Close()
	requireCompanyFlowsReleased(t, g)
}

func TestCompanyRelayServerEOFVisibleToClient(t *testing.T) {
	response := "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		r, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			return
		}
		r.Body.Close()
		io.WriteString(c, response)
		// HTTP servers can close an idle keep-alive socket. The browser must
		// receive EOF rather than reuse a connection whose server has exited.
	})
	c := companyRelayDial(t, dialer, 1)
	defer c.Close()
	io.WriteString(c, "GET /history HTTP/1.1\r\nHost: fixture\r\n\r\n")
	got, err := io.ReadAll(c)
	if err != nil || string(got) != response {
		t.Fatalf("server EOF hidden from client: bytes=%d err=%v", len(got), err)
	}
	c.Close()
	requireCompanyFlowsReleased(t, g)
}

func TestCompanyRelayKeepAliveReusesFlow(t *testing.T) {
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		reader := bufio.NewReader(c)
		for {
			r, err := http.ReadRequest(reader)
			if err != nil {
				return
			}
			r.Body.Close()
			if _, err := io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"); err != nil {
				return
			}
		}
	})
	c := companyRelayDial(t, dialer, 1)
	defer c.Close()
	reader := bufio.NewReader(c)
	for i := 0; i < 10; i++ {
		io.WriteString(c, "GET /history HTTP/1.1\r\nHost: fixture\r\n\r\n")
		r, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil || string(body) != "OK" {
			t.Fatalf("keep-alive request %d: %q %v", i, body, err)
		}
	}
	c.(*net.TCPConn).CloseWrite()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	c.Close()
	requireCompanyFlowsReleased(t, g)
}
