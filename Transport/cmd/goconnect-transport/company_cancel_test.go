package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/sys/unix"
)

// These tests only use the existing loopback TCP/UNIX relay fixture. Counting
// UNIX endpoints inspects this isolated test process's descriptors, never a
// production process or a company destination. Each backend connection
// contributes two endpoints; the one known listener is accounted separately.
func companyCancelUnixSocketDescriptors(t *testing.T) int {
	t.Helper()
	directory, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	// Read names only: statting entries races with ordinary socket closure.
	names, err := directory.Readdirnames(-1)
	directory.Close()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		addr, err := unix.Getsockname(fd)
		if err != nil {
			continue
		}
		_, ok := addr.(*unix.SockaddrUnix)
		if !ok {
			continue
		}
		// Count even orphaned accepted endpoints after their peer has closed.
		// Getpeername would miss those on Darwin; SO_ACCEPTCONN is unavailable.
		count++
	}
	return count
}

func companyCancelWait(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s in loopback fixture", what)
	}
}

func companyCancelCleanupCounts(t *testing.T, g *companyGate, b *companyBackend) (int, int) {
	t.Helper()
	g.mu.Lock()
	flows := len(b.flows)
	g.mu.Unlock()
	endpoints := companyCancelUnixSocketDescriptors(t)
	if g.ctx.Err() == nil {
		// The one fixture backend listener remains open until root cancellation.
		endpoints--
	}
	return flows, endpoints
}

func companyCancelRequireCleanup(t *testing.T, g *companyGate, b *companyBackend) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		flows, channels := companyCancelCleanupCounts(t, g, b)
		if flows == 0 && channels == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("loopback relay retained root_flows=%d backend_unix_endpoints=%d", flows, channels)
		}
		time.Sleep(time.Millisecond)
	}
}

type companyCancelHTTPConn struct {
	*net.TCPConn // Preserve CloseWrite through this test-only observation wrapper.
	closed       chan struct{}
	once         sync.Once
}

func (c *companyCancelHTTPConn) Close() error {
	err := c.TCPConn.Close()
	c.once.Do(func() { close(c.closed) })
	return err
}

type companyCancelSingleListener struct {
	conn     net.Conn
	accepted atomic.Bool
}

func (l *companyCancelSingleListener) Accept() (net.Conn, error) {
	if l.accepted.Swap(true) {
		return nil, net.ErrClosed
	}
	return l.conn, nil
}
func (l *companyCancelSingleListener) Addr() net.Addr { return l.conn.LocalAddr() }
func (l *companyCancelSingleListener) Close() error   { return nil }

// The ordinary net/http server, rather than a synthetic cancel signal,
// observes peer EOF, cancels Request.Context, exits its handler and closes the
// accepted TCP socket. Serve's listener close does not close accepted sockets.
func companyCancelHTTPFixture(t *testing.T, streaming bool) (*companyGate, proxy.Dialer, <-chan struct{}, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	if endpoints := companyCancelUnixSocketDescriptors(t); endpoints != 0 {
		t.Fatalf("test process already has %d connected UNIX endpoints", endpoints)
	}
	entered, canceled, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		observed := &companyCancelHTTPConn{TCPConn: c.(*net.TCPConn), closed: closed}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if streaming {
				io.WriteString(w, "ready\n")
				w.(http.Flusher).Flush()
			}
			close(entered)
			<-r.Context().Done()
			close(canceled)
		})}
		_ = server.Serve(&companyCancelSingleListener{conn: observed})
		<-closed
		_ = server.Close()
	})
	return g, dialer, entered, canceled, closed
}

func companyCancelBegin(t *testing.T, dialer proxy.Dialer, streaming bool) (net.Conn, io.Reader) {
	t.Helper()
	c := companyRelayDial(t, dialer, 1)
	t.Cleanup(func() { c.Close() })
	if _, err := io.WriteString(c, "GET /slow HTTP/1.1\r\nHost: loopback-fixture\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if !streaming {
		return c, c
	}
	response, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	ready := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(response.Body, ready); err != nil || string(ready) != "ready\n" {
		t.Fatalf("stream did not start: %q %v", ready, err)
	}
	return c, response.Body
}

