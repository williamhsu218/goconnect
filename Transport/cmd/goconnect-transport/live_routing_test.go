package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
)

func TestLiveRulesKeepPriorityAndModeFallback(t *testing.T) {
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r := routeRequest{LiveRouting: true, RoutingMode: mode, OwnerPID: 123, SOCKSPort: 1234, Token: strings.Repeat("a", 40), Gateway: "192.0.2.1", DirectDomains: []domainRule{{Domain: "example.com"}}}
		if err := r.validate(); err != nil {
			t.Fatal(err)
		}
		config := makeLiveConfig(r, "utun100", 12345)
		rules := config["rules"].([]string)
		target, last := "GoConnect", "MATCH,REJECT"
		if mode == modeGlobal {
			target, last = "DIRECT", "MATCH,GoConnect"
		}
		if rules[0] != "DOMAIN,example.com,DIRECT" || rules[1] != "RULE-SET,goconnect-apps,"+target || rules[len(rules)-1] != last {
			t.Fatal(rules)
		}
		if config["ipv6"] != true || len(config["tun"].(map[string]any)["inet6-address"].([]string)) != 1 {
			t.Fatal("dual-stack ingress missing")
		}
		pf := liveRoutingRules(501, "utun100", r, tailscaleBypass{})
		if !strings.Contains(pf, "route-to (utun100 "+liveIPv6Address+") inet6") {
			t.Fatal("IPv6 not captured", pf)
		}
		if strings.Contains(pf, "group ") || !strings.Contains(pf, "block return out quick on ! lo0 inet6") {
			t.Fatal(pf)
		}
		for _, line := range strings.Split(strings.TrimSpace(pf), "\n") {
			if !strings.Contains(line, "user 501") {
				t.Fatal("other users affected", line)
			}
		}
	}
}

func TestLiveChangedAppsAreScoped(t *testing.T) {
	got := changedAppPaths([]string{"/A.app", "/B.app"}, []string{"/B.app", "/C.app"})
	if strings.Join(got, ",") != "/A.app,/C.app" {
		t.Fatal(got)
	}
	if len(changedAppPaths([]string{"/A.app"}, []string{"/A.app"})) != 0 {
		t.Fatal("no-op changes traffic")
	}
}

type liveLoopbackDialer struct{ address, udp string }

func (d liveLoopbackDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network == "udp" {
		return (&net.Dialer{}).DialContext(ctx, network, d.udp)
	}
	return (&net.Dialer{}).DialContext(ctx, network, d.address)
}

func liveUDPRelay(socks string) (net.Conn, net.Conn, error) {
	control, err := net.DialTimeout("tcp", socks, time.Second)
	if err != nil {
		return nil, nil, err
	}
	control.SetDeadline(time.Now().Add(2 * time.Second))
	fail := func(err error) (net.Conn, net.Conn, error) { control.Close(); return nil, nil, err }
	control.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err = io.ReadFull(control, reply); err != nil {
		return fail(err)
	}
	control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	reply = make([]byte, 10)
	if _, err = io.ReadFull(control, reply); err != nil {
		return fail(err)
	}
	if reply[1] != 0 || reply[3] != 1 {
		return fail(fmt.Errorf("unexpected UDP relay"))
	}
	relay, err := net.Dial("udp", net.JoinHostPort(net.IP(reply[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(reply[8:])))))
	if err != nil {
		return fail(err)
	}
	control.SetDeadline(time.Time{})
	return control, relay, nil
}
func liveUDPMarker(t *testing.T, marker string) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			_, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			conn.WriteTo([]byte(marker), addr)
		}
	}()
	return conn
}

