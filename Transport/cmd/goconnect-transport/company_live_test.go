package main

import (
	"context"
	"encoding/json"
	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type companyPrimaryFixture struct{ companyFixtureDialer }

func (d *companyPrimaryFixture) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.companyFixtureDialer.DialContext(ctx, network, "10.201.0.9:443")
}

// Real Mihomo forwarding with TUN/SCDynamicStore explicitly absent. Real system
// ingress, resolver selection and SMB remain separate live acceptance gates.
func TestLiveCompanyGateStableCore(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("set packaged runtime for isolated real-core acceptance")
	}
	gate, claim, companyFixture := fixtureCompanyGate(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	primaryListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer primaryListener.Close()
	token := strings.Repeat("p", 64)
	go func() {
		_ = (&bridge.SOCKSServer{Token: token, Dialer: &companyPrimaryFixture{}}).Serve(ctx, primaryListener)
	}()
	controller, _ := freePort()
	socks, _ := freePort()
	dnsPort, _ := freePort()
	request := routeRequest{RoutingMode: modeGlobal, Token: token, SOCKSPort: uint16(primaryListener.Addr().(*net.TCPAddr).Port), managedDNS: &managedDNSPlan{port: dnsPort, resolvers: []nativeResolver{{servers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port: 53}}}}
	config := makeTransparentConfig(request, "utun999", controller, originalNetwork{Interface: "lo0"}, nil, tailscaleBypass{})
	config["tun"].(map[string]any)["enable"] = false
	config["socks-port"] = socks
	config["log-level"] = "warning"
	gate.configure(config)
	directory := t.TempDir()
	data, _ := json.Marshal(config)
	configPath := filepath.Join(directory, "config.json")
	if err = os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	core := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", directory, "-f", configPath)
	core.Env = managedTunEnvironment()
	log, err := os.Create(filepath.Join(directory, "core.log"))
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
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socks)))
	var primary net.Conn
	front, _ := proxy.SOCKS5("tcp", address, nil, nil)
	for i := 0; i < 50; i++ {
		primary, err = front.Dial("tcp", "198.51.100.9:443")
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		logs, _ := os.ReadFile(filepath.Join(directory, "core.log"))
		t.Fatalf("core startup: %v %s", err, logs)
	}
	defer primary.Close()
	primary.SetDeadline(time.Now().Add(4 * time.Second))
	initial := "GET / HTTP/1.1\r\nHost: primary.example.invalid\r\n\r\n"
	io.WriteString(primary, initial)
	initialReply := make([]byte, len(initial))
	if _, err = io.ReadFull(primary, initialReply); err != nil || string(initialReply) != initial {
		t.Fatalf("primary fixture: %v", err)
	}
	echoCompany(t, primary)
	for i := 0; i < 3; i++ {
		gen := companyTestGeneration[:35] + string(rune('1'+i))
		activateCompanyGate(t, gate, claim, gen)
		beforeBusiness := companyFixture.business.Load()
		company, err := front.Dial("tcp", "app.corp.example.invalid:443")
		if err != nil {
			t.Fatal(err)
		}
		echoCompany(t, company)
		if companyFixture.business.Load() != beforeBusiness+1 {
			t.Fatal("company business bypassed CompanyGate")
		}
		echoCompany(t, primary)
		if err = gate.command(context.Background(), companyCommand{ExpectedRevision: gate.snapshot().Revision, Generation: gen, Action: "block"}); err != nil {
			t.Fatal(err)
		}
		company.Close()
		echoCompany(t, primary)
		if core.ProcessState != nil {
			t.Fatal("primary core restarted or exited")
		}
	}
	activateCompanyGate(t, gate, claim, "00000000-0000-4000-8000-000000000009")
	beforeBusiness := companyFixture.business.Load()
	company, err := front.Dial("tcp", "10.201.0.9:443")
	if err != nil {
		t.Fatal(err)
	}
	defer company.Close()
	company.SetDeadline(time.Now().Add(3 * time.Second))
	payload := "GET / HTTP/1.1\r\nHost: outside.example.invalid\r\n\r\n"
	if _, err = io.WriteString(company, payload); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, len(payload))
	if _, err = io.ReadFull(company, reply); err != nil || string(reply) != payload {
		t.Fatalf("company literal sniffer changed target: %v", err)
	}
	if companyFixture.business.Load() != beforeBusiness+1 {
		t.Fatal("company literal leaked into primary")
	}
}
