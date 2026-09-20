package bridge

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/sys/unix"
)

func pairedStacks(t *testing.T) (*Stack, *Stack, context.Context) {
	t.Helper()
	client, err := New([]netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1")}, nil, 1400)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New([]netip.Addr{netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("2001:db8::2")}, nil, 1400)
	if err != nil {
		t.Fatal(err)
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	for i, s := range []*Stack{client, server} {
		file := os.NewFile(uintptr(fds[i]), "test-packet-socket")
		channel, err := net.FileConn(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			if err := s.Pump(ctx, channel); err != nil && ctx.Err() == nil {
				t.Logf("packet pump stopped: %v", err)
			}
		}()
	}
	t.Cleanup(func() { cancel(); client.Close(); server.Close() })
	return client, server, ctx
}

func socksListener(t *testing.T, ctx context.Context, stack *Stack) (string, string) {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("test-session-", 4)
	go func() { _ = (&SOCKSServer{Dialer: stack, Token: token}).Serve(ctx, l) }()
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().String(), token
}

func TestSOCKSTCPOverIsolatedStackAndHalfClose(t *testing.T) {
	for _, host := range []string{"192.0.2.2", "2001:db8::2"} {
		t.Run(host, func(t *testing.T) {
			client, server, ctx := pairedStacks(t)
			listener, err := server.Network.ListenTCPAddrPort(netip.AddrPortFrom(netip.MustParseAddr(host), 8080))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			payload := bytes.Repeat([]byte("packet transport 🐚\n"), 700)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				body, e := io.ReadAll(conn)
				if e == nil {
					_, _ = conn.Write(body)
				}
			}()
			address, token := socksListener(t, ctx, client)
			dialer, err := proxy.SOCKS5("tcp", address, &proxy.Auth{User: "goconnect", Password: token}, proxy.Direct)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := dialer.Dial("tcp", net.JoinHostPort(host, "8080"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			_ = conn.(*net.TCPConn).CloseWrite()
			got, err := io.ReadAll(conn)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("response truncated: got %d want %d", len(got), len(payload))
			}
			if client.Sent.Load() == 0 || client.Received.Load() == 0 {
				t.Fatal("no VPN packet traffic")
			}
		})
	}
}

func authenticate(t *testing.T, address, token string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	if _, err = io.ReadFull(conn, reply); err != nil || !bytes.Equal(reply, []byte{5, 2}) {
		t.Fatal("method handshake", err)
	}
	data := append([]byte{1, 9}, []byte("goconnect")...)
	data = append(data, byte(len(token)))
	data = append(data, []byte(token)...)
	_, _ = conn.Write(data)
	if _, err = io.ReadFull(conn, reply); err != nil || reply[1] != 0 {
		t.Fatal("authentication", err)
	}
	return conn
}

func TestSOCKSUDPOverIsolatedStack(t *testing.T) {
	client, server, ctx := pairedStacks(t)
	listener, err := server.Network.DialUDPAddrPort(netip.MustParseAddrPort("192.0.2.2:5353"), netip.AddrPort{})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		buf := make([]byte, 1024)
		n, peer, e := listener.ReadFrom(buf)
		if e == nil {
			_, _ = listener.WriteTo(buf[:n], peer)
		}
	}()
	address, token := socksListener(t, ctx, client)
	control := authenticate(t, address, token)
	defer control.Close()
	_, _ = control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	header := make([]byte, 3)
	if _, err = io.ReadFull(control, header); err != nil || header[1] != 0 {
		t.Fatal("UDP associate", err)
	}
	relay, err := readAddress(control)
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.Dial("udp", relay)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(5 * time.Second))
	target, _ := encodedAddress("192.0.2.2:5353")
	packet := append(append([]byte{0, 0, 0}, target...), []byte("udp-through-vpn")...)
	_, _ = udp.Write(packet)
	buffer := make([]byte, 1024)
	n, err := udp.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer[:n], packet) {
		t.Fatal("wrong UDP reply")
	}
}

func TestSOCKSRejectsUnauthenticatedClients(t *testing.T) {
	client, _, ctx := pairedStacks(t)
	address, token := socksListener(t, ctx, client)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte{5, 1, 0})
	response := make([]byte, 2)
	_, err = io.ReadFull(conn, response)
	if err != nil || response[1] != 255 {
		t.Fatal("no-auth was accepted", err)
	}
	dialer, _ := proxy.SOCKS5("tcp", address, &proxy.Auth{User: "goconnect", Password: token + "wrong"}, proxy.Direct)
	if c, e := dialer.Dial("tcp", "192.0.2.2:80"); e == nil {
		c.Close()
		t.Fatal("wrong password was accepted")
	}
}

func TestMissingVPNDNSNeverFallsBackToHostResolver(t *testing.T) {
	client, _, _ := pairedStacks(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := client.DialContext(ctx, "tcp", "localhost:80"); err == nil {
		conn.Close()
		t.Fatal("unexpected host fallback")
	}
}