// A single long-running process issues every request in the real core test.
func TestLiveRoutingProbeProcess(t *testing.T) {
	if os.Getenv("GOCONNECT_LIVE_PROBE") != "1" {
		return
	}
	dialer, err := proxy.SOCKS5("tcp", os.Getenv("GOCONNECT_LIVE_SOCKS"), nil, proxy.Direct)
	if err != nil {
		os.Exit(2)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.Dial(network, address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 4 * time.Second, Transport: transport}
	control, relay, err := liveUDPRelay(os.Getenv("GOCONNECT_LIVE_SOCKS"))
	// Core starts after these processes. Open the UDP association lazily below.
	if err == nil {
		defer control.Close()
		defer relay.Close()
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var target string
		if decoder.Decode(&target) != nil {
			os.Exit(0)
		}
		reply := map[string]any{"pid": os.Getpid()}
		if strings.HasPrefix(target, "udp://") {
			if relay == nil {
				control, relay, err = liveUDPRelay(os.Getenv("GOCONNECT_LIVE_SOCKS"))
				if err == nil {
					defer control.Close()
					defer relay.Close()
				}
			}
			if relay == nil {
				reply["error"] = err.Error()
			} else {
				address, e := net.ResolveUDPAddr("udp4", strings.TrimPrefix(target, "udp://"))
				if e != nil {
					reply["error"] = e.Error()
				} else {
					packet := []byte{0, 0, 0, 1}
					packet = append(packet, address.IP.To4()...)
					packet = binary.BigEndian.AppendUint16(packet, uint16(address.Port))
					packet = append(packet, []byte("probe")...)
					relay.SetDeadline(time.Now().Add(3 * time.Second))
					_, e = relay.Write(packet)
					buf := make([]byte, 2048)
					var n int
					if e == nil {
						n, e = relay.Read(buf)
					}
					if e != nil {
						reply["error"] = e.Error()
					} else if n < 10 {
						reply["error"] = "short UDP reply"
					} else {
						reply["body"] = string(buf[10:n])
					}
				}
			}
			encoder.Encode(reply)
			continue
		}
		response, e := client.Get(target)
		if e != nil {
			reply["error"] = e.Error()
		} else {
			data, e := io.ReadAll(response.Body)
			response.Body.Close()
			reply["body"] = string(data)
			if e != nil {
				reply["error"] = e.Error()
			}
		}
		if encoder.Encode(reply) != nil {
			os.Exit(0)
		}
	}
}

type persistentLiveProbe struct {
	debug   func()
	command *exec.Cmd
	input   io.WriteCloser
	encoder *json.Encoder
	decoder *json.Decoder
}

type liveProbeResult struct {
	PID   int    `json:"pid"`
	Body  string `json:"body"`
	Error string `json:"error"`
}

func startPersistentLiveProbe(t *testing.T, dir, name, socks string) *persistentLiveProbe {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, binary, err := probeApp(dir, name, self)
	if err != nil {
		t.Fatal(err)
	}
	return startPersistentProbeAtPath(t, binary, socks)
}

func startPersistentExecutableProbe(t *testing.T, dir, name, socks string) (*persistentLiveProbe, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, name)
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(binary, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return startPersistentProbeAtPath(t, binary, socks), binary
}

func startPersistentProbeAtPath(t *testing.T, binary, socks string) *persistentLiveProbe {
	t.Helper()
	command := exec.Command(binary, "-test.run=^TestLiveRoutingProbeProcess$")
	command.Env = append(os.Environ(), "GOCONNECT_LIVE_PROBE=1", "GOCONNECT_LIVE_SOCKS="+socks)
	input, _ := command.StdinPipe()
	output, _ := command.StdoutPipe()
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); command.Process.Kill(); command.Wait() })
	return &persistentLiveProbe{command: command, input: input, encoder: json.NewEncoder(input), decoder: json.NewDecoder(output)}
}
func (p *persistentLiveProbe) probe(target string) (liveProbeResult, error) {
	if err := p.encoder.Encode(target); err != nil {
		return liveProbeResult{}, err
	}
	var result liveProbeResult
	if err := p.decoder.Decode(&result); err != nil {
		return result, err
	}
	return result, nil
}

func (p *persistentLiveProbe) request(t *testing.T, target, want string) {
	t.Helper()
	result, err := p.probe(target)
	if err != nil {
		t.Fatal(err)
	}
	if result.PID != p.command.Process.Pid || result.Body != want || result.Error != "" {
		if p.debug != nil {
			p.debug()
		}
		t.Fatalf("PID/routing mismatch: %+v want %s", result, want)
	}
}

