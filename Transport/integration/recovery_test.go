package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
)

// Preserve the fixture's IP stack across TLS connections, as a VPN gateway
// preserves a parent session. Packet loss during outages is intentional.
type resumableCSTP struct {
	mu      sync.Mutex
	current *cstpChannel
	changed chan struct{}
	closed  bool
}

func (s *resumableCSTP) replace(c *cstpChannel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if c != nil {
			c.Close()
		}
		return
	}
	if s.current != nil {
		s.current.Close()
	}
	s.current = c
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *resumableCSTP) Read(b []byte) (int, error) {
	for {
		s.mu.Lock()
		c, wake, closed := s.current, s.changed, s.closed
		s.mu.Unlock()
		if closed {
			return 0, io.EOF
		}
		if c == nil {
			<-wake
			continue
		}
		n, err := c.Read(b)
		if err == nil {
			return n, nil
		}
		s.mu.Lock()
		if s.current == c {
			s.current = nil
		}
		s.mu.Unlock()
	}
}
func (s *resumableCSTP) Write(b []byte) (int, error) {
	s.mu.Lock()
	c := s.current
	s.mu.Unlock()
	if c != nil {
		_ = c.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = c.Write(b)
	}
	return len(b), nil
}
func (s *resumableCSTP) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.changed)
		if s.current != nil {
			s.current.Close()
		}
	}
	return nil
}

type recoveryFixture struct {
	tunnel     *resumableCSTP
	server     *httptest.Server
	ca         string
	logins     atomic.Int32
	connects   atomic.Int32
	reject     atomic.Int32
	stall      atomic.Bool
	newAddress atomic.Bool
	done       chan struct{}
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	f := &recoveryFixture{tunnel: &resumableCSTP{changed: make(chan struct{})}, done: make(chan struct{})}
	stack, err := bridge.New([]netip.Addr{netip.MustParseAddr("192.0.2.2")}, nil, 1400)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tcp, err := stack.Network.ListenTCPAddrPort(netip.MustParseAddrPort("192.0.2.2:8080"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, err := stack.Network.DialUDPAddrPort(netip.MustParseAddrPort("192.0.2.2:5353"), netip.AddrPort{})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		b := make([]byte, 4096)
		for {
			n, p, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			udp.WriteTo(b[:n], p)
		}
	}()
	go stack.Pump(ctx, f.tunnel)
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			f.connects.Add(1)
			if !strings.Contains(r.Header.Get("Cookie"), "fixture-cookie") {
				http.Error(w, "denied", 401)
				return
			}
			if f.stall.Load() {
				select {
				case <-r.Context().Done():
				case <-f.done:
				}
				return
			}
			if code := f.reject.Load(); code != 0 {
				http.Error(w, "temporary", int(code))
				return
			}
			conn, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			address := "192.0.2.1"
			if f.newAddress.Load() {
				address = "192.0.2.9"
			}
			fmt.Fprintf(buffer, "HTTP/1.1 200 CONNECTED\r\nX-CSTP-Version: 1\r\nX-CSTP-MTU: 1400\r\nX-CSTP-Address: %s\r\nX-CSTP-Netmask: 255.255.255.0\r\nX-CSTP-DNS: 192.0.2.2\r\nX-CSTP-DPD: 30\r\nX-CSTP-Keepalive: 20\r\n\r\n", address)
			buffer.Flush()
			f.tunnel.replace(&cstpChannel{Conn: conn, reader: buffer})
			return
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 65536))
		w.Header().Set("Content-Type", "text/xml")
		if bytes.Contains(b, []byte("<password>synthetic-secret-never-log</password>")) {
			f.logins.Add(1)
			fmt.Fprint(w, `<?xml version="1.0"?><config-auth client="vpn" type="complete"><auth id="success"/><session-token>fixture-cookie</session-token></config-auth>`)
		} else {
			fmt.Fprint(w, `<?xml version="1.0"?><config-auth client="vpn" type="auth-request"><auth id="main"><form method="post" action="/auth"><input type="text" name="username" label="Username:"/><input type="password" name="password" label="Password:"/></form></auth></config-auth>`)
		}
	}))
	f.ca = filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err = os.WriteFile(f.ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(f.done)
		cancel()
		f.tunnel.Close()
		tcp.Close()
		udp.Close()
		stack.Close()
		f.server.Close()
	})
	return f
}

