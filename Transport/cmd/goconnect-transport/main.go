package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"goconnect.local/transport/internal/bridge"
)

var outputMu sync.Mutex

func emit(value any) {
	outputMu.Lock()
	defer outputMu.Unlock()
	_ = json.NewEncoder(os.Stdout).Encode(value)
}
func fail(message string) { emit(map[string]any{"event": "error", "message": message}) }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "GoConnectTransport: session | pipe | route | version")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var err error
	if os.Args[1] == "service-run" {
		defer startRootDiagnostics("service")()
	}
	if os.Args[1] == "service-worker" {
		defer startRootDiagnostics("worker")()
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("GoConnectTransport 0.11.0")
	case "service-diagnostics":
		err = serviceDiagnostics()
	case "service-status":
		emit(serviceStatus())
	case "service-install":
		if len(os.Args) != 3 {
			err = errors.New("missing local user")
		} else {
			err = installNetworkService(os.Args[2])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	case "service-uninstall":
		err = uninstallNetworkService()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	case "service-run":
		err = runNetworkService(ctx)
	case "service-worker":
		err = serviceWorker(ctx, os.Args[2:])
	case "service-route":
		if len(os.Args) != 3 {
			err = errors.New("missing session")
		} else {
			err = serviceRoute(ctx, os.Args[2])
		}
	case "subscription-import":
		err = subscriptionCommand(ctx, true)
	case "subscription-fetch":
		err = subscriptionCommand(ctx, false)
	case "proxy-front":
		err = proxyFrontSession(ctx)
	case "company-hook":
		err = errors.New("separate company VPN sessions are disabled")
	case "subscription-session":
		err = subscriptionSession(ctx)
	case "session":
		err = session(ctx)
	case "routing-check":
		err = errors.New("legacy PF routing is disabled")
	case "routing-probe":
		err = routingProbe(ctx, os.Args[2:])
	case "pipe":
		err = packetPipe(ctx)
	case "route":
		err = errors.New("legacy PF routing is disabled")
	case "config-check":
		if len(os.Args) != 3 {
			err = errors.New("missing session")
		} else {
			err = checkConfig(os.Args[2])
		}
	default:
		err = errors.New("unknown command")
	}
	if err != nil {
		trace("process.failed", 1)
		if diagnostics != nil {
			diagnostics.close()
		}
		os.Exit(1)
	}
}

