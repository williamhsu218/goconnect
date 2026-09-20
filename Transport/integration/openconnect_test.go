package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
)

// This fixture implements the minimum TLS/CSTP exchange needed to exercise the
// real bundled OpenConnect binary. It is not Cisco/ocserv interoperability evidence.
type cstpChannel struct {
	net.Conn
	reader *bufio.ReadWriter
	mu     sync.Mutex
}

func (c *cstpChannel) frame(kind byte, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	header := []byte{'S', 'T', 'F', 1, byte(len(body) >> 8), byte(len(body)), kind, 0}
	if _, err := c.Conn.Write(append(header, body...)); err != nil {
		return err
	}
	return nil
}
func (c *cstpChannel) Write(data []byte) (int, error) {
	err := c.frame(0, data)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
func (c *cstpChannel) Read(data []byte) (int, error) {
	for {
		header := make([]byte, 8)
		if _, err := io.ReadFull(c.reader, header); err != nil {
			return 0, err
		}
		if !bytes.Equal(header[:4], []byte{'S', 'T', 'F', 1}) {
			return 0, errors.New("invalid CSTP header")
		}
		n := int(binary.BigEndian.Uint16(header[4:6]))
		if n > len(data) {
			return 0, io.ErrShortBuffer
		}
		if _, err := io.ReadFull(c.reader, data[:n]); err != nil {
			return 0, err
		}
		switch header[6] {
		case 0:
			return n, nil
		case 3:
			_ = c.frame(4, nil)
		case 5:
			return 0, io.EOF
		}
	}
}

type helper struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	events chan map[string]any
	ended  chan error
}

func startHelper(t *testing.T, server, ca string) *helper {
	t.Helper()
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("set GOCONNECT_TEST_RUNTIME to a packaged Runtime directory")
	}
	h := &helper{events: make(chan map[string]any, 100), ended: make(chan error, 1)}
	h.cmd = exec.Command(filepath.Join(runtime, "bin/GoConnectTransport"), "session")
	h.cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	var err error
	h.input, err = h.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := h.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := map[string]any{"server": server, "username": "fixture-user", "password": "synthetic-secret-never-log", "group": "", "token": strings.Repeat("local-fixture-token-", 3), "openconnectPath": filepath.Join(runtime, "bin/openconnect"), "caFile": ca}
	if err = json.NewEncoder(h.input).Encode(r); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var event map[string]any
			if json.Unmarshal(scanner.Bytes(), &event) == nil {
				h.events <- event
			}
		}
		close(h.events)
	}()
	go func() { h.ended <- h.cmd.Wait() }()
	t.Cleanup(func() {
		_ = h.input.Close()
		select {
		case <-h.ended:
		case <-time.After(5 * time.Second):
			_ = h.cmd.Process.Kill()
		}
	})
	return h
}

