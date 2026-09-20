package main

import (
	"net/netip"
	"os/exec"
	"strings"
	"testing"
)

func TestLiveActualPacketFilterSyntax(t *testing.T) {
	tailscale, err := discoverTailscale()
	if err != nil {
		t.Fatal(err)
	}
	local, err := discoverLocalNetworks()
	if err != nil {
		t.Fatal(err)
	}
	dns, err := discoverSystemDNS()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		for _, probe := range []bool{false, true} {
			for _, live := range []bool{false, true} {
				r := routeRequest{LiveRouting: live, RoutingMode: mode, Gateway: "192.0.2.10", ProbeOnly: probe, systemLAN: local, systemDNS: dns, DirectDomains: []domainRule{{Domain: "192.168.2.0/24"}, {Domain: "fd00:1234::/64"}, {Domain: "192.0.2.11"}}}
				rules := routingRules(501, []capturedApp{{GID: 61001}}, "utun100", r, tailscale)
				// -n parses only; never enables PF, loads an anchor or touches state.
				command := exec.Command("/sbin/pfctl", "-n", "-a", "com.apple/goconnect-syntax-qa", "-f", "-")
				command.Stdin = strings.NewReader(rules)
				if output, e := command.CombinedOutput(); e != nil {
					t.Fatalf("%s probe=%v live=%v: %s", mode, probe, live, output)
				}
			}
		}
	}
}

func TestModesKeepCorporateTrafficAndExcludeUnmarkedDirectApps(t *testing.T) {
	r := routeRequest{OwnerPID: 123, Token: strings.Repeat("x", 40), SOCKSPort: 1234, Gateway: "192.0.2.10", RoutingMode: modeGlobal}
	if err := r.validate(); err != nil {
		t.Fatal("global must support an empty exclusion list", err)
	}
	r.AppPaths = []string{"/Applications/Direct (QA).app"}
	rules := makeConfig(r, "", "utun100", 19000)["rules"].([]string)
	if len(rules) != 2 || !strings.HasSuffix(rules[0], ",REJECT") || rules[1] != "MATCH,GoConnect" {
		t.Fatal(rules)
	}
	for _, rule := range rules {
		if strings.Contains(rule, ",DIRECT") {
			t.Fatal("unmarked excluded apps must not be relayed through TUN", rule)
		}
	}
	r.RoutingMode = "typo"
	if r.validate() == nil {
		t.Fatal("unknown policy accepted")
	}
}

func TestGlobalBypassPrecedesCaptureAndPreservesOtherUsers(t *testing.T) {
	r := routeRequest{RoutingMode: modeGlobal, Gateway: "192.0.2.10"}
	apps := []capturedApp{{GID: 61010}}
	ts := tailscaleBypass{Interfaces: []string{"utun9"}, Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6, netip.MustParsePrefix("192.168.14.0/24")}}
	rules := routingRules(501, apps, "utun100", r, ts)
	excluded, bypass, block, capture := strings.Index(rules, "group 61010"), strings.Index(rules, "on utun9"), strings.Index(rules, "to 100.64.0.0/10"), strings.Index(rules, "route-to")
	if !(excluded >= 0 && excluded < bypass && bypass < block && block < capture) {
		t.Fatal("incorrect bypass precedence", rules)
	}
	if !strings.Contains(rules[:capture], "to 192.0.2.10 user 501") {
		t.Fatal("VPN transport would loop")
	}
	if strings.Contains(rules, "10.0.0.0/8") || strings.Contains(rules, "172.16.0.0/12") || strings.Contains(rules, "192.168.0.0/16") {
		t.Fatal("corporate private routes must remain on VPN")
	}
	for _, line := range strings.Split(strings.TrimSpace(rules), "\n") {
		if !strings.Contains(line, "user 501") || !strings.Contains(line, "proto { tcp udp }") {
			t.Fatal("rule affects other users/system services", line)
		}
		if strings.HasPrefix(line, "pass ") && !strings.Contains(line, "no state") {
			t.Fatal("state could survive a policy change", line)
		}
	}
	r.ProbeOnly = true
	probe := routingRules(501, apps, "utun100", r, ts)
	if !strings.Contains(probe, "route-to (utun100 198.19.254.1) inet proto { tcp udp } from any to 1.1.1.1 user 501") || strings.Contains(probe, "inet6 proto { tcp udp } from any to any user 501\n") {
		t.Fatal("global test changes unrelated traffic", probe)
	}
}

func TestWhitelistTailscaleRulesRemainAppScopedAndSurviveLinkLoss(t *testing.T) {
	apps := []capturedApp{{GID: 61001}, {GID: 61002}}
	ts := tailscaleBypass{Interfaces: []string{"utun9"}, Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6}}
	before := routingRules(501, apps, "utun100", routeRequest{}, ts)
	for _, line := range strings.Split(strings.TrimSpace(before), "\n") {
		if !strings.Contains(line, "user 501 group 6100") {
			t.Fatal("ordinary apps were affected", line)
		}
	}
	ts.Interfaces = nil
	after := routingRules(501, apps, "utun100", routeRequest{}, ts)
	if strings.Contains(after, "on utun9") || !strings.Contains(after, "block return out quick on ! lo0 inet proto { tcp udp } from any to 100.64.0.0/10") {
		t.Fatal("disconnected tailnet could be sent to corporate VPN", after)
	}
}

func TestTailscaleRoutesUseOnlyItsActualInterfaces(t *testing.T) {
	table := `Destination Gateway Flags Netif Expire
default 192.0.2.1 UGScg en8
default link#25 UCSIg utun9
100.64/10 link#25 UCS utun9
100.100.100.100/32 link#25 UCS utun9
192.168.14/24 link#25 UCS utun9
10/8 link#3 UCS en8
fd7a:115c:a1e0::/48 link#25 UCS utun9
fe80::%utun9/64 link#25 UCI utun9
ff00::/8 link#25 UmCI utun9
malformed link#25 UCS utun9`
	prefixes := tailscaleRoutePrefixes(table, []string{"utun9"})
	var values []string
	for _, p := range prefixes {
		values = append(values, p.String())
	}
	if strings.Join(values, ",") != "100.64.0.0/10,192.168.14.0/24,fd7a:115c:a1e0::/48" {
		t.Fatal(values)
	}
	remembered := mergePrefixes(prefixes, []netip.Prefix{tailnetIPv4})
	if len(remembered) != 3 {
		t.Fatal("subnet lost when Tailscale disconnected")
	}
	for _, input := range []string{"default", "0/0", "::/0", "1;evil/32", "1.2.3.4/33", "1.2.3.4/x", "ff00::/8"} {
		if _, ok := parseRoutePrefix(input); ok {
			t.Fatal("unsafe route accepted", input)
		}
	}
}

func TestDomainExceptionsDoNotExpandWhitelistCapture(t *testing.T) {
	r := routeRequest{RoutingMode: modeWhitelist, Gateway: "192.0.2.10", DirectDomains: []domainRule{{"example.com", true}}}
	apps := []capturedApp{{GID: 61010}}
	ts := tailscaleBypass{Interfaces: []string{"utun9"}, Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6}}
	rules := routingRules(501, apps, "utun100", r, ts)
	for _, line := range strings.Split(strings.TrimSpace(rules), "\n") {
		if !strings.Contains(line, "user 501 group 61010") {
			t.Fatal("domain exception captured another app", line)
		}
	}
	r.DirectDomains = nil
	if rules != routingRules(501, apps, "utun100", r, ts) {
		t.Fatal("domain exceptions must not alter PF capture scope")
	}
}