func TestLiveCoreUpdatesAppsWithoutRestartingProcesses(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("set bundled runtime for real-core integration")
	}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		t.Run(mode, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "DIRECT") }))
			defer direct.Close()
			vpn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "VPN") }))
			defer vpn.Close()
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer listener.Close()
			token := strings.Repeat("a", 40)
			directUDP := liveUDPMarker(t, "DIRECT")
			vpnUDP := liveUDPMarker(t, "VPN")
			go (&bridge.SOCKSServer{Dialer: liveLoopbackDialer{strings.TrimPrefix(vpn.URL, "http://"), vpnUDP.LocalAddr().String()}, Token: token}).Serve(ctx, listener)
			controlPort, _ := freePort()
			socksPort, _ := freePort()
			for i := 0; i < 20; i++ {
				udp, e := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", socksPort))
				if e == nil {
					udp.Close()
					break
				}
				socksPort, _ = freePort()
			}
			a := startPersistentLiveProbe(t, dir, "Alpha App", fmt.Sprintf("127.0.0.1:%d", socksPort))
			b := startPersistentLiveProbe(t, dir, "Bravo", fmt.Sprintf("127.0.0.1:%d", socksPort))
			service, servicePath := startPersistentExecutableProbe(t, dir, "agy-service", fmt.Sprintf("127.0.0.1:%d", socksPort))
			paths := []string{filepath.Join(dir, "Alpha App.app"), servicePath}
			request := routeRequest{LiveRouting: true, RoutingMode: mode, OwnerPID: os.Getpid(), SOCKSPort: uint16(listener.Addr().(*net.TCPAddr).Port), Token: token, Gateway: "127.0.0.1", AppPaths: paths, DirectDomains: []domainRule{{Domain: "direct.test"}}}
			config := makeLiveConfig(request, "utun999", controlPort)
			config["tun"].(map[string]any)["enable"] = false
			config["socks-port"] = socksPort
			config["log-level"] = "debug"
			config["hosts"] = map[string]any{"direct.test": "127.0.0.1"}
			if err = writeLiveRules(dir, paths); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(config)
			os.WriteFile(filepath.Join(dir, "config.json"), data, 0600)
			core := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", filepath.Join(dir, "config.json"))
			log, err := os.Create(filepath.Join(dir, "core.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			core.Stdout = log
			core.Stderr = log
			if err = core.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { core.Process.Kill(); core.Wait() }()
			api := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
			defer api.CloseIdleConnections()
			ready := false
			for i := 0; i < 60; i++ {
				if _, err = coreSnapshot(api, controlPort, token); err == nil && liveRulesReady(api, controlPort, token, len(liveRulePayload(paths))) {
					ready = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !ready {
				contents, _ := os.ReadFile(filepath.Join(dir, "core.log"))
				t.Fatal("core failed", string(contents))
			}
			first, other := "VPN", "DIRECT"
			if mode == modeGlobal {
				first, other = "DIRECT", "VPN"
			}
			a.debug = func() {
				snap, _ := coreSnapshot(api, controlPort, token)
				t.Logf("core connections: %+v; expected paths: %+v", snap, paths)
				contents, _ := os.ReadFile(filepath.Join(dir, "core.log"))
				t.Log(string(contents))
			}
			b.debug = a.debug
			service.debug = a.debug
			capability, err := a.probe(direct.URL)
			if err != nil {
				t.Fatal(err)
			}
			if capability.PID != a.command.Process.Pid || capability.Body != first || capability.Error != "" {
				contents, _ := os.ReadFile(filepath.Join(dir, "core.log"))
				if strings.Contains(string(contents), "find process error") {
					t.Skip("当前用户态 Mihomo 无法读取进程路径；精确路径需由显式 root TUN 验收覆盖")
				}
				t.Fatalf("PID/routing mismatch: %+v want %s", capability, first)
			}
			b.request(t, direct.URL, other)
			service.request(t, direct.URL, first)
			a.request(t, "udp://"+directUDP.LocalAddr().String(), first)
			b.request(t, "udp://"+directUDP.LocalAddr().String(), other)
			service.request(t, "udp://"+directUDP.LocalAddr().String(), first)
			a.request(t, strings.Replace(direct.URL, "127.0.0.1", "direct.test", 1), "DIRECT")
			handle, err := os.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer handle.Close()
			c := &controlFiles{directory: handle, uid: uint32(os.Getuid()), request: request}
			update := func(seq int, paths []string) {
				data, _ := json.Marshal(map[string]any{"sequence": seq, "action": "updateApps", "appPaths": paths})
				os.WriteFile(filepath.Join(dir, "command.json"), data, 0600)
				if err := c.handleLiveCommand(ctx, api, controlPort, dir); err != nil {
					t.Fatal(err)
				}
				if !c.snapshot.CommandResult.Success {
					t.Fatal(c.snapshot.CommandResult.Message)
				}
			}
			before, _ := coreSnapshot(api, controlPort, token)
			stableIDs := map[string]bool{}
			for _, flow := range before.Connections {
				if strings.Contains(flow.Metadata.ProcessPath, "/Bravo.app/") {
					stableIDs[flow.ID] = true
				}
			}
			if len(stableIDs) < 2 {
				t.Fatal("missing unaffected TCP/UDP flows")
			}
			assertUnchanged := func() {
				after, e := coreSnapshot(api, controlPort, token)
				if e != nil {
					t.Fatal(e)
				}
				present := map[string]bool{}
				for _, flow := range after.Connections {
					present[flow.ID] = true
				}
				for id := range stableIDs {
					if !present[id] {
						t.Fatal("unaffected app connection closed")
					}
				}
			}
			update(1, []string{})
			assertUnchanged()
			a.request(t, direct.URL, other)
			b.request(t, direct.URL, other)
			service.request(t, direct.URL, other)
			a.request(t, "udp://"+directUDP.LocalAddr().String(), other)
			b.request(t, "udp://"+directUDP.LocalAddr().String(), other)
			service.request(t, "udp://"+directUDP.LocalAddr().String(), other)
			a.request(t, strings.Replace(direct.URL, "127.0.0.1", "direct.test", 1), "DIRECT")
			update(2, paths)
			assertUnchanged()
			a.request(t, direct.URL, first)
			b.request(t, direct.URL, other)
			service.request(t, direct.URL, first)
			a.request(t, "udp://"+directUDP.LocalAddr().String(), first)
			b.request(t, "udp://"+directUDP.LocalAddr().String(), other)
			service.request(t, "udp://"+directUDP.LocalAddr().String(), first)
			a.request(t, strings.Replace(direct.URL, "127.0.0.1", "direct.test", 1), "DIRECT")
			t.Log("same PIDs survived initial policy, removal and re-addition:", strconv.Itoa(a.command.Process.Pid), strconv.Itoa(b.command.Process.Pid))
		})
	}
}