type request struct {
	Server          string `json:"server"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Group           string `json:"group"`
	Token           string `json:"token"`
	OpenConnectPath string `json:"openconnectPath"`
	CAFile          string `json:"caFile,omitempty"`
}

func (r request) validate() error {
	u, err := url.Parse(r.Server)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid server")
	}
	if r.Username == "" || r.Password == "" || len(r.Token) < 32 || len(r.Token) > 200 {
		return errors.New("missing credential")
	}
	for _, value := range []string{r.Server, r.Username, r.Group, r.Password} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("invalid input")
		}
	}
	if !filepath.IsAbs(r.OpenConnectPath) {
		return errors.New("invalid executable")
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func session(parent context.Context) error {
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 65536))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		fail("无法读取连接配置。")
		return err
	}
	var r request
	if err = json.Unmarshal(line, &r); err != nil {
		fail("连接配置无效。")
		return err
	}
	if err = r.validate(); err != nil {
		fail("请检查服务器地址和账号信息。")
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Keeping stdin open is the lifetime lease from the GUI. EOF means stop.
	go func() { _, _ = io.Copy(io.Discard, reader); cancel() }()
	args := []string{"--protocol=anyconnect", "--non-inter", "--passwd-on-stdin", "--reconnect-timeout=0", "--script-tun", "--script", "exec " + shellQuote(self) + " pipe", "--user", r.Username}
	if r.Group != "" {
		args = append(args, "--authgroup", r.Group)
	}
	ca := r.CAFile
	if ca == "" {
		candidate := filepath.Join(filepath.Dir(r.OpenConnectPath), "..", "share", "cacert.pem")
		if _, e := os.Stat(candidate); e == nil {
			ca = candidate
		}
	}
	if ca != "" {
		args = append(args, "--cafile", ca)
	}
	args = append(args, "--", r.Server)
	cmd := exec.Command(r.OpenConnectPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C", "GOCONNECT_TOKEN=" + r.Token, "GOCONNECT_OWNER_PID=" + strconv.Itoa(os.Getpid())}
	cmd.Stdin = strings.NewReader(r.Password + "\n")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		fail("无法启动 OpenConnect。")
		return err
	}
	r.Password = ""
	line = nil
	ready := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var event map[string]any
			if json.Unmarshal(scanner.Bytes(), &event) == nil && event["event"] != nil {
				if event["event"] == "ready" {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
				emit(event)
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// Only classify known error categories. Raw OpenConnect output is never retained.
			lower := strings.ToLower(scanner.Text())
			if strings.Contains(lower, "certificate") && (strings.Contains(lower, "fail") || strings.Contains(lower, "invalid")) {
				fail("服务器证书校验未通过。请确认服务器地址或配置受信任的证书。")
			}
			if strings.Contains(lower, "authentication failed") || strings.Contains(lower, "login failed") {
				fail("身份认证失败，请检查账号、密码和连接组。")
			}
		}
	}()
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	timer := time.NewTimer(75 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ready:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case err = <-ended:
			ended <- err
			if cleanupErr := stopChild(cmd, ended); cleanupErr != nil {
				fail("VPN 子进程尚未退出，请导出诊断。")
				return cleanupErr
			}
			if ctx.Err() == nil && err != nil {
				fail("VPN 隧道未能建立或已中断。请核对认证方式和网络。")
			}
			emit(map[string]any{"event": "stopped"})
			return err
		case <-timer.C:
			fail("连接超时，请检查服务器和网络。")
			cancel()
		case <-ctx.Done():
			if err := stopChild(cmd, ended); err != nil {
				fail("VPN 进程无法退出，请导出诊断后处理；未自动重启。")
				return err
			}

			emit(map[string]any{"event": "stopped"})
			return nil
		}
	}
}

func packetPipe(parent context.Context) error {
	fd, err := strconv.Atoi(os.Getenv("VPNFD"))
	if err != nil || fd < 3 {
		return errors.New("invalid VPNFD")
	}
	file := os.NewFile(uintptr(fd), "vpn-packet-channel")
	if file == nil {
		return errors.New("missing VPNFD")
	}
	defer file.Close()
	channel, err := net.FileConn(file)
	if err != nil {
		return err
	}
	defer channel.Close()
	_ = file.Close()
	var addresses, dns []netip.Addr
	for _, key := range []string{"INTERNAL_IP4_ADDRESS", "INTERNAL_IP6_ADDRESS"} {
		value := strings.Split(os.Getenv(key), "/")[0]
		if address, err := netip.ParseAddr(value); err == nil {
			addresses = append(addresses, address)
		}
	}
	for _, key := range []string{"INTERNAL_IP4_DNS", "INTERNAL_IP6_DNS"} {
		for _, value := range strings.Fields(os.Getenv(key)) {
			if address, err := netip.ParseAddr(value); err == nil {
				dns = append(dns, address)
			}
		}
	}
	mtu, _ := strconv.Atoi(os.Getenv("INTERNAL_IP4_MTU"))
	if mtu == 0 {
		mtu = 1400
	}
	stack, err := bridge.New(addresses, dns, mtu)
	if err != nil {
		return err
	}
	defer stack.Close()
	token := os.Getenv("GOCONNECT_TOKEN")
	if len(token) < 32 {
		return errors.New("missing token")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() { _ = (&bridge.SOCKSServer{Dialer: stack, Token: token}).Serve(ctx, listener); cancel() }()
	gateway := os.Getenv("VPNGATEWAY")
	remoteNetworks, remoteExcluded, err := advertisedRemoteNetworks(os.Getenv)
	if err != nil {
		return err
	}
	emit(map[string]any{"event": "ready", "port": listener.Addr().(*net.TCPAddr).Port, "addresses": addresses, "dns": dns, "gateway": gateway, "remoteNetworks": remoteNetworks, "remoteExcluded": remoteExcluded})
	owner, _ := strconv.Atoi(os.Getenv("GOCONNECT_OWNER_PID"))
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if owner <= 1 || syscall.Kill(owner, 0) != nil {
					cancel()
					return
				}
				emit(map[string]any{"event": "stats", "received": stack.Received.Load(), "sent": stack.Sent.Load()})
			}
		}
	}()
	return stack.Pump(ctx, channel)
}
