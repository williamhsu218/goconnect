package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"goconnect.local/transport/internal/bridge"
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

func routingCheck(parent context.Context, tun bool, global bool) (returnErr error) {
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	defer cancel()
	// The parent GUI can revoke this entire check by closing stdin.
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	defer func() {
		if returnErr != nil {
			returnErr = routingCheckError(ctx, returnErr)
			fail(returnErr.Error())
		}
		emit(map[string]any{"event": "check-finished", "passed": returnErr == nil, "tun": tun})
	}()
	if global && !tun {
		return errors.New("全局模式验证需要 --tun")
	}
	label := "白名单模式"
	if global {
		label = "全局模式"
	}
	emit(map[string]any{"event": "check-progress", "message": "开始验证" + label + "；只对受限测试地址生效。"})
	runtime, err := runtimePath()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "goconnect-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	selected, selectedBin, err := probeApp(dir, "Selected", self)
	if err != nil {
		return err
	}
	_, directBin, err := probeApp(dir, "Direct", self)
	if err != nil {
		return err
	}
	helperBin := filepath.Join(selected, "Contents/Helpers/ProbeHelper")
	if err = os.MkdirAll(filepath.Dir(helperBin), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	if err = os.WriteFile(helperBin, data, 0o700); err != nil {
		return err
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	token := hex.EncodeToString(random)
	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer proxyListener.Close()
	proxyCtx, stopProxy := context.WithCancel(ctx)
	defer stopProxy()
	go func() { _ = (&bridge.SOCKSServer{Dialer: probeDialer{}, Token: token}).Serve(proxyCtx, proxyListener) }()
	request := routeRequest{LiveRouting: true, OwnerPID: os.Getpid(), ControlDirectory: dir, Token: token, SOCKSPort: uint16(proxyListener.Addr().(*net.TCPAddr).Port), AppPaths: []string{selected}, Gateway: "127.0.0.1", ProbeOnly: true}
	if global {
		request.RoutingMode = modeGlobal
	}
	controllerPort, _ := freePort()
	inboundPort, _ := freePort()
	device, _ := chooseDevice()
	config := makeConfig(request, runtime, device, controllerPort)
	baseline, err := routeInterface(probeTarget)
	if err != nil {
		return err
	}
	var command *exec.Cmd
	var leaseStop chan struct{}
	if tun {
		bytes, _ := json.Marshal(request)
		if err = os.WriteFile(filepath.Join(dir, "session.json"), bytes, 0o600); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "lease"), []byte(token), 0o600); err != nil {
			return err
		}
		leaseStop = make(chan struct{})
		defer close(leaseStop)
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-leaseStop:
					return
				case <-tick.C:
					_ = os.Chtimes(filepath.Join(dir, "lease"), time.Now(), time.Now())
				}
			}
		}()
		command = exec.Command(self, "service-route", filepath.Join(dir, "session.json"))
		command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=en_US.UTF-8"}
		emit(map[string]any{"event": "check-progress", "message": "通过已授权的本机服务验证当前模式；公网测试范围仅 1.1.1.1。"})
	} else {
		config["tun"].(map[string]any)["enable"] = false
		config["socks-port"] = inboundPort
		if err = writeLiveRules(dir, request.AppPaths); err != nil {
			return err
		}
		bytes, _ := json.Marshal(config)
		if err = os.WriteFile(filepath.Join(dir, "config.json"), bytes, 0o600); err != nil {
			return err
		}
		command = exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", filepath.Join(dir, "config.json"))
		emit(map[string]any{"event": "check-progress", "message": "启动真实 Mihomo，验证独立 App 进程的 TCP / UDP 规则。"})
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		return errors.New("无法启动分流验证")
	}
	finished := make(chan struct{})
	go func() { _ = command.Wait(); close(finished) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if tun {
				_ = os.Remove(filepath.Join(dir, "lease"))
			} else {
				_ = command.Process.Signal(os.Interrupt)
			}
			select {
			case <-finished:
			case <-time.After(8 * time.Second):
				_ = command.Process.Kill()
			}
		})
	}
	defer stop()
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	started := false
	for !started {
		select {
		case <-ctx.Done():
			return errors.New("分流验证已取消或等待授权超时")
		case <-finished:
			state, _ := readRouteSnapshot(filepath.Join(dir, "status"))
			if state.Message != "" {
				return errors.New(state.Message)
			}
			return errors.New("分流验证未获授权或核心提前退出")
		case <-time.After(200 * time.Millisecond):
			if tun {
				state, _ := readRouteSnapshot(filepath.Join(dir, "status"))
				started = state.State == "ready"
				if state.Device != "" {
					device = state.Device
				}
				if state.State == "failed" {
					if state.Message != "" {
						return errors.New(state.Message)
					}
					return errors.New("管理员 TUN 未能启动")
				}
			} else {
				response, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/version", controllerPort))
				if e == nil {
					response.Body.Close()
					started = liveRulesReady(client, controllerPort, token, len(liveRulePayload(request.AppPaths)))
				}
			}
		}
	}
	if tun {
		after, e := routeInterface(probeTarget)
		if e != nil || after != baseline || after == device {
			return errors.New("普通进程的默认路由被改变")
		}
		emit(map[string]any{"event": "check-progress", "message": "默认路由仍在原网络接口，未创建全局 TUN 路由。"})
	}
	via := ""
	if !tun {
		via = net.JoinHostPort("127.0.0.1", strconv.Itoa(int(inboundPort)))
	}
	runAt := func(binary, network string, localPort uint16, target string) (probeReply, error) {
		args := []string{"routing-probe", network, via}
		if localPort != 0 || target != "" {
			port := ""
			if localPort != 0 {
				port = strconv.Itoa(int(localPort))
			}
			args = append(args, port, target)
		}
		command := exec.CommandContext(ctx, binary, args...)
		output, e := command.Output()
		if e != nil {
			return probeReply{}, e
		}
		var result probeReply
		e = json.Unmarshal(output, &result)
		return result, e
	}
	run := func(binary, network string) (probeReply, error) { return runAt(binary, network, 0, "") }
	vpnBinary, directBinary := selectedBin, directBin
	selectedLabel, otherLabel := "白名单 App", "非白名单 App"
	if global {
		vpnBinary, directBinary = directBin, selectedBin
		selectedLabel, otherLabel = "直连排除 App", "默认 VPN App"
	}
	var selectedUDPPort uint16
	for _, entry := range []struct {
		label, binary string
		tunnel        bool
	}{{selectedLabel, selectedBin, !global}, {"包内助手", helperBin, !global}, {otherLabel, directBin, global}} {
		for _, network := range []string{"tcp", "udp"} {
			result, e := run(entry.binary, network)
			if !entry.tunnel && e == nil && result.Outcome != "ok" {
				return fmt.Errorf("原网络 %s 探针未收到有效响应，暂无法完成直连验证", strings.ToUpper(network))
			}
			if e != nil || result.Outcome != "ok" || result.Tunnel != entry.tunnel {
				return fmt.Errorf("%s 的 %s 分流未通过（%s）", entry.label, strings.ToUpper(network), result.Outcome)
			}
			if tun && entry.binary == vpnBinary && network == "udp" {
				selectedUDPPort = result.LocalPort
			}
			emit(map[string]any{"event": "check-progress", "message": entry.label + " · " + strings.ToUpper(network) + " → " + map[bool]string{true: "VPN 出口", false: "原网络直连"}[entry.tunnel] + "，已验证。"})
		}
	}
	if tun {
		if selectedUDPPort < 1024 {
			return errors.New("未取得白名单 UDP 的测试端口")
		}
		reused, e := runAt(directBinary, "udp", selectedUDPPort, "")
		if e != nil || reused.Outcome != "ok" || reused.Tunnel || reused.LocalPort != selectedUDPPort {
			return errors.New("直连进程复用 VPN UDP 端口时未能保持原网络直连")
		}
		emit(map[string]any{"event": "check-progress", "message": "直连进程复用 VPN UDP 端口后仍走原网络，未被旧连接接管。"})
		snapshot, e := readRouteSnapshot(filepath.Join(dir, "status"))
		if e != nil || !snapshot.VPNObserved || len(snapshot.Apps) != 1 || snapshot.Apps[0].Path != selected || snapshot.Blocked != 0 || snapshot.CaptureMode != "process-rules" || !snapshot.OriginalRoute {
			return errors.New("实际流量通过，但核心应用命中状态未能读回")
		}
		emit(map[string]any{"event": "check-progress", "message": "普通启动的测试应用已按进程规则分别使用 VPN 与直连出口。"})
	}
	if tun {
		snapshot, _ := readRouteSnapshot(filepath.Join(dir, "status"))
		if snapshot.TailscaleActive {
			baselineCmd := exec.CommandContext(ctx, self, "routing-probe", "udp", "", "", "tailscale-dns")
			output, baselineErr := baselineCmd.Output()
			var baselineReply probeReply
			if baselineErr == nil {
				baselineErr = json.Unmarshal(output, &baselineReply)
			}
			if baselineErr == nil && baselineReply.Outcome == "ok" {
				reply, e := runAt(vpnBinary, "udp", 0, "tailscale-dns")
				if e != nil || reply.Outcome != "ok" || reply.Tunnel {
					return errors.New("Tailscale DNS 绕过验证失败")
				}
				emit(map[string]any{"event": "check-progress", "message": "应走 VPN 的应用仍通过原 Tailscale 路径访问其 DNS，已验证。"})
			} else {
				emit(map[string]any{"event": "check-progress", "message": "已识别 Tailscale；其 DNS 基线无响应，本项未完成网络验收。"})
			}
		}
	}
	stopProxy()
	_ = proxyListener.Close()
	for _, network := range []string{"tcp", "udp"} {
		blocked, e := run(vpnBinary, network)
		if e != nil || blocked.Outcome != "error" {
			return fmt.Errorf("VPN 出口中断时应走 VPN 的应用 %s 出现意外回退", network)
		}
		direct, e := run(directBinary, network)
		if direct.Tunnel {
			return fmt.Errorf("VPN 出口中断后，直连应用 %s 意外进入了测试 VPN", network)
		}
		if e != nil || direct.Outcome != "ok" {
			return fmt.Errorf("VPN 出口中断场景中，原网络 %s 响应未能确认", strings.ToUpper(network))
		}
	}
	emit(map[string]any{"event": "check-progress", "message": "VPN 出口中断：VPN 应用连接失败，直连应用继续使用原网络。"})
	stop()
	if tun {
		after, e := run(vpnBinary, "tcp")
		if e != nil || after.Outcome != "ok" || after.Tunnel {
			return errors.New("停止 TUN 后普通网络未恢复")
		}
		emit(map[string]any{"event": "check-progress", "message": "测试 TUN 已停止，原网络连接已恢复。"})
	}
	return nil
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
