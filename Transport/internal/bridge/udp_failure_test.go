package bridge

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type failedUDP struct {
	net.Conn
	closed  chan struct{}
	once    sync.Once
	writes  atomic.Int32
	partial bool
}

func (c *failedUDP) Read([]byte) (int, error) { <-c.closed; return 0, net.ErrClosed }
func (c *failedUDP) Write([]byte) (int, error) {
	c.writes.Add(1)
	if c.partial {
		return 1, nil
	}
	return 0, syscall.EPIPE
}
func (c *failedUDP) Close() error                     { c.once.Do(func() { close(c.closed) }); return nil }
func (c *failedUDP) SetReadDeadline(time.Time) error  { return nil }
func (c *failedUDP) SetWriteDeadline(time.Time) error { return nil }
func (c *failedUDP) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 5353}
}

type replacingUDPDialer struct {
	calls   atomic.Int32
	failed  *failedUDP
	address string
}

func (d *replacingUDPDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.calls.Add(1) == 1 {
		return d.failed, nil
	}
	return (&net.Dialer{}).DialContext(ctx, "udp", d.address)
}

func udpAssociation(t *testing.T, ctx context.Context, server *SOCKSServer) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(ctx, listener)
	t.Cleanup(func() { listener.Close() })
	c := authenticate(t, listener.Addr().String(), server.Token)
	t.Cleanup(func() { c.Close() })
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	h := make([]byte, 3)
	if _, err = io.ReadFull(c, h); err != nil || h[1] != 0 {
		t.Fatal("associate", err)
	}
	a, err := readAddress(c)
	if err != nil {
		t.Fatal(err)
	}
	u, err := net.Dial("udp", a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	return c, u
}

func TestUDPFailedRemoteIsRetiredWithoutReplayingDatagram(t *testing.T) {
	testUDPRetirement(t, false)
}

func TestUDPPartialWriteIsRetiredWithoutReplayingDatagram(t *testing.T) {
	testUDPRetirement(t, true)
}

func testUDPRetirement(t *testing.T, partial bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	echo, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 100)
		for {
			n, p, e := echo.ReadFrom(b)
			if e != nil {
				return
			}
			echo.WriteTo(b[:n], p)
		}
	}()
	d := &replacingUDPDialer{failed: &failedUDP{closed: make(chan struct{}), partial: partial}, address: echo.LocalAddr().String()}
	_, u := udpAssociation(t, ctx, &SOCKSServer{Dialer: d, Token: "fixture-token"})
	a, _ := encodedAddress("192.0.2.2:5353")
	prefix := append([]byte{0, 0, 0}, a...)
	u.Write(append(prefix, []byte("first-must-not-replay")...))
	select {
	case <-d.failed.closed:
	case <-time.After(time.Second):
		t.Fatal("failed UDP endpoint remained cached")
	}
	if d.failed.writes.Load() != 1 || d.calls.Load() != 1 {
		t.Fatal("failed datagram replayed")
	}
	u.SetDeadline(time.Now().Add(time.Second))
	u.Write(append(prefix, []byte("next-request")...))
	b := make([]byte, 100)
	n, err := u.Read(b)
	if err != nil {
		t.Fatal("next request could not recover", err)
	}
	if string(b[n-len("next-request"):n]) != "next-request" || d.calls.Load() != 2 {
		t.Fatal("wrong recovery")
	}
}

type blockingUDP struct {
	*failedUDP
	started chan struct{}
}

func (c *blockingUDP) Write([]byte) (int, error) {
	close(c.started)
	<-c.closed
	return 0, net.ErrClosed
}

type fixedUDPDialer struct{ conn net.Conn }

func (d fixedUDPDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return d.conn, nil
}
func TestUDPCancelUnblocksPendingRemoteWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &blockingUDP{failedUDP: &failedUDP{closed: make(chan struct{})}, started: make(chan struct{})}
	_, u := udpAssociation(t, ctx, &SOCKSServer{Dialer: fixedUDPDialer{c}, Token: "fixture-token"})
	a, _ := encodedAddress("192.0.2.2:5353")
	u.Write(append(append([]byte{0, 0, 0}, a...), []byte("pending")...))
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("write not started")
	}
	cancel()
	select {
	case <-c.closed:
	case <-time.After(time.Second):
		t.Fatal("cancel left write blocked")
	}
}