func TestBundledOpenConnectCSTPTCPUDPAndDisconnect(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("requires packaged runtime")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stack, err := bridge.New([]netip.Addr{netip.MustParseAddr("192.0.2.2")}, nil, 1400)
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()
	tcp, err := stack.Network.ListenTCPAddrPort(netip.MustParseAddrPort("192.0.2.2:8080"))
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		conn, e := tcp.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	udp, err := stack.Network.DialUDPAddrPort(netip.MustParseAddrPort("192.0.2.2:5353"), netip.AddrPort{})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buffer := make([]byte, 1024)
		n, peer, e := udp.ReadFrom(buffer)
		if e == nil {
			_, _ = udp.WriteTo(buffer[:n], peer)
		}
	}()
	var authorized atomic.Bool
	closed := make(chan struct{})
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			if !authorized.Load() || !strings.Contains(r.Header.Get("Cookie"), "fixture-cookie") {
				http.Error(w, "forbidden", 403)
				return
			}
			conn, buffer, e := w.(http.Hijacker).Hijack()
			if e != nil {
				return
			}
			fmt.Fprint(buffer, "HTTP/1.1 200 CONNECTED\r\nX-CSTP-Version: 1\r\nX-CSTP-MTU: 1400\r\nX-CSTP-Address: 192.0.2.1\r\nX-CSTP-Netmask: 255.255.255.0\r\nX-CSTP-DNS: 192.0.2.2\r\nX-CSTP-Keepalive: 30\r\n\r\n")
			_ = buffer.Flush()
			channel := &cstpChannel{Conn: conn, reader: buffer}
			_ = stack.Pump(ctx, channel)
			close(closed)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 65536))
		w.Header().Set("Content-Type", "text/xml")
		if bytes.Contains(body, []byte("<username>fixture-user</username>")) && bytes.Contains(body, []byte("<password>synthetic-secret-never-log</password>")) {
			authorized.Store(true)
			fmt.Fprint(w, `<?xml version="1.0"?><config-auth client="vpn" type="complete"><auth id="success"/><session-token>fixture-cookie</session-token></config-auth>`)
		} else {
			fmt.Fprint(w, `<?xml version="1.0"?><config-auth client="vpn" type="auth-request"><auth id="main"><form method="post" action="/auth"><input type="text" name="username" label="Username:"/><input type="password" name="password" label="Password:"/></form></auth></config-auth>`)
		}
	}))
	defer fixture.Close()
	ca := filepath.Join(t.TempDir(), "fixture-ca.pem")
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Certificate().Raw}), 0o600)
	h := startHelper(t, fixture.URL, ca)
	port := 0
	deadline := time.After(15 * time.Second)
	for port == 0 {
		select {
		case event, ok := <-h.events:
			if !ok {
				t.Fatal("helper exited before ready")
			}
			if event["event"] == "error" {
				t.Fatalf("connection failed: %v", event["message"])
			}
			if event["event"] == "ready" {
				port = int(event["port"].(float64))
				if event["gateway"] != "127.0.0.1" {
					t.Fatal("missing gateway exclusion")
				}
			}
		case <-deadline:
			t.Fatal("CSTP handshake timed out")
		}
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	token := strings.Repeat("local-fixture-token-", 3)
	dialer, _ := proxy.SOCKS5("tcp", address, &proxy.Auth{User: "goconnect", Password: token}, proxy.Direct)
	conn, err := dialer.Dial("tcp", "192.0.2.2:8080")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	body := bytes.Repeat([]byte("CSTP-payload\n"), 300)
	_, _ = conn.Write(body)
	response := make([]byte, len(body))
	if _, err = io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if !bytes.Equal(response, body) {
		t.Fatal("CSTP TCP payload changed")
	}
	// UDP ASSOCIATE goes through the same authenticated SOCKS endpoint.
	control, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = control.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	_, _ = io.ReadFull(control, reply)
	auth := append([]byte{1, 9}, []byte("goconnect")...)
	auth = append(auth, byte(len(token)))
	auth = append(auth, []byte(token)...)
	_, _ = control.Write(auth)
	_, _ = io.ReadFull(control, reply)
	_, _ = control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	bind := make([]byte, 10)
	if _, err = io.ReadFull(control, bind); err != nil || bind[1] != 0 {
		t.Fatal("UDP association failed", err)
	}
	udpConn, err := net.Dial("udp", net.JoinHostPort(net.IP(bind[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(bind[8:])))))
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()
	_ = udpConn.SetDeadline(time.Now().Add(5 * time.Second))
	packet := append([]byte{0, 0, 0, 1, 192, 0, 2, 2, 20, 233}, []byte("CSTP-UDP")...)
	_, _ = udpConn.Write(packet)
	response = make([]byte, 1024)
	n, err := udpConn.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response[:n], packet) {
		t.Fatal("CSTP UDP payload changed")
	}
	_ = control.Close()
	_ = h.input.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("stdin EOF did not close VPN connection")
	}
	t.Log("Actual bundled OpenConnect: certificate-validated login, CSTP, TCP, UDP, and EOF disconnect passed")
}

func TestUntrustedCertificateIsRejected(t *testing.T) {
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "must not authenticate", 500) }))
	defer fixture.Close()
	h := startHelper(t, fixture.URL, "")
	rejected := false
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, ok := <-h.events:
			if !ok {
				if !rejected {
					t.Fatal("no certificate rejection event")
				}
				return
			}
			if event["event"] == "ready" {
				t.Fatal("untrusted certificate accepted")
			}
			if message, ok := event["message"].(string); ok {
				if strings.Contains(message, "证书") {
					rejected = true
				}
				if strings.Contains(message, "synthetic-secret") {
					t.Fatal("credential leaked into events")
				}
			}
		case <-deadline:
			t.Fatal("certificate rejection timed out")
		}
	}
}
