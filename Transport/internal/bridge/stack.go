package bridge

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// Stack owns an isolated TCP/IP stack. It never opens a system TUN or installs routes.
type Stack struct {
	Device   tun.Device
	Network  *netstack.Net
	Received atomic.Uint64
	Sent     atomic.Uint64
	once     sync.Once
}

func New(addresses, dns []netip.Addr, mtu int) (*Stack, error) {
	if len(addresses) == 0 || mtu < 576 || mtu > 65535 {
		return nil, errors.New("invalid tunnel parameters")
	}
	dev, network, err := netstack.CreateNetTUN(addresses, dns, mtu)
	if err != nil {
		return nil, err
	}
	return &Stack{Device: dev, Network: network}, nil
}

func (s *Stack) Close() { s.once.Do(func() { _ = s.Device.Close() }) }
func (s *Stack) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Deliberately no host-network fallback, including DNS failures.
	return s.Network.DialContext(ctx, network, address)
}

func (s *Stack) Pump(ctx context.Context, transport io.ReadWriteCloser) error {
	results := make(chan error, 2)
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, err := transport.Read(buffer)
			if err != nil {
				results <- err
				return
			}
			if n == 0 {
				continue
			}
			if _, err = s.Device.Write([][]byte{buffer[:n]}, 0); err != nil {
				results <- err
				return
			}
			s.Received.Add(uint64(n))
		}
	}()
	go func() {
		buffers := [][]byte{make([]byte, 65535)}
		sizes := []int{0}
		discard := false
		for {
			n, err := s.Device.Read(buffers, sizes, 0)
			if err != nil {
				results <- err
				return
			}
			for i := 0; i < n; i++ {
				if discard {
					continue
				}
				written, err := writePacket(ctx, transport, buffers[i][:sizes[i]])
				if err != nil {
					results <- err
					discard = true
					continue
				}
				if written != sizes[i] {
					results <- io.ErrShortWrite
					discard = true
					continue
				}
				s.Sent.Add(uint64(written))
			}
		}
	}()
	var result error
	select {
	case result = <-results:
	case <-ctx.Done():
		result = ctx.Err()
	}
	_ = transport.Close()
	s.Close()
	return result
}

// Darwin UNIX datagram sockets can report ENOBUFS during a short burst. Preserve
// packet boundaries and apply bounded back pressure while OpenConnect drains it.
func writePacket(ctx context.Context, target io.Writer, packet []byte) (int, error) {
	deadline := time.Now().Add(time.Second)
	for {
		n, err := target.Write(packet)
		if err == nil || (!errors.Is(err, syscall.ENOBUFS) && !errors.Is(err, syscall.EAGAIN)) || time.Now().After(deadline) {
			return n, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