func companyCancelRequireTerminal(t *testing.T, c net.Conn, reader io.Reader) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := io.ReadAll(reader)
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		t.Fatalf("client remained waiting after cancellation: %v", err)
	}
	// EOF, RST and an interrupted HTTP chunk stream all prove termination.
}

func TestCompanyCancelRSTHTTPBackendReleases(t *testing.T) {
	g, dialer, entered, canceled, closed := companyCancelHTTPFixture(t, false)
	b := g.active
	c, _ := companyCancelBegin(t, dialer, false)
	companyCancelWait(t, entered, "HTTP handler")
	if _, endpoints := companyCancelCleanupCounts(t, g, b); endpoints != 2 {
		t.Fatalf("expected two observable backend UNIX endpoints, got %d", endpoints)
	}
	if err := c.(*net.TCPConn).SetLinger(0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	companyCancelWait(t, canceled, "real HTTP Request.Context cancellation")
	companyCancelWait(t, closed, "HTTP server socket close")
	requireCompanyFlowsReleased(t, g)
	companyCancelRequireCleanup(t, g, b)
}

func TestCompanyCancelRootReleasesPendingAndStreaming(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "pending"
		if streaming {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			g, dialer, entered, canceled, closed := companyCancelHTTPFixture(t, streaming)
			b := g.active
			c, reader := companyCancelBegin(t, dialer, streaming)
			companyCancelWait(t, entered, "HTTP handler")
			g.cancel()
			companyCancelRequireTerminal(t, c, reader)
			c.Close()
			companyCancelWait(t, canceled, "HTTP cancellation")
			companyCancelWait(t, closed, "HTTP connection close")
			requireCompanyFlowsReleased(t, g)
			companyCancelRequireCleanup(t, g, b)
		})
	}
}

func TestCompanyCancelGenerationReleasesPendingAndStreaming(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "pending"
		if streaming {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			g, dialer, entered, canceled, closed := companyCancelHTTPFixture(t, streaming)
			// The base fixture uses one root context. Give the company generation
			// its production-style child context so block does not cancel root.
			generationCtx, generationCancel := context.WithCancel(g.ctx)
			t.Cleanup(generationCancel)
			g.mu.Lock()
			b := g.active
			b.ctx, b.cancel, b.generation = generationCtx, generationCancel, companyTestGeneration
			g.state.State, g.state.Generation = "active", companyTestGeneration
			g.mu.Unlock()
			c, reader := companyCancelBegin(t, dialer, streaming)
			companyCancelWait(t, entered, "HTTP handler")
			if err := g.command(context.Background(), companyCommand{ExpectedRevision: g.snapshot().Revision, Generation: companyTestGeneration, Action: "block"}); err != nil {
				t.Fatal(err)
			}
			if g.ctx.Err() != nil || g.snapshot().State != "blocked" {
				t.Fatal("company generation block canceled root or did not block scope")
			}
			companyCancelRequireTerminal(t, c, reader)
			c.Close()
			companyCancelWait(t, canceled, "HTTP cancellation")
			companyCancelWait(t, closed, "HTTP connection close")
			companyCancelRequireCleanup(t, g, b)
		})
	}
}

func companyCancelUnresponsiveFixture(t *testing.T, idleBudget ...time.Duration) (*companyGate, proxy.Dialer, <-chan struct{}, chan struct{}) {
	t.Helper()
	budget := 150 * time.Millisecond
	if len(idleBudget) > 0 {
		budget = idleBudget[0]
	}
	if endpoints := companyCancelUnixSocketDescriptors(t); endpoints != 0 {
		t.Fatalf("test process already has %d connected UNIX endpoints", endpoints)
	}
	fin, release := make(chan struct{}), make(chan struct{})
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		// Receiving FIN is intentionally not treated as a cancellation by this
		// server: a valid protocol may finish computing after request EOF.
		_, _ = io.Copy(io.Discard, c)
		close(fin)
		<-release
		io.WriteString(c, "finished")
	}, budget)
	return g, dialer, fin, release
}

