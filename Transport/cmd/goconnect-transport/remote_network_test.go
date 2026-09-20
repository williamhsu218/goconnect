package main

import (
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteAdvertisementBoundsAndMasks(t *testing.T) {
	env := map[string]string{"INTERNAL_IP4_ADDRESS": "10.1.2.3"}
	read := func(k string) string { return env[k] }
	inc, exc, err := advertisedRemoteNetworks(read)
	if err != nil || len(inc) != 3 || len(exc) != 0 {
		t.Fatal(inc, exc, err)
	}
	env["CISCO_SPLIT_INC"] = "1"
	env["CISCO_SPLIT_INC_0_ADDR"] = "192.168.14.0"
	env["CISCO_SPLIT_INC_0_MASK"] = "255.255.255.0"
	inc, _, err = advertisedRemoteNetworks(read)
	if err != nil || strings.Join(inc, ",") != "192.168.14.0/24" {
		t.Fatal(inc, err)
	}
	env["CISCO_SPLIT_INC_0_PROTOCOL"] = "6"
	inc, _, err = advertisedRemoteNetworks(read)
	if err != nil || len(inc) != 0 {
		t.Fatal("protocol-specific include widened", inc, err)
	}
	delete(env, "CISCO_SPLIT_INC_0_PROTOCOL")
	env["CISCO_SPLIT_INC_0_ADDR"] = "203.0.113.0"
	inc, _, err = advertisedRemoteNetworks(read)
	if err != nil || len(inc) != 0 {
		t.Fatal("public routes became private permission", inc, err)
	}
	env["CISCO_SPLIT_INC"] = "-1"
	if _, _, err = advertisedRemoteNetworks(read); err == nil {
		t.Fatal("malformed count grants default")
	}
	for _, s := range []string{"0.0.0.0/0", "192.0.0.0/8", "172.0.0.0/8", "100.64.0.0/10", "fe80::/64", "127.0.0.1", "8.8.8.8"} {
		if _, ok := remotePrefix(s); ok {
			t.Fatal(s)
		}
	}
}

func systemRequest() routeRequest {
	return routeRequest{LiveRouting: true, RoutingMode: modeWhitelist, OwnerPID: 123, SOCKSPort: 1234, Token: strings.Repeat("x", 40), Gateway: "192.168.14.1", RemoteNetworks: []string{"192.168.0.0/16", "fc00::/7"}, RemoteExcluded: []string{"192.168.22.0/24"}, systemDevice: "utun101", coreGID: 60001,
		systemLAN: []localNetworkRoute{{Prefix: netip.MustParsePrefix("192.168.31.0/24"), Interface: "en0"}}, systemDNS: []netip.Addr{netip.MustParseAddr("192.168.31.1")}, DirectDomains: []domainRule{{Domain: "192.168.23.0/24"}, {Domain: "example.com"}}}
}
func TestRemoteSystemRulesAreScopedAndParse(t *testing.T) {
	r := systemRequest()
	ts := tailscaleBypass{Interfaces: []string{"utun7"}, Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6, netip.MustParsePrefix("192.168.25.0/24")}}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r.RoutingMode = mode
		rules := liveRoutingRules(501, "utun100", r, ts)
		capture := strings.Index(rules, "route-to (utun101")
		for _, text := range []string{"user 0 group 60001", "pass out quick on utun7", "to 192.168.25.0/24 user < 500", "to 192.168.31.0/24", "to 192.168.23.0/24", "to 192.168.22.0/24", "to 192.168.14.1/32"} {
			at := strings.Index(rules, text)
			if at < 0 || at > capture {
				t.Fatal("missing precedence", text, rules)
			}
		}
		for _, line := range strings.Split(rules, "\n") {
			if strings.Contains(line, "route-to (utun101") && (!strings.Contains(line, "user < 500") || strings.Contains(line, "to any")) {
				t.Fatal("broad system capture", line)
			}
		}
		f := filepath.Join(t.TempDir(), "rules.pf")
		os.WriteFile(f, []byte(rules), 0600)
		if out, err := exec.Command("/sbin/pfctl", "-n", "-f", f).CombinedOutput(); err != nil {
			t.Fatalf("PF syntax: %v %s", err, out)
		}
	}
	r.ProbeOnly = true
	if systemRoutingRules(r, ts) != "" {
		t.Fatal("diagnostic broadened")
	}
}
func TestRemoteCoreKeepsAppFallbackAndDedicatedPolicy(t *testing.T) {
	r := systemRequest()
	config := makeLiveConfig(r, "utun100", 19091)
	rules := config["rules"].([]string)
	if rules[len(rules)-1] != "MATCH,REJECT" {
		t.Fatal("weakened whitelist", rules)
	}
	system := config["sub-rules"].(map[string]any)[systemInbound].([]string)
	if system[0] != "IP-CIDR,192.168.23.0/24,DIRECT,no-resolve" || system[len(system)-1] != "MATCH,GoConnect" {
		t.Fatal(system)
	}
	// Validate against the bundled parser, without starting either TUN.
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		runtime = "/Applications/GoConnect.app/Contents/Resources/Runtime"
	}
	if _, err := os.Stat(filepath.Join(runtime, "bin/mihomo")); err != nil {
		t.Skip("Mihomo unavailable")
	}
	dir := t.TempDir()
	if err := writeLiveRules(dir, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config)
	file := filepath.Join(dir, "config.json")
	os.WriteFile(file, data, 0600)
	if out, err := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-t", "-d", dir, "-f", file).CombinedOutput(); err != nil {
		t.Fatalf("Mihomo: %v %s", err, out)
	}
}
