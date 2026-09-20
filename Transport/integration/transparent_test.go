package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"goconnect.local/transport/internal/bridge"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transparentFixtureDialer struct {
	target string
	udp    string
	calls  atomic.Int64
}

func (d *transparentFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if (network == "tcp" && address != "1.1.1.1:80") || (network == "udp" && address != "1.1.1.1:53") {
		return nil, fmt.Errorf("unexpected target")
	}
	d.calls.Add(1)
	target := d.target
	if network == "udp" {
		target = d.udp
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, target)
	if err != nil {
		return nil, err
	}
	if network == "udp" {
		return transparentUDPConn{Conn: conn}, nil
	}
	return conn, nil
}

type transparentUDPConn struct{ net.Conn }

func (c transparentUDPConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 53}
}

// An ordinary process remains alive before, during and after VPN setup. It has
// no proxy settings, and its executable lives inside the selected App bundle.
func TestTransparentChildProcess(t *testing.T) {
	if os.Getenv("GOCONNECT_TRANSPARENT_CHILD") != "1" {
		t.Skip("child only")
	}
	fmt.Println("READY")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var reply []byte
		var err error
		if scanner.Text() == "tcp" {
			client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			var resp *http.Response
			resp, err = client.Get("http://1.1.1.1/")
			if err == nil {
				reply, err = io.ReadAll(io.LimitReader(resp.Body, 32768))
				resp.Body.Close()
			}
		} else {
			var conn net.Conn
			conn, err = net.DialTimeout("udp4", "1.1.1.1:53", 3*time.Second)
			if err == nil {
				conn.SetDeadline(time.Now().Add(8 * time.Second))
				// DNS A example.com; public and credential-free.
				query := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
				_, err = conn.Write(query)
				if err == nil {
					reply = make([]byte, 4096)
					var n int
					n, err = conn.Read(reply)
					reply = reply[:n]
				}
				conn.Close()
			}
		}
		result := map[string]any{"vpn": strings.Contains(string(reply), "GOCONNECT-VPN-FIXTURE"), "bytes": len(reply), "error": ""}
		if err != nil {
			result["error"] = err.Error()
		}
		json.NewEncoder(os.Stdout).Encode(result)
	}
	os.Exit(0)
}