func TestCompanyCancelFINUnresponsiveServerBoundary(t *testing.T) {
	for _, fullClose := range []bool{false, true} {
		name := "client_half_close"
		if fullClose {
			name = "client_full_graceful_close"
		}
		t.Run(name, func(t *testing.T) {
			g, dialer, fin, release := companyCancelUnresponsiveFixture(t)
			b := g.active
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			c := companyRelayDial(t, dialer, 1)
			defer c.Close()
			io.WriteString(c, "request")
			if fullClose {
				c.Close()
			} else {
				c.(*net.TCPConn).CloseWrite()
			}
			companyCancelWait(t, fin, "server request EOF")
			// A FIN does not distinguish a valid request half-close from a full
			// graceful client close. Keeping the response direction open here
			// is a protocol boundary, not evidence of a newly introduced leak.
			flows, channels := companyCancelCleanupCounts(t, g, b)
			if flows != 1 || channels != 2 {
				t.Fatalf("expected pending response ownership: root_flows=%d backend_unix_endpoints=%d", flows, channels)
			}
			releaseOnce.Do(func() { close(release) })
			if !fullClose {
				got, err := io.ReadAll(c)
				if err != nil || string(got) != "finished" {
					t.Fatalf("legitimate response after FIN: %q %v", got, err)
				}
			}
			c.Close()
			requireCompanyFlowsReleased(t, g)
			companyCancelRequireCleanup(t, g, b)
		})
	}
}

func TestCompanyCancelRSTUnresponsiveServerReleases(t *testing.T) {
	g, dialer, fin, release := companyCancelUnresponsiveFixture(t)
	b := g.active
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	c := companyRelayDial(t, dialer, 1)
	io.WriteString(c, "request")
	if err := c.(*net.TCPConn).SetLinger(0); err != nil {
		c.Close()
		t.Fatal(err)
	}
	c.Close()
	companyCancelWait(t, fin, "server request EOF after client RST")
	deadline := time.Now().Add(2 * time.Second)
	for {
		flows, channels := companyCancelCleanupCounts(t, g, b)
		if flows == 0 && channels == 0 {
			break
		}
		if time.Now().After(deadline) {
			// Unlike FIN, a client RST is an unambiguous terminal error. There
			// is no client response left to preserve. Capture the existing
			// relay behavior without changing production code in this task.
			t.Errorf("client RST retained root_flows=%d backend_unix_endpoints=%d while server ignored FIN", flows, channels)
			break
		}
		time.Sleep(time.Millisecond)
	}
	releaseOnce.Do(func() { close(release) })
	requireCompanyFlowsReleased(t, g)
	companyCancelRequireCleanup(t, g, b)
}

func TestCompanyCancelServerEOFFiniteDrain(t *testing.T) {
	g, dialer := fixtureCompanyRelay(t, func(c net.Conn) {
		io.WriteString(c, "response")
		c.(*net.TCPConn).CloseWrite()
		io.Copy(io.Discard, c)
	}, 150*time.Millisecond)
	b := g.active
	c := companyRelayDial(t, dialer, 1)
	defer c.Close()
	got, err := io.ReadAll(c)
	if err != nil || string(got) != "response" {
		t.Fatalf("server half-close response: %q %v", got, err)
	}
	// Leave the client's write direction open. A server-originated EOF must
	// start the same finite drain as a client-originated EOF.
	companyCancelRequireCleanup(t, g, b)
}

func TestCompanyCancelLegacyZeroPreservesRelay(t *testing.T) {
	g, dialer, fin, release := companyCancelUnresponsiveFixture(t, 0)
	b := g.active
	var once sync.Once
	defer once.Do(func() { close(release) })
	c := companyRelayDial(t, dialer, 1)
	io.WriteString(c, "request")
	c.(*net.TCPConn).SetLinger(0)
	c.Close()
	companyCancelWait(t, fin, "legacy relay request EOF")
	flows, endpoints := companyCancelCleanupCounts(t, g, b)
	if flows != 1 || endpoints != 2 {
		t.Fatalf("zero policy changed legacy path: root_flows=%d backend_unix_endpoints=%d", flows, endpoints)
	}
	once.Do(func() { close(release) })
	requireCompanyFlowsReleased(t, g)
	companyCancelRequireCleanup(t, g, b)
}