func waitRecoveryEvent(t *testing.T, h *helper, want string, timeout time.Duration) map[string]any {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-h.events:
			if !ok {
				t.Fatalf("helper ended waiting for %s", want)
			}
			if e["event"] == want {
				return e
			}
			if e["event"] == "ready" {
				t.Fatal("recovery replaced the original packet/SOCKS endpoint")
			}
			if e["event"] == "error" || e["event"] == "stopped" {
				t.Fatalf("unexpected %v waiting for %s", e, want)
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}
func recoveryDialer(t *testing.T, port int) proxy.Dialer {
	t.Helper()
	d, e := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &proxy.Auth{User: "goconnect", Password: strings.Repeat("local-fixture-token-", 3)}, &net.Dialer{Timeout: 3 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func echoRecovery(t *testing.T, c net.Conn) {
	t.Helper()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	b := bytes.Repeat([]byte("recovery-data"), 600)
	if _, e := c.Write(b); e != nil {
		t.Fatal(e)
	}
	got := make([]byte, len(b))
	if _, e := io.ReadFull(c, got); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, b) {
		t.Fatal("payload changed")
	}
}

func recoveryUDP(t *testing.T, port int) net.Conn {
	t.Helper()
	control, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { control.Close() })
	control.SetDeadline(time.Now().Add(5 * time.Second))
	control.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	io.ReadFull(control, reply)
	token := strings.Repeat("local-fixture-token-", 3)
	auth := append([]byte{1, 9}, []byte("goconnect")...)
	auth = append(auth, byte(len(token)))
	auth = append(auth, []byte(token)...)
	control.Write(auth)
	if _, err = io.ReadFull(control, reply); err != nil || reply[1] != 0 {
		t.Fatal("UDP authentication", err)
	}
	control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	bind := make([]byte, 10)
	if _, err = io.ReadFull(control, bind); err != nil || bind[1] != 0 {
		t.Fatal("UDP association", err)
	}
	control.SetDeadline(time.Time{})
	u, err := net.Dial("udp", net.JoinHostPort(net.IP(bind[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(bind[8:])))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	return u
}
func echoRecoveryUDP(t *testing.T, u net.Conn) {
	t.Helper()
	packet := append([]byte{0, 0, 0, 1, 192, 0, 2, 2, 20, 233}, []byte("recovered-UDP")...)
	u.SetDeadline(time.Now().Add(5 * time.Second))
	if _, e := u.Write(packet); e != nil {
		t.Fatal(e)
	}
	got := make([]byte, 1024)
	n, e := u.Read(got)
	if e != nil || !bytes.Equal(packet, got[:n]) {
		t.Fatal("UDP recovery failed", e)
	}
}

func TestBundledOpenConnectRepeatedRecovery(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("requires packaged runtime")
	}
	if os.Getenv("GOCONNECT_TEST_CPU_PRESSURE") == "1" {
		previous := runtime.GOMAXPROCS(2)
		stop := make(chan struct{})
		var workers sync.WaitGroup
		var work atomic.Uint64
		for i := 0; i < 8; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				var n uint64
				for {
					select {
					case <-stop:
						work.Add(n)
						return
					default:
						for j := 0; j < 10000; j++ {
							n = n*1664525 + 1013904223
						}
						if n == 0 {
							runtime.Gosched()
						}
					}
				}
			}()
		}
		t.Cleanup(func() {
			close(stop)
			workers.Wait()
			runtime.GOMAXPROCS(previous)
			t.Logf("8 CPU pressure workers drained, checksum %d", work.Load())
		})
	}
	f := newRecoveryFixture(t)
	h := startHelper(t, f.server.URL, f.ca)
	ready := waitRecoveryEvent(t, h, "ready", 10*time.Second)
	port := int(ready["port"].(float64))
	c, err := recoveryDialer(t, port).Dial("tcp", "192.0.2.2:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	echoRecovery(t, c)
	u := recoveryUDP(t, port)
	echoRecoveryUDP(t, u)
	for i := 0; i < 2; i++ {
		f.reject.Store(503)
		f.tunnel.replace(nil)
		waitRecoveryEvent(t, h, "reconnecting", 10*time.Second)
		// The server is unavailable longer than the old zero-retry setting.
		time.Sleep(1500 * time.Millisecond)
		f.reject.Store(0)
		waitRecoveryEvent(t, h, "reconnected", 15*time.Second)
		echoRecovery(t, c)    // SAME TCP connection and SOCKS port survive both outages.
		echoRecoveryUDP(t, u) // SAME UDP association and relay port also survive.
		fresh, e := recoveryDialer(t, port).Dial("tcp", "192.0.2.2:8080")
		if e != nil {
			t.Fatal(e)
		}
		echoRecovery(t, fresh)
		fresh.Close()
	}
	if f.logins.Load() != 1 || f.connects.Load() < 5 {
		t.Fatal("password replay or no real retry", f.logins.Load(), f.connects.Load())
	}
	h.input.Close()
	waitRecoveryEvent(t, h, "stopped", 5*time.Second)
}

func TestBundledOpenConnectRecoveryTerminalAndCancel(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("requires packaged runtime")
	}
	for _, mode := range []string{"cookie_rejected", "address_changed", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newRecoveryFixture(t)
			h := startHelper(t, f.server.URL, f.ca)
			waitRecoveryEvent(t, h, "ready", 10*time.Second)
			switch mode {
			case "cookie_rejected":
				f.reject.Store(401)
			case "address_changed":
				f.newAddress.Store(true)
			case "cancel":
				f.reject.Store(503)
			}
			f.tunnel.replace(nil)
			if mode == "cancel" {
				waitRecoveryEvent(t, h, "reconnecting", 10*time.Second)
				h.input.Close()
			}
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			stopped := false
			cookie := false
			for {
				select {
				case e, ok := <-h.events:
					if !ok {
						if !stopped {
							t.Fatal("no terminal event")
						}
						if mode == "cookie_rejected" && !cookie {
							t.Fatal("lost auth reason")
						}
						if f.logins.Load() != 1 {
							t.Fatal("credentials replayed")
						}
						return
					}
					if e["event"] == "reconnected" || e["event"] == "ready" {
						t.Fatal("unsafe session revived")
					}
					if e["event"] == "stopped" {
						stopped = true
					}
					if e["code"] == "openconnect.cookie_rejected" {
						cookie = true
					}
				case <-deadline.C:
					t.Fatal("terminal/cancel did not stop promptly")
				}
			}
		})
	}
}