func TestLiveUpdateFailureRestoresOldProvider(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(rollbackFails), func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(dir, "Old.app")
			os.Mkdir(app, 0700)
			old := []string{app}
			if err := writeLiveRules(dir, old); err != nil {
				t.Fatal(err)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					calls++
					if calls == 1 || rollbackFails {
						w.WriteHeader(500)
					} else {
						w.WriteHeader(204)
					}
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"providers": map[string]any{liveRulesProvider: map[string]any{"ruleCount": 1}}})
			}))
			defer server.Close()
			port, _ := strconv.Atoi(strings.Split(server.URL, ":")[2])
			handle, _ := os.Open(dir)
			defer handle.Close()
			c := &controlFiles{directory: handle, uid: uint32(os.Getuid()), request: routeRequest{LiveRouting: true, OwnerPID: os.Getpid(), SOCKSPort: 1234, Token: strings.Repeat("a", 40), Gateway: "127.0.0.1", AppPaths: old}}
			os.WriteFile(filepath.Join(dir, "command.json"), []byte(`{"sequence":1,"action":"updateApps","appPaths":[]}`), 0600)
			err = c.handleLiveCommand(context.Background(), server.Client(), uint16(port), dir)
			if (err != nil) != rollbackFails {
				t.Fatalf("unexpected rollback error %v", err)
			}
			if c.snapshot.CommandResult.Success || len(c.request.AppPaths) != 1 {
				t.Fatal("failed update changed committed state")
			}
			content, _ := os.ReadFile(filepath.Join(dir, liveRulesFile))
			if !strings.Contains(string(content), "Old") {
				t.Fatal("old file not restored")
			}
			st, _ := os.Stat(filepath.Join(dir, liveRulesFile))
			if st.Mode().Perm() != 0600 {
				t.Fatal("rules permissions")
			}
			// Replaying an acknowledged command must not retry or disturb connections.
			previousCalls := calls
			c.handleLiveCommand(context.Background(), server.Client(), uint16(port), dir)
			if calls != previousCalls {
				t.Fatal("replayed update")
			}
		})
	}
}
