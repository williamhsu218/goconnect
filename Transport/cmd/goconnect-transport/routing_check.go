package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/net/proxy"
)

const probeTarget = "1.1.1.1"

var probeQuery = []byte{0x47, 0x43, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 9, 'g', 'o', 'c', 'o', 'n', 'n', 'e', 'c', 't', 7, 'i', 'n', 'v', 'a', 'l', 'i', 'd', 0, 0, 1, 0, 1}
var probeDNSAnswer = []byte{192, 0, 2, 123}

type probeReply struct {
	Outcome   string `json:"outcome"`
	Tunnel    bool   `json:"tunnel"`
	Network   string `json:"network"`
	LocalPort uint16 `json:"localPort,omitempty"`
}

// Real executable copies in distinct .app paths issue these requests; no mocked PID.
func routingProbe(ctx context.Context, args []string) error {
	if len(args) < 1 || (args[0] != "tcp" && args[0] != "udp") {
		return errors.New("invalid probe")
	}
	network := args[0]
	via := ""
	if len(args) > 1 {
		via = args[1]
	}
	result := probeReply{Outcome: "error", Network: network}
	defer func() { emit(result) }()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	port := "80"
	if network == "udp" {
		port = "53"
	}
	target := probeTarget
	if len(args) > 3 && args[3] != "" {
		if args[3] != "tailscale-dns" || network != "udp" || via != "" {
			return errors.New("invalid probe target")
		}
		target = "100.100.100.100"
	}
	destination := net.JoinHostPort(target, port)
	var conn net.Conn
	var control net.Conn
	var err error
	if via == "" {
		dialer := &net.Dialer{}
		if len(args) > 2 && args[2] != "" {
			port, e := strconv.ParseUint(args[2], 10, 16)
			if e != nil || network != "udp" || port < 1024 {
				return errors.New("invalid probe source port")
			}
			dialer.LocalAddr = &net.UDPAddr{IP: net.IPv4zero, Port: int(port)}
		}
		conn, err = dialer.DialContext(ctx, network, destination)
	} else if network == "tcp" {
		dialer, e := proxy.SOCKS5("tcp", via, nil, proxy.Direct)
		if e != nil {
			return e
		}
		conn, err = dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", destination)
	} else {
		control, err = net.DialTimeout("tcp", via, 2*time.Second)
		if err == nil {
			defer control.Close()
			_ = control.SetDeadline(time.Now().Add(5 * time.Second))
			_, err = control.Write([]byte{5, 1, 0})
			response := make([]byte, 2)
			if err == nil {
				_, err = io.ReadFull(control, response)
			}
			if err == nil && bytes.Equal(response, []byte{5, 0}) {
				_, err = control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
				bind := make([]byte, 10)
				if err == nil {
					_, err = io.ReadFull(control, bind)
				}
				if err == nil && bind[1] == 0 && bind[3] == 1 {
					conn, err = net.Dial("udp", net.JoinHostPort(net.IP(bind[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(bind[8:])))))
				} else {
					err = errors.New("UDP association failed")
				}
			} else {
				err = errors.New("SOCKS handshake failed")
			}
		}
	}
	if err != nil || conn == nil {
		return nil
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		result.LocalPort = uint16(addr.Port)
	}
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if network == "tcp" {
		_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: one.one.one.one\r\nConnection: keep-alive\r\n\r\n")
		if err != nil {
			return nil
		}
		response, e := http.ReadResponse(bufio.NewReader(conn), nil)
		if e != nil {
			return nil
		}
		result.Outcome = "ok"
		result.Tunnel = response.Header.Get("X-GoConnect-Probe") == "tunnel"
		_ = response.Body.Close()
	} else {
		packet := probeQuery
		if via != "" {
			packet = append([]byte{0, 0, 0, 1, 1, 1, 1, 1, 0, 53}, probeQuery...)
		}
		buffer := make([]byte, 2048)
		var n int
		var e error
		// UDP does not guarantee delivery. Retry the same socket/route within
		// a fixed bound; never replace it with another destination or DIRECT.
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				return nil
			}
			if _, err = conn.Write(packet); err != nil {
				return nil
			}
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			n, e = conn.Read(buffer)
			if e == nil {
				break
			}
			if timeout, ok := e.(net.Error); !ok || !timeout.Timeout() {
				return nil
			}
		}
		if e != nil {
			return nil
		}
		answer := buffer[:n]
		if via != "" {
			if n < 10 {
				return nil
			}
			answer = answer[10:]
		}
		if len(answer) < 12 || !bytes.Equal(answer[:2], probeQuery[:2]) {
			return nil
		}
		result.Outcome = "ok"
		result.Tunnel = len(answer) > 4 && bytes.Equal(answer[len(answer)-4:], probeDNSAnswer)
	}
	// Keep connections observable long enough for the core's /connections readback.
	select {
	case <-ctx.Done():
	case <-time.After(1200 * time.Millisecond):
	}
	return nil
}

type probeDialer struct{}
type probeConn struct {
	net.Conn
	target net.Addr
}

func (c probeConn) RemoteAddr() net.Addr { return c.target }
func (probeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if address != probeTarget+":80" && address != probeTarget+":53" {
		return nil, errors.New("probe target only")
	}
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		_ = b.SetDeadline(time.Now().Add(8 * time.Second))
		if network == "tcp" {
			reader := bufio.NewReader(b)
			if _, err := http.ReadRequest(reader); err != nil {
				return
			}
			_, _ = io.WriteString(b, "HTTP/1.1 200 OK\r\nX-GoConnect-Probe: tunnel\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n")
			_, _ = io.Copy(io.Discard, b)
		} else {
			buffer := make([]byte, 2048)
			n, err := b.Read(buffer)
			if err != nil || n < 12 {
				return
			}
			answer := append([]byte{}, buffer[:n]...)
			answer[2] = 0x81
			answer[3] = 0x80
			answer[6] = 0
			answer[7] = 1
			answer = append(answer, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 1, 0, 4)
			answer = append(answer, probeDNSAnswer...)
			_, _ = b.Write(answer)
			_, _ = io.Copy(io.Discard, b)
		}
	}()
	return probeConn{a, &net.UDPAddr{IP: net.ParseIP(probeTarget), Port: 53}}, nil
}

func probeApp(dir, name, self string) (string, string, error) {
	app := filepath.Join(dir, name+".app")
	binary := filepath.Join(app, "Contents/MacOS/Probe")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return "", "", err
	}
	if err = os.WriteFile(binary, data, 0o700); err != nil {
		return "", "", err
	}
	return app, binary, nil
}

func routingCheckError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return errors.New("分流自检已取消。")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("分流自检超时，请检查管理员授权和测试地址连通性。")
	}
	return err
}