func TestBundledOpenConnectRecoveryHardDeadline(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("requires packaged runtime")
	}
	f := newRecoveryFixture(t)
	h := startHelper(t, f.server.URL, f.ca)
	waitRecoveryEvent(t, h, "ready", 10*time.Second)
	// A TLS connection that accepts but never completes CONNECT must not make
	// OpenConnect's sleep-only retry budget unbounded.
	f.stall.Store(true)
	f.tunnel.replace(nil)
	waitRecoveryEvent(t, h, "reconnecting", 10*time.Second)
	start := time.Now()
	timer := time.NewTimer(85 * time.Second)
	defer timer.Stop()
	expired := false
	for {
		select {
		case e, ok := <-h.events:
			if !ok {
				if !expired || time.Since(start) < 70*time.Second {
					t.Fatal("missing real hard deadline", expired, time.Since(start))
				}
				return
			}
			if e["code"] == "openconnect.recovery_expired" {
				expired = true
			}
			if e["event"] == "reconnected" || e["event"] == "ready" {
				t.Fatal("stalled handshake reported recovered")
			}
		case <-timer.C:
			t.Fatal("hard deadline did not stop stalled connection")
		}
	}
}

func TestBundledOpenConnectSchedulingPause(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("requires packaged runtime")
	}
	f := newRecoveryFixture(t)
	h := startHelper(t, f.server.URL, f.ca)
	pid := 0
	port := 0
	deadline := time.After(10 * time.Second)
	for port == 0 || pid == 0 {
		select {
		case e := <-h.events:
			if e["code"] == "openconnect.started" {
				pid = int(e["value"].(float64))
			}
			if e["event"] == "ready" {
				port = int(e["port"].(float64))
			}
		case <-deadline:
			t.Fatal("startup timed out")
		}
	}
	c, err := recoveryDialer(t, port).Dial("tcp", "192.0.2.2:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	echoRecovery(t, c)
	// Only this test's child is suspended, never the installed VPN or host.
	if err = syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGCONT)
	writeDone := make(chan error, 1)
	payload := bytes.Repeat([]byte("paused-packets"), 40000)
	c.SetDeadline(time.Now().Add(25 * time.Second))
	go func() { _, e := c.Write(payload); writeDone <- e }()
	time.Sleep(6500 * time.Millisecond)
	if err = syscall.Kill(pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if err = <-writeDone; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("pause corrupted payload")
	}
	echoRecovery(t, c)
	h.input.Close()
	waitRecoveryEvent(t, h, "stopped", 5*time.Second)
}
