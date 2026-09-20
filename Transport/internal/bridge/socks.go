package bridge

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
type SOCKSServer struct {
	Dialer Dialer
	Token  string
}

func (s *SOCKSServer) Serve(ctx context.Context, listener net.Listener) error {
	go func() { <-ctx.Done(); _ = listener.Close() }()
	limit := make(chan struct{}, 256)
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		select {
		case limit <- struct{}{}:
			go func() { defer func() { <-limit }(); s.handle(ctx, conn) }()
		default:
			_ = conn.Close()
		}
	}
}

func (s *SOCKSServer) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil || greeting[0] != 5 || greeting[1] == 0 {
		return
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	supported := false
	for _, v := range methods {
		if v == 2 {
			supported = true
		}
	}
	if !supported {
		_, _ = conn.Write([]byte{5, 255})
		return
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return
	}
	auth := make([]byte, 2)
	if _, err := io.ReadFull(conn, auth); err != nil || auth[0] != 1 || auth[1] == 0 {
		return
	}
	username := make([]byte, int(auth[1]))
	if _, err := io.ReadFull(conn, username); err != nil {
		return
	}
	size := []byte{0}
	if _, err := io.ReadFull(conn, size); err != nil {
		return
	}
	password := make([]byte, int(size[0]))
	if _, err := io.ReadFull(conn, password); err != nil {
		return
	}
	if string(username) != "goconnect" || subtle.ConstantTimeCompare(password, []byte(s.Token)) != 1 {
		_, _ = conn.Write([]byte{1, 1})
		return
	}
	_, _ = conn.Write([]byte{1, 0})
	header := make([]byte, 3)
	if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 || header[2] != 0 {
		return
	}
	address, err := readAddress(conn)
	if err != nil {
		reply(conn, 8, nil)
		return
	}
	switch header[1] {
	case 1:
		dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		remote, err := s.Dialer.DialContext(dialCtx, "tcp", address)
		cancel()
		if err != nil {
			reply(conn, 4, nil)
			return
		}
		defer remote.Close()
		if err := reply(conn, 0, nil); err != nil {
			return
		}
		_ = conn.SetDeadline(time.Time{})
		closed := make(chan struct{}, 2)
		go func() {
			_, _ = io.Copy(remote, conn)
			if c, ok := remote.(interface{ CloseWrite() error }); ok {
				_ = c.CloseWrite()
			}
			closed <- struct{}{}
		}()
		go func() {
			_, _ = io.Copy(conn, remote)
			if c, ok := conn.(interface{ CloseWrite() error }); ok {
				_ = c.CloseWrite()
			}
			closed <- struct{}{}
		}()
		// Preserve the response after the client half-closes its request body.
		for i := 0; i < 2; i++ {
			select {
			case <-ctx.Done():
				return
			case <-closed:
			}
		}
	case 3:
		s.udp(ctx, conn)
	default:
		_ = reply(conn, 7, nil)
	}
}

func readAddress(r io.Reader) (string, error) {
	kind := []byte{0}
	if _, err := io.ReadFull(r, kind); err != nil {
		return "", err
	}
	var host string
	switch kind[0] {
	case 1:
		data := make([]byte, 4)
		if _, err := io.ReadFull(r, data); err != nil {
			return "", err
		}
		host = net.IP(data).String()
	case 4:
		data := make([]byte, 16)
		if _, err := io.ReadFull(r, data); err != nil {
			return "", err
		}
		host = net.IP(data).String()
	case 3:
		size := []byte{0}
		if _, err := io.ReadFull(r, size); err != nil || size[0] == 0 {
			return "", errors.New("bad hostname")
		}
		data := make([]byte, int(size[0]))
		if _, err := io.ReadFull(r, data); err != nil {
			return "", err
		}
		host = string(data)
	default:
		return "", errors.New("bad address type")
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(r, port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))), nil
}

func encodedAddress(address string) ([]byte, error) {
	host, portString, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		return nil, err
	}
	var data []byte
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			data = append([]byte{1}, v4...)
		} else {
			data = append([]byte{4}, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return nil, errors.New("bad hostname")
		}
		data = append([]byte{3, byte(len(host))}, host...)
	}
	return append(data, byte(port>>8), byte(port)), nil
}

func reply(conn net.Conn, code byte, address net.Addr) error {
	destination := "0.0.0.0:0"
	if address != nil {
		destination = address.String()
	}
	encoded, err := encodedAddress(destination)
	if err != nil {
		return err
	}
	_, err = conn.Write(append([]byte{5, code, 0}, encoded...))
	return err
}

func (s *SOCKSServer) udp(parent context.Context, control net.Conn) {
	local, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = reply(control, 1, nil)
		return
	}
	defer local.Close()
	if err = reply(control, 0, local.LocalAddr()); err != nil {
		return
	}
	_ = control.SetDeadline(time.Time{})
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, control); cancel() }()
	go func() { <-ctx.Done(); _ = local.Close() }()
	var mu sync.Mutex
	remotes := make(map[string]net.Conn)
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, remote := range remotes {
			_ = remote.Close()
		}
	}()
	var source *net.UDPAddr
	buffer := make([]byte, 65535)
	for {
		_ = local.SetReadDeadline(time.Now().Add(2 * time.Minute))
		n, peer, err := local.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if !peer.IP.IsLoopback() || n < 7 || buffer[0] != 0 || buffer[1] != 0 || buffer[2] != 0 {
			continue
		}
		if source == nil {
			source = peer
		} else if source.String() != peer.String() {
			continue
		}
		reader := &sliceReader{data: buffer[3:n]}
		address, err := readAddress(reader)
		if err != nil {
			continue
		}
		mu.Lock()
		remote := remotes[address]
		count := len(remotes)
		mu.Unlock()
		if remote == nil {
			if count >= 128 {
				continue
			}
			dialCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			remote, err = s.Dialer.DialContext(dialCtx, "udp", address)
			stop()
			if err != nil {
				continue
			}
			mu.Lock()
			remotes[address] = remote
			mu.Unlock()
			go func(target string, remote net.Conn, client *net.UDPAddr) {
				defer remote.Close()
				defer func() {
					mu.Lock()
					if remotes[target] == remote {
						delete(remotes, target)
					}
					mu.Unlock()
				}()
				body := make([]byte, 65000)
				encoded, err := encodedAddress(remote.RemoteAddr().String())
				if err != nil {
					return
				}
				for {
					_ = remote.SetReadDeadline(time.Now().Add(90 * time.Second))
					n, err := remote.Read(body)
					if err != nil {
						return
					}
					packet := append([]byte{0, 0, 0}, encoded...)
					packet = append(packet, body[:n]...)
					if _, err = local.WriteToUDP(packet, client); err != nil {
						return
					}
				}
			}(address, remote, source)
		}
		_, _ = remote.Write(reader.data)
	}
}

type sliceReader struct{ data []byte }

func (s *sliceReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}
