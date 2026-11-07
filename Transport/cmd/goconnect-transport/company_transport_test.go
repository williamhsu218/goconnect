package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
)

type companyTestDialer struct{ calls atomic.Int32 }

func (d *companyTestDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.calls.Add(1)
	a, b := net.Pipe()
	go b.Close()
	return a, nil
}
func TestCompanyLiteralDialerRejectsNonTCPAndNames(t *testing.T) {
	d := &companyTestDialer{}
	wrapped := companyLiteralDialer{d}
	for _, test := range []struct{ network, address string }{{"udp", "10.4.5.6:53"}, {"tcp", "server.corp.test:443"}, {"tcp", "[fd00::1]:443"}, {"tcp", "10.4.5.6:0"}, {"tcp", "bad"}} {
		if c, e := wrapped.DialContext(context.Background(), test.network, test.address); e == nil {
			c.Close()
			t.Fatalf("accepted %s %s", test.network, test.address)
		}
	}
	if d.calls.Load() != 0 {
		t.Fatal("rejected target reached underlying stack")
	}
	c, e := wrapped.DialContext(context.Background(), "tcp", "10.4.5.6:443")
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	if d.calls.Load() != 1 {
		t.Fatal("literal target not forwarded")
	}
}
func companyTestSocketDir(t *testing.T) string {
	t.Helper()
	dir, e := os.MkdirTemp("/private/tmp", "gct-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	return dir
}
func TestCompanyTransportSocketSecurity(t *testing.T) {
	dir := companyTestSocketDir(t)
	path := filepath.Join(dir, "backend.sock")
	r := request{CompanySocket: path, CompanyProxyPort: 23000, CompanyProxyToken: strings.Repeat("a", 32)}
	if e := validateCompanyTransportRequest(r); e != nil {
		t.Fatal(e)
	}
	if e := validateCompanyTransportRequest(request{}); e != nil {
		t.Fatal("legacy request changed", e)
	}
	r.CompanyProxyToken = "short"
	if e := validateCompanyTransportRequest(r); e == nil {
		t.Fatal("short token accepted")
	}
	r.CompanyProxyToken = strings.Repeat("a", 32)
	os.Chmod(dir, 0755)
	if e := validateCompanyTransportRequest(r); e == nil {
		t.Fatal("public directory accepted")
	}
	os.Chmod(dir, 0700)
	os.WriteFile(path, []byte("old"), 0600)
	if e := validateCompanyTransportRequest(r); e == nil {
		t.Fatal("occupied path accepted")
	}
	os.Remove(path)
	link := filepath.Join(dir, "symlink")
	os.Symlink(dir, link)
	r.CompanySocket = filepath.Join(link, "backend.sock")
	if e := validateCompanyTransportRequest(r); e == nil {
		t.Fatal("symlink parent accepted")
	}
}
func TestCompanyUnixListenerRejectsUDPAssociate(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	l, e := companyTransportListener(path)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := &companyTestDialer{}
	go (&bridge.SOCKSServer{Dialer: companyLiteralDialer{dialer}, Token: strings.Repeat("a", 32)}).Serve(ctx, l)
	c, e := net.Dial("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte{5, 1, 2})
	b := make([]byte, 2)
	if _, e = io.ReadFull(c, b); e != nil || b[1] != 2 {
		t.Fatal("greeting", e, b)
	}
	auth := append([]byte{1, 9}, []byte("goconnect")...)
	auth = append(auth, 32)
	auth = append(auth, strings.Repeat("a", 32)...)
	c.Write(auth)
	if _, e = io.ReadFull(c, b); e != nil || b[1] != 0 {
		t.Fatal("authentication", e, b)
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	reply := make([]byte, 10)
	if _, e = io.ReadFull(c, reply); e != nil || reply[1] != 7 {
		t.Fatal("UDP associate did not fail", e, reply)
	}
	if dialer.calls.Load() != 0 {
		t.Fatal("UDP reached stack")
	}
}
func TestCompanyOuterProxyAuthorityAndAuthentication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	token := strings.Repeat("b", 32)
	var calls atomic.Int32
	proxy, e := newCompanyOuterProxyWithDial(ctx, "vpn.corp.test", 443, []string{"203.0.113.20"}, token, func(ctx context.Context, network, address string) (net.Conn, error) {
		calls.Add(1)
		if network != "tcp" || address != "203.0.113.20:443" {
			t.Errorf("unexpected outer dial %s %s", network, address)
		}
		a, b := net.Pipe()
		go func() { defer b.Close(); io.Copy(b, b) }()
		return a, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	defer proxy.Close()
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("goconnect:"+token))
	check := func(method, target, authorization string, want int) {
		t.Helper()
		c, e := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(proxy.Port))))
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(2 * time.Second))
		io.WriteString(c, method+" "+target+" HTTP/1.1\r\nHost: "+target+"\r\nProxy-Authorization: "+authorization+"\r\n\r\n")
		reader := bufio.NewReader(c)
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(line, " "+strconv.Itoa(want)+" ") {
			t.Fatalf("response=%q wanted%d", line, want)
		}
		if want == 200 {
			for {
				line, e = reader.ReadString('\n')
				if e != nil {
					t.Fatal(e)
				}
				if line == "\r\n" {
					break
				}
			}
			c.Write([]byte("ping"))
			b := make([]byte, 4)
			if _, e = io.ReadFull(reader, b); e != nil || string(b) != "ping" {
				t.Fatal("tunnel bytes", e, string(b))
			}
		}
	}
	check("CONNECT", "vpn.corp.test:443", auth, 200)
	check("CONNECT", "203.0.113.20:443", auth, 200)
	check("CONNECT", "vpn.corp.test.evil:443", auth, 403)
	check("CONNECT", "redirect.corp.test:443", auth, 403)
	check("CONNECT", "vpn.corp.test:8443", auth, 403)
	check("CONNECT", "198.51.100.55:443", auth, 403)
	check("CONNECT", "vpn.corp.test:443", "Basic invalid", 407)
	check("GET", "vpn.corp.test:443", auth, 405)
	if calls.Load() != 2 {
		t.Fatalf("rejected outer targets dialed: %d", calls.Load())
	}
}
func TestCompanyOuterProxyCancelClosesActiveConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	token := strings.Repeat("c", 32)
	p, e := newCompanyOuterProxyWithDial(ctx, "vpn.corp.test", 443, []string{"10.255.0.1"}, token, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("fixture") })
	if e != nil {
		t.Fatal(e)
	}
	c, e := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p.Port))))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	cancel()
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("proxy listener survived cancellation")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = c.Read(make([]byte, 1)); e == nil {
		t.Fatal("active connection survived cancellation")
	}
	p.Close()
}
func TestCompanyOuterProxyRejectsUnsafePolicy(t *testing.T) {
	for _, ips := range [][]string{{}, {"127.0.0.1"}, {"169.254.1.2"}, {"0.0.0.0"}, {"224.0.0.1"}, {"::1"}, {"vpn.corp.test"}, {"10.1.1.1", "10.1.1.1"}} {
		p, e := newCompanyOuterProxyWithDial(context.Background(), "vpn.corp.test", 443, ips, strings.Repeat("a", 32), func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("invalid policy dialed")
			return nil, nil
		})
		if e == nil {
			p.Close()
			t.Fatalf("accepted %v", ips)
		}
	}
	if p, e := newCompanyOuterProxy(context.Background(), "vpn.corp.test", 443, []string{"203.0.113.1"}, "lo0", strings.Repeat("a", 32)); e == nil {
		p.Close()
		t.Fatal("loopback physical interface accepted")
	}
}
