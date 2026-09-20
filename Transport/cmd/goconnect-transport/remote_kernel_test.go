package main

import (
	"context"
	"encoding/json"
	"fmt"
	"goconnect.local/transport/internal/bridge"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Opt-in elevated acceptance. A unique PF anchor captures only root/system
// sockets to one synthetic private host; no current-user/default routes change.
func TestRemotePrivilegedTCPUDP(t *testing.T) {
	if os.Getenv("GOCONNECT_ROOT_REMOTE_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("explicit elevated fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 2048)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	socks, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer socks.Close()
	r := systemRequest()
	r.RemoteNetworks = []string{"192.168.254.254/32"}
	r.RemoteExcluded = nil
	r.DirectDomains = nil
	r.systemLAN = nil
	r.systemDNS = nil
	r.SOCKSPort = uint16(socks.Addr().(*net.TCPAddr).Port)
	go (&bridge.SOCKSServer{Dialer: systemFixtureDialer{tcp: echo.Addr().String(), udp: udp.LocalAddr().String()}, Token: r.Token}).Serve(ctx, socks)
	first, e := chooseDevice()
	if e != nil {
		t.Fatal(e)
	}
	r.systemDevice, e = chooseOtherDevice(first)
	if e != nil {
		t.Fatal(e)
	}
	r.coreGID, e = unusedCoreGID()
	if e != nil {
		t.Fatal(e)
	}
	port, e := freePort()
	if e != nil {
		t.Fatal(e)
	}
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Fatal("runtime required")
	}
	dir := t.TempDir()
	writeLiveRules(dir, nil)
	config := makeLiveConfig(r, first, port)
	config["tun"].(map[string]any)["enable"] = false
	data, _ := json.Marshal(config)
	file := filepath.Join(dir, "config.json")
	os.WriteFile(file, data, 0600)
	cmd := exec.CommandContext(ctx, filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: r.coreGID, Groups: []uint32{}}}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Signal(syscall.SIGTERM); cmd.Wait() }()
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	until := time.Now().Add(8 * time.Second)
	for !systemDeviceReady(r) || !liveRulesReady(client, port, r.Token, 1) {
		if time.Now().After(until) {
			t.Fatal("system listener not ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	out, e := systemCommand("/sbin/pfctl", "-si")
	if e != nil || !strings.Contains(string(out), "Status: Enabled") {
		t.Fatal("fixture requires already-enabled PF")
	}
	pf := &packetFilter{anchor: fmt.Sprintf("com.apple/goconnect-qa-system-%d", os.Getpid())}
	defer func() {
		if e := pf.close(); e != nil {
			t.Error(e)
		}
	}()
	if e = pf.load(systemRoutingRules(r, tailscaleBypass{})); e != nil {
		t.Fatal(e)
	}
	for _, network := range []string{"tcp4", "udp4"} {
		c, e := net.DialTimeout(network, "192.168.254.254:445", 4*time.Second)
		if e != nil {
			t.Fatal(network, e)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		challenge := []byte("goconnect-system-roundtrip-" + network)
		if _, e = c.Write(challenge); e != nil {
			c.Close()
			t.Fatal(e)
		}
		reply := make([]byte, len(challenge))
		_, e = io.ReadFull(c, reply)
		c.Close()
		if e != nil || string(reply) != string(challenge) {
			t.Fatalf("%s: %q %v", network, reply, e)
		}
		t.Log(network, "root socket -> PF -> system TUN -> authenticated VPN SOCKS -> echo -> reply passed")
	}
}

// The fixture routes to loopback but preserves the logical remote address in
// SOCKS UDP replies, as the real VPN stack does. A loopback source header would
// correctly be rejected by the core as a response from a different peer.
type systemFixtureDialer struct{ tcp, udp string }
type systemFixtureConn struct {
	net.Conn
	peer net.Addr
}

func (c systemFixtureConn) RemoteAddr() net.Addr { return c.peer }
func (d systemFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	target := d.tcp
	if network == "udp" {
		target = d.udp
	}
	c, e := (&net.Dialer{}).DialContext(ctx, network, target)
	if e != nil {
		return nil, e
	}
	if network == "udp" {
		a, e := net.ResolveUDPAddr("udp", address)
		if e != nil {
			c.Close()
			return nil, e
		}
		return systemFixtureConn{c, a}, nil
	}
	return c, nil
}