func transparentScopedDefaults(t *testing.T) string {
	t.Helper()
	var rows []string
	for _, family := range []string{"inet", "inet6"} {
		b, e := exec.Command("/usr/sbin/netstat", "-rn", "-f", family).Output()
		if e != nil {
			t.Fatal(e)
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 4 && f[0] == "default" && f[3] == "en0" && strings.Contains(f[2], "I") {
				rows = append(rows, family+":"+f[1])
			}
		}
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

// Explicitly opted-in: one public test host plus a documentation-only prefix; never captures default routes.
func TestInstalledTransparentApplicationModes(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_TRANSPARENT") != "1" {
		t.Skip("explicit installed-service TUN test required")
	}
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Fatal("runtime required")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "GOCONNECT-VPN-FIXTURE") }))
	defer fixture.Close()
	udp, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 4096)
		for {
			_, peer, err := udp.ReadFrom(b)
			if err != nil {
				return
			}
			udp.WriteTo([]byte("GOCONNECT-VPN-FIXTURE"), peer)
		}
	}()
	dir, e := os.MkdirTemp("", "GoConnect-transparent-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		t.Fatal(e)
	}
	binaries := []string{}
	for _, name := range []string{"Selected.app", "Other.app", "Selected.app/Contents/Frameworks/Helper.app"} {
		binary := filepath.Join(dir, name, "Contents/MacOS/Probe")
		if e = os.MkdirAll(filepath.Dir(binary), 0700); e != nil {
			t.Fatal(e)
		}
		self, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		data, e := os.ReadFile(self)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(binary, data, 0700); e != nil {
			t.Fatal(e)
		}
		binaries = append(binaries, binary)
	}
	service := filepath.Join(dir, "SelectedService")
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(self)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(service, data, 0700); e != nil {
		t.Fatal(e)
	}
	binaries = append(binaries, service)
	for _, mode := range []string{"whitelist", "global"} {
		t.Run(mode, func(t *testing.T) {
			scopedBefore := transparentScopedDefaults(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			listener, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			dialer := &transparentFixtureDialer{target: fixture.Listener.Addr().String(), udp: udp.LocalAddr().String()}
			token := strings.Repeat("transparent-test-", 4)
			go (&bridge.SOCKSServer{Dialer: dialer, Token: token}).Serve(ctx, listener)
			type probe struct {
				cmd *exec.Cmd
				in  io.WriteCloser
				out *bufio.Scanner
			}
			probes := []probe{}
			for _, binary := range binaries {
				cmd := exec.Command(binary, "-test.run=^TestTransparentChildProcess$")
				cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GOCONNECT_TRANSPARENT_CHILD=1"}
				input, err := cmd.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				scanner := bufio.NewScanner(output)
				if !scanner.Scan() || scanner.Text() != "READY" {
					cmd.Process.Kill()
					t.Fatal("probe did not become ready")
				}
				probes = append(probes, probe{cmd, input, scanner})
			}
			defer func() {
				for _, p := range probes {
					p.in.Close()
					p.cmd.Wait()
				}
			}()
			// Seed a scoped route cache, as an already-connected VPN socket does.
			_ = exec.Command("/usr/bin/curl", "--disable", "--interface", "en0", "--noproxy", "*", "--connect-timeout", "1", "--max-time", "1", "-s", "http://203.0.113.5/").Run()
			control := filepath.Join(dir, mode)
			if e = os.Mkdir(control, 0700); e != nil {
				t.Fatal(e)
			}
			lease := filepath.Join(control, "lease")
			os.WriteFile(lease, []byte(token), 0600)
			go func() {
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case now := <-tick.C:
						os.Chtimes(lease, now, now)
					}
				}
			}()
			request := map[string]any{"transparentRouting": true, "liveRouting": true, "probeOnly": true, "ownerPID": os.Getpid(), "controlDirectory": control, "token": token, "socksPort": listener.Addr().(*net.TCPAddr).Port, "gateway": "203.0.113.5", "routingMode": mode, "appPaths": []string{filepath.Join(dir, "Selected.app"), service}}
			b, _ := json.Marshal(request)
			file := filepath.Join(control, "session.json")
			os.WriteFile(file, b, 0600)
			cmd := exec.Command(filepath.Join(runtime, "bin/GoConnectTransport"), "service-route", file)
			var workerOutput bytes.Buffer
			cmd.Stdout = &workerOutput
			cmd.Stderr = &workerOutput
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			ended := make(chan error, 1)
			go func() { ended <- cmd.Wait() }()
			stopped := false
			defer func() {
				os.Remove(lease)
				if !stopped {
					select {
					case <-ended:
					case <-time.After(18 * time.Second):
						cmd.Process.Kill()
						t.Error("worker did not exit")
					}
				}
			}()
			var status struct{ State, Message, SystemDevice string }
			ready := false
			for start := time.Now(); time.Since(start) < 25*time.Second; {
				b, _ = os.ReadFile(filepath.Join(control, "status"))
				json.Unmarshal(b, &status)
				if status.State == "ready" {
					ready = true
					break
				}
				if status.State == "failed" {
					t.Fatal(status.Message)
				}
				select {
				case e := <-ended:
					stopped = true
					data, _ := os.ReadFile(filepath.Join(control, "status"))
					json.Unmarshal(data, &status)
					t.Fatalf("worker ended: %v %s state=%s message=%s", e, workerOutput.String(), status.State, status.Message)
				case <-time.After(200 * time.Millisecond):
				}
			}
			if !ready {
				t.Fatal("TUN not ready")
			}
			// Check the static outer-gateway route survives capture of its parent
			// prefix and the first health interval, unlike the 0.10.0 cloned route.
			time.Sleep(6 * time.Second)
			outer, err := exec.Command("/sbin/route", "-n", "get", "203.0.113.5").Output()
			if err != nil || strings.Contains(string(outer), status.SystemDevice) || !strings.Contains(string(outer), "HOST") {
				t.Fatalf("outer gateway pin failed: %s", outer)
			}
			t.Log("static gateway bypass survived enclosing TUN route")
			for i, p := range probes {
				want := i != 1
				if mode == "global" {
					want = !want
				}
				for _, protocol := range []string{"tcp", "udp"} {
					fmt.Fprintln(p.in, protocol)
					if !p.out.Scan() {
						t.Fatal("probe exited")
					}
					var result struct {
						VPN   bool
						Bytes int
						Error string
					}
					if err := json.Unmarshal(p.out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Error != "" || result.Bytes == 0 || result.VPN != want {
						t.Fatalf("App %d %s: %+v wantVPN=%v", i, protocol, result, want)
					}
					t.Logf("already-running App %d %s VPN=%v", i, protocol, result.VPN)
				}
			}
			wantCalls := int64(6)
			if mode == "global" {
				wantCalls = 2
			}
			if dialer.calls.Load() != wantCalls {
				t.Fatalf("unexpected VPN flow count %d", dialer.calls.Load())
			}

			os.Remove(lease)
			select {
			case e := <-ended:
				stopped = true
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(18 * time.Second):
				t.Fatal("cleanup timeout")
			}
			route, _ := exec.Command("/sbin/route", "-n", "get", "1.1.1.1").Output()
			if strings.Contains(string(route), status.SystemDevice) {
				t.Fatal("test route remains")
			}
			if after := transparentScopedDefaults(t); after != scopedBefore {
				t.Fatal("scoped default cleanup changed original routes", after)
			}
			t.Log("owned scoped defaults cleaned; original scoped defaults preserved")
		})
	}
}
