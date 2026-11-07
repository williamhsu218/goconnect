package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/sys/unix"
)

// The company stack never resolves names or carries UDP. The controller resolves
// company names over its verified backend and sends only a checked literal IP.
type companyLiteralDialer struct{ bridge.Dialer }

func (d companyLiteralDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	ip, parseErr := netip.ParseAddr(host)
	p, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || parseErr != nil || !ip.Is4() || portErr != nil || p == 0 || network != "tcp" {
		return nil, errors.New("company backend accepts literal IPv4 TCP only")
	}
	return d.Dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

func validateCompanyTransportRequest(r request) error {
	if r.CompanySocket == "" && r.CompanyProxyPort == 0 && r.CompanyProxyToken == "" {
		return nil
	}
	if r.CompanySocket == "" || r.CompanyProxyPort == 0 || !validCompanyProxyToken(r.CompanyProxyToken) {
		return errors.New("invalid company transport")
	}
	return validateCompanySocketPath(r.CompanySocket)
}
func validCompanyProxyToken(token string) bool {
	if len(token) < 32 || len(token) > 200 {
		return false
	}
	for _, c := range token {
		if c < 33 || c > 126 || strings.ContainsRune(":@/?#%\\", c) {
			return false
		}
	}
	return true
}
func validateCompanySocketPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 102 {
		return errors.New("invalid company socket path")
	}
	parent := filepath.Dir(path)
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return errors.New("company socket parent must be canonical")
	}
	st, err := os.Lstat(parent)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return errors.New("company socket parent must be private")
	}
	uid, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(uid.Uid) != os.Geteuid() {
		return errors.New("company socket parent owner mismatch")
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("company socket already exists")
	}
	return nil
}
func companyProxyURL(port uint16, token string) string {
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), User: url.UserPassword("goconnect", token)}).String()
}
func companyProcessStartTime(pid int) (uint64, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	return uint64(p.Proc.P_starttime.Sec)*1_000_000 + uint64(p.Proc.P_starttime.Usec), nil
}
func companyTransportListener(path string) (net.Listener, error) {
	if path == "" {
		return net.Listen("tcp4", "127.0.0.1:0")
	}
	if err := validateCompanySocketPath(path); err != nil {
		return nil, err
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return companySOCKSListener{l}, nil
}

// Intercept only the method/auth/request prefix, then replay it into the existing
// SOCKS implementation. This prevents even a successful UDP ASSOCIATE response.
type companySOCKSListener struct{ net.Listener }

func (l companySOCKSListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &companySOCKSConn{Conn: c}, nil
}

type companySOCKSConn struct {
	net.Conn
	mu        sync.Mutex
	pending   []byte
	state     int
	remaining int
	denied    bool
}

// Keep the write half-close visible through the protocol-filtering wrapper.
// Closing the whole socket here would discard the response to a half-closed
// request; swallowing EOF instead retains a SOCKS handler and its flow slot.
func (c *companySOCKSConn) CloseWrite() error { return companyCloseWrite(c.Conn) }

func companyCloseWrite(conn net.Conn) error {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return errors.ErrUnsupported
}

func (c *companySOCKSConn) Read(p []byte) (int, error) {
	// SOCKSServer uses fixed-size reads; classify only protocol header transitions.
	n, err := c.Conn.Read(p)
	if n == 0 {
		return n, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.denied {
		return 0, io.EOF
	}
	c.pending = append(c.pending, p[:n]...)
	for {
		switch c.state {
		case 0:
			if len(c.pending) < 2 {
				return n, err
			}
			c.remaining = int(c.pending[1])
			c.pending = c.pending[2:]
			c.state = 1
		case 1:
			if len(c.pending) < c.remaining {
				return n, err
			}
			c.pending = c.pending[c.remaining:]
			c.state = 2
		case 2:
			if len(c.pending) < 2 {
				return n, err
			}
			c.remaining = int(c.pending[1])
			c.pending = c.pending[2:]
			c.state = 3
		case 3:
			if len(c.pending) < c.remaining+1 {
				return n, err
			}
			c.pending = c.pending[c.remaining:]
			c.remaining = int(c.pending[0])
			c.pending = c.pending[1:]
			c.state = 4
		case 4:
			if len(c.pending) < c.remaining {
				return n, err
			}
			c.pending = c.pending[c.remaining:]
			c.state = 5
		case 5:
			if len(c.pending) < 3 {
				return n, err
			}
			command := c.pending[1]
			c.pending = nil
			c.state = 6
			if command != 1 {
				c.denied = true
				_, _ = c.Conn.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
				return 0, io.EOF
			}
		default:
			c.pending = nil
			return n, err
		}
	}
}

// Root owns this fixed listener. OpenConnect's every outer CONNECT is checked
// before dialing, including authentication redirects and TCP reconnects.
type companyOuterProxy struct {
	Port     uint16
	listener net.Listener
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   map[net.Conn]struct{}
	closed   bool
	done     chan struct{}
}
type companyOuterDial func(context.Context, string, string) (net.Conn, error)

func newCompanyOuterProxy(ctx context.Context, host string, port uint16, ips []string, iface string, token string) (*companyOuterProxy, error) {
	if iface == "" {
		return nil, errors.New("company outer interface required")
	}
	nic, err := net.InterfaceByName(iface)
	if err != nil || nic.Index <= 0 || nic.Flags&net.FlagLoopback != 0 || strings.HasPrefix(iface, "utun") {
		return nil, errors.New("invalid company outer interface")
	}
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, c syscall.RawConn) error {
		if network != "tcp4" && network != "tcp" {
			return errors.New("company outer IPv4 TCP only")
		}
		var inner error
		if e := c.Control(func(fd uintptr) { inner = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, nic.Index) }); e != nil {
			return e
		}
		return inner
	}}
	return newCompanyOuterProxyWithDial(ctx, host, port, ips, token, func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp4", address)
	})
}
func newCompanyOuterProxyWithDial(parent context.Context, host string, port uint16, ips []string, token string, dial companyOuterDial) (*companyOuterProxy, error) {
	host = strings.ToLower(host)
	if host == "" || strings.ContainsAny(host, " /\\\r\n\x00:@") || port == 0 || !validCompanyProxyToken(token) || len(ips) == 0 || len(ips) > 4 || dial == nil {
		return nil, errors.New("invalid company outer policy")
	}
	approved := make(map[string]bool)
	for _, raw := range ips {
		ip, err := netip.ParseAddr(raw)
		if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return nil, errors.New("invalid company outer address")
		}
		approved[ip.String()] = true
	}
	if len(approved) != len(ips) {
		return nil, errors.New("duplicate company outer address")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	p := &companyOuterProxy{Port: uint16(listener.Addr().(*net.TCPAddr).Port), listener: listener, cancel: cancel, active: make(map[net.Conn]struct{}), done: make(chan struct{})}
	go func() { <-ctx.Done(); _ = p.Close() }()
	go func() {
		defer close(p.done)
		limit := make(chan struct{}, 32)
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			select {
			case limit <- struct{}{}:
				if !p.track(c) {
					c.Close()
					<-limit
					continue
				}
				go func() {
					defer func() { <-limit; p.untrack(c); c.Close() }()
					p.serve(ctx, c, host, port, ips, approved, token, dial)
				}()
			default:
				c.Close()
			}
		}
	}()
	return p, nil
}
func (p *companyOuterProxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.active[c] = struct{}{}
	return true
}
func (p *companyOuterProxy) untrack(c net.Conn) { p.mu.Lock(); delete(p.active, c); p.mu.Unlock() }
func (p *companyOuterProxy) Close() error {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	err := p.listener.Close()
	for c := range p.active {
		_ = c.Close()
	}
	return err
}
func (p *companyOuterProxy) serve(ctx context.Context, c net.Conn, host string, port uint16, ips []string, approved map[string]bool, token string, dial companyOuterDial) {
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	// A hard cap covers request line plus all headers. Do not use ReadRequest's
	// unbounded ReadLine accumulation on a loopback endpoint accessible to users.
	reader := bufio.NewReader(c)
	var header bytes.Buffer
	for header.Len() <= 8192 {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return
		}
		header.Write(line)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}
	if header.Len() > 8192 || !bytes.HasSuffix(header.Bytes(), []byte("\r\n\r\n")) {
		return
	}
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(header.Bytes())))
	if err != nil {
		return
	}
	reject := func(code int) {
		if code == 407 {
			_, _ = fmt.Fprint(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"GoConnect Company\"\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
			return
		}
		_, _ = fmt.Fprintf(c, "HTTP/1.1 %d Rejected\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", code)
	}
	if req.Method != http.MethodConnect || req.URL.Scheme != "" || req.URL.RawQuery != "" || req.ContentLength > 0 || len(req.TransferEncoding) > 0 {
		reject(405)
		return
	}
	auth := req.Header.Get("Proxy-Authorization")
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("goconnect:"+token))
	if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
		reject(407)
		return
	}
	target, targetPort, e := net.SplitHostPort(req.RequestURI)
	if e != nil || targetPort != strconv.Itoa(int(port)) || (strings.ToLower(target) != host && !approved[target]) {
		reject(403)
		return
	}
	// Match the complete authority, not a hostname prefix, user-info or suffix.
	if req.Host != req.RequestURI {
		reject(403)
		return
	}
	dialIPs := ips
	if approved[target] {
		dialIPs = []string{target}
	}
	var upstream net.Conn
	for _, ip := range dialIPs {
		upstream, err = dial(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(int(port))))
		if err == nil {
			break
		}
	}
	if err != nil || upstream == nil {
		reject(502)
		return
	}
	defer upstream.Close()
	if !p.track(upstream) {
		return
	}
	defer p.untrack(upstream)
	if _, err = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, reader)
		if h, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = h.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, upstream)
		if h, ok := c.(interface{ CloseWrite() error }); ok {
			_ = h.CloseWrite()
		}
		done <- struct{}{}
	}()
	// A client may half-close its request while still expecting a response.
	// Keep both directions until both complete or the controller cancels.
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return
		case <-done:
		}
	}
}
