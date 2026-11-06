package main

import (
	"context"
	"encoding/base64"
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
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
	"golang.org/x/net/dns/dnsmessage"
)

func TestSubscriptionDNSKeepsOtherProtocols(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "socks5", "http", "anytls", "trojan", "ss"} {
		config := map[string]any{"dns": map[string]any{"enable": false}, "rules": []string{"MATCH,Selected"}}
		before := map[string]any{"dns": map[string]any{"enable": false}, "rules": []string{"MATCH,Selected"}}
		configureSubscriptionDNS(config, map[string]any{"type": kind})
		if !reflect.DeepEqual(config, before) {
			t.Fatalf("changed %s DNS behavior", kind)
		}
	}
}

// A real bundled core receives UDP DNS but reaches an HTTPS resolver only via
// the selected proxy. Private DNS and non-DNS UDP must not reach that resolver.
// The fixtures are loopback-only and never change native DNS, PF or TUN.
func TestLiveSubscriptionReliableDNS(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("packaged runtime required")
	}
	var queries, connects atomic.Int32
	resolver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.URL.Path != "/dns-query" || (r.Method != "POST" && r.Method != "GET") {
			http.Error(w, "expected HTTPS DNS", 400)
			return
		}
		var body []byte
		var err error
		if r.Method == "GET" {
			body, err = base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
		} else {
			body, err = io.ReadAll(io.LimitReader(r.Body, 4097))
		}
		if err != nil || len(body) > 4096 {
			http.Error(w, "invalid query", 400)
			return
		}
		var msg dnsmessage.Message
		if msg.Unpack(body) != nil || len(msg.Questions) != 1 || msg.Response {
			http.Error(w, "invalid DNS", 400)
			return
		}
		queries.Add(1)
		msg.Response = true
		msg.RecursionAvailable = true
		msg.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: msg.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 123}}}}
		answer, _ := msg.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(answer)
	}))
	defer resolver.Close()
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := &dnsFixtureDialer{target: resolver.Listener.Addr().String(), connects: &connects}
	go (&bridge.SOCKSServer{Dialer: dialer, Token: "dns-fixture"}).Serve(ctx, upstream)
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"mode": "rule", "log-level": "debug", "ipv6": true, "allow-lan": false, "bind-address": "127.0.0.1", "socks-port": port, "tun": map[string]any{"enable": false}}
	configureSubscriptionDNS(config, map[string]any{"name": "Selected", "type": "hysteria2"})
	proxyPort := upstream.Addr().(*net.TCPAddr).Port
	config["proxies"].([]map[string]any)[0] = map[string]any{"name": "Selected", "type": "socks5", "server": "127.0.0.1", "port": proxyPort, "udp": true, "username": "goconnect", "password": "dns-fixture"}
	dns := config["dns"].(map[string]any)
	// Only the isolated fixture relaxes certificate verification. Production
	// uses the normal trust store for both public DoH endpoints.
	dns["nameserver"] = []string{resolver.URL + "/dns-query#Selected&skip-cert-verify=true"}
	dir := t.TempDir()
	data, _ := json.Marshal(config)
	file := filepath.Join(dir, "config.json")
	os.WriteFile(file, data, 0600)
	cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", file)
	logFile, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			log, _ := os.ReadFile(logFile.Name())
			t.Logf("core log: %s", log)
		}
	}()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	ready := false
	for i := 0; i < 100; i++ {
		c, e := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		t.Fatal("core not ready")
	}
	// The core binds TCP before UDP. A successful TCP connect alone is not
	// evidence that the UDP listener can receive the first test datagram.
	udpReady := false
	for i := 0; i < 100; i++ {
		listener, e := net.ListenPacket("udp4", address)
		if e != nil {
			udpReady = true
			break
		}
		listener.Close()
		time.Sleep(10 * time.Millisecond)
	}
	if !udpReady {
		t.Fatal("core UDP listener not ready")
	}
	// Listener creation precedes the core's Running state. Keep fixture
	// startup outside the first-packet DNS acceptance measurement.
	time.Sleep(100 * time.Millisecond)
	control, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.SetDeadline(time.Now().Add(2 * time.Second))
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { relay.Close() }()
	control.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err = io.ReadFull(control, reply); err != nil || !reflect.DeepEqual(reply, []byte{5, 0}) {
		t.Fatalf("SOCKS greeting: %v", err)
	}
	request := binary.BigEndian.AppendUint16([]byte{5, 3, 0, 1, 127, 0, 0, 1}, uint16(relay.LocalAddr().(*net.UDPAddr).Port))
	control.Write(request)
	reply = make([]byte, 10)
	if _, err = io.ReadFull(control, reply); err != nil || reply[1] != 0 || reply[3] != 1 {
		t.Fatalf("SOCKS UDP associate: %v", err)
	}
	control.SetDeadline(time.Time{})
	relayAddress := &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))}
	lastDestination := "8.8.8.8:53"
	query := func(destination string, destinationPort uint16, index int, timeout time.Duration) (dnsmessage.Message, error) {
		// Mihomo selects a UDP route per source socket. Independent private
		// DNS and non-DNS flows need their own source socket, as real clients
		// do; reusing one would test its existing source-NAT route cache.
		key := net.JoinHostPort(destination, strconv.Itoa(int(destinationPort)))
		if key != lastDestination {
			relay.Close()
			relay, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				return dnsmessage.Message{}, err
			}
			lastDestination = key
		}
		name := dnsmessage.MustNewName(fmt.Sprintf("q%d.reliable-dns.test.", index))
		msg := dnsmessage.Message{Header: dnsmessage.Header{ID: uint16(index + 1), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
		wire, _ := msg.Pack()
		packet := append([]byte{0, 0, 0, 1}, net.ParseIP(destination).To4()...)
		packet = binary.BigEndian.AppendUint16(packet, destinationPort)
		packet = append(packet, wire...)
		relay.SetDeadline(time.Now().Add(timeout))
		if _, err := relay.WriteToUDP(packet, relayAddress); err != nil {
			return msg, err
		}
		buffer := make([]byte, 4096)
		n, _, err := relay.ReadFromUDP(buffer)
		if err != nil {
			return msg, err
		}
		if n < 22 {
			return msg, fmt.Errorf("short DNS response")
		}
		var answer dnsmessage.Message
		if err = answer.Unpack(buffer[10:n]); err != nil {
			return msg, err
		}
		if answer.ID != msg.ID || !answer.Response || len(answer.Questions) != 1 || answer.Questions[0] != msg.Questions[0] {
			return msg, fmt.Errorf("response does not match query")
		}
		return answer, nil
	}
	for i := 0; i < 8; i++ {
		answer, err := query("8.8.8.8", 53, i, 3*time.Second)
		if err != nil || len(answer.Answers) != 1 {
			t.Fatalf("public UDP DNS did not use reliable resolver: %v", err)
		}
		if a, ok := answer.Answers[0].Body.(*dnsmessage.AResource); !ok || a.A != [4]byte{192, 0, 2, 123} {
			t.Fatal("DNS did not come from fixture resolver")
		}
	}
	if queries.Load() != 8 || connects.Load() < 1 {
		t.Fatalf("DoH bypassed selected proxy or query cache concealed forwarding: queries=%d connects=%d", queries.Load(), connects.Load())
	}
	for i, target := range []string{"10.0.0.53", "192.168.1.1", "100.100.100.100", "127.0.0.1"} {
		if _, err := query(target, 53, 20+i, 200*time.Millisecond); err == nil {
			t.Fatalf("private resolver %s was redirected to public DoH", target)
		}
	}
	if _, err := query("8.8.8.8", 123, 30, 200*time.Millisecond); err == nil {
		t.Fatal("non-DNS UDP was redirected")
	}
	if queries.Load() != 8 {
		t.Fatal("private DNS or non-DNS UDP reached DoH")
	}
}

// The selected SOCKS fixture supports UDP but never sends it to the network.
// This prevents Mihomo from skipping private-DNS rules for lack of UDP support.
type dnsFixtureDialer struct {
	target   string
	connects *atomic.Int32
}

func (d *dnsFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address != d.target {
		return nil, fmt.Errorf("fixture refuses %s", network)
	}
	d.connects.Add(1)
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
