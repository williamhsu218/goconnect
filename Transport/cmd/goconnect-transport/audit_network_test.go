package main

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestLiveAuditDiscoveryAndCIDRPriority(t *testing.T) {
	r := routeRequest{LiveRouting: true, Gateway: "192.0.2.1", RoutingMode: modeGlobal, systemLAN: []localNetworkRoute{{"en0", netip.MustParsePrefix("192.168.31.0/24")}}, DirectDomains: []domainRule{{Domain: "10.211.55.0/24"}, {Domain: "fd00:1234::/64"}}}
	pf := liveRoutingRules(501, "utun100", r, tailscaleBypass{Prefixes: []netip.Prefix{tailnetIPv4}, Interfaces: []string{"utun11"}})
	for _, want := range []string{"on en0 inet proto udp", "224.0.0.0/24", "239.255.0.0/16", "255.255.255.255", "ff02::/16", "to 10.211.55.0/24", "to fd00:1234::/64"} {
		at := strings.Index(pf, want)
		if at < strings.Index(pf, "to 100.64.0.0/10") || at > strings.Index(pf, "route-to") {
			t.Fatal(want, pf)
		}
	}
	if strings.Contains(pf, "224.0.0.0/4") || strings.Contains(pf, "ff00::/8") {
		t.Fatal("global multicast bypass")
	}
	r.ProbeOnly = true
	pf = liveRoutingRules(501, "utun100", r, tailscaleBypass{})
	if strings.Contains(pf, "239.255") || strings.Contains(pf, "10.211.55") {
		t.Fatal("probe scope expanded")
	}
	rules := directDomainRules(r.DirectDomains)
	if strings.Join(rules, ";") != "IP-CIDR,10.211.55.0/24,DIRECT,no-resolve;IP-CIDR6,fd00:1234::/64,DIRECT,no-resolve" {
		t.Fatal(rules)
	}
	for _, invalid := range []string{"0.0.0.0/0", "::/0", "127.0.0.1", "224.0.0.251", "255.255.255.255", "192.168.2.1/24", "10.0.0.0/8\npass all", "::ffff:192.168.1.1"} {
		if _, ok := directIPPrefix(invalid); ok {
			t.Fatal(invalid)
		}
	}
}
func TestLiveAuditExplicitLocalRoutes(t *testing.T) {
	attached := []localNetworkRoute{{"en0", netip.MustParsePrefix("192.168.31.0/24")}}
	table := "10/7 192.168.31.1 UGSc en0\n172/8 192.168.31.1 UGSc en0\ndefault 192.168.31.1 UGSc en0\n192.168.2 192.168.31.1 UGSc en0\n10.20/16 192.168.31.2 UGSc en0\n10.40/16 10.0.0.1 UGSc utun11\n10.50/16 10.0.0.1 UGSc en0\n8.8.8.0/24 192.168.31.1 UGSc en0\n10.60/16 192.168.31.1 UGBS en0\n"
	got := localRoutedNetworks(table, attached)
	if len(got) != 2 || got[0].Prefix.String() != "192.168.2.0/24" || got[1].Prefix.String() != "10.20.0.0/16" {
		t.Fatal(got)
	}
	for _, iface := range []string{"vnic0", "vmnet1"} {
		if _, ok := localAttachedRoute(iface, netip.MustParsePrefix("10.211.55.3/24")); !ok {
			t.Fatal(iface)
		}
	}
}
func TestLiveAuditNetworkJitterIsAtomicAndBounded(t *testing.T) {
	calls := 0
	poll := networkPoller{baseline: "en0", read: func() (routingNetworkState, error) {
		calls++
		if calls == 3 {
			return routingNetworkState{Interface: "en0"}, nil
		}
		return routingNetworkState{Interface: "en0", LAN: []localNetworkRoute{{"en1", netip.MustParsePrefix("10.0.0.0/8")}}}, errors.New("transient DNS failure")
	}}
	for i := 0; i < 2; i++ {
		s, e := poll.poll()
		if s != nil || e != nil {
			t.Fatal(s, e)
		}
	}
	s, e := poll.poll()
	if s == nil || e != nil || poll.failures != 0 || len(s.LAN) != 0 {
		t.Fatal(s, e)
	}
	for i := 0; i < 2; i++ {
		s, e := poll.poll()
		if s != nil || e != nil {
			t.Fatal(s, e)
		}
	}
	if s, e := poll.poll(); s != nil || e == nil {
		t.Fatal("continuous failure tolerated")
	}
	poll.read = func() (routingNetworkState, error) {
		return routingNetworkState{Interface: "utun12"}, errors.New("subsequent DNS failure")
	}
	if s, e := poll.poll(); s != nil || e == nil {
		t.Fatal("confirmed route change ignored")
	}
}
