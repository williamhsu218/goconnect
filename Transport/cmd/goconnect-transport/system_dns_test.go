package main

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

func TestLiveSystemDNSPreservesNarrowResolverPaths(t *testing.T) {
	resolvers, err := parseSystemDNS("resolver #1\n nameserver[0] : 192.168.31.1\n nameserver[1] : fe80::1%en0\n nameserver[2] : 192.168.31.1\n nameserver[3] : 127.0.0.1\n nameserver[4] : 100.100.100.100\n search domain[0] : private.example\n")
	if err != nil || len(resolvers) != 3 {
		t.Fatal(resolvers, err)
	}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r := routeRequest{LiveRouting: true, RoutingMode: mode, Gateway: "192.0.2.10", systemDNS: resolvers}
		ts := tailscaleBypass{Interfaces: []string{"utun9"}, Prefixes: []netip.Prefix{tailnetIPv4}}
		pf := liveRoutingRules(501, "utun100", r, ts)
		dns := "to 192.168.31.1 port 53 user 501 flags any no state"
		if at := strings.Index(pf, dns); at < 0 || at > strings.Index(pf, "route-to") || at < strings.Index(pf, "to 100.64.0.0/10") {
			t.Fatal("DNS/Tailscale/capture order", pf)
		}
		if !strings.Contains(pf, "inet6 proto { tcp udp } from any to fe80::1 port 53 user 501 flags any no state") {
			t.Fatal("scoped resolver missing", pf)
		}
		if strings.Contains(pf, "to 192.168.0.0/16") || strings.Contains(pf, "to 192.168.31.1 user") {
			t.Fatal("broad LAN bypass", pf)
		}
		r.ProbeOnly = true
		if strings.Contains(liveRoutingRules(501, "utun100", r, ts), "port 53") {
			t.Fatal("probe bypasses unrelated traffic")
		}
	}
	for _, invalid := range []string{"nameserver[0] : 0.0.0.0", "nameserver[0] : 224.0.0.1", "nameserver[0] : a;bad"} {
		if _, err := parseSystemDNS(invalid); err == nil {
			t.Fatal("invalid resolver accepted")
		}
	}
	// Resolver exceptions are root-discovered, not injectable via session data.
	d := json.NewDecoder(bytes.NewBufferString(`{"systemDNS":["8.8.8.8"]}`))
	d.DisallowUnknownFields()
	if d.Decode(&routeRequest{}) == nil {
		t.Fatal("client supplied resolver bypass")
	}
}
