package main

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

func TestLiveLocalNetworkScopeAndPriority(t *testing.T) {
	prefix := netip.MustParsePrefix("192.168.31.7/24")
	lan, ok := localAttachedRoute("en0", prefix)
	if !ok || lan.Prefix.String() != "192.168.31.0/24" {
		t.Fatal(lan)
	}
	for _, iface := range []string{"utun11", "lo0", "en0;bad", "awdl0"} {
		if _, ok := localAttachedRoute(iface, prefix); ok {
			t.Fatal(iface)
		}
	}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r := routeRequest{LiveRouting: true, RoutingMode: mode, Gateway: "192.0.2.1", systemLAN: []localNetworkRoute{lan}}
		pf := liveRoutingRules(501, "utun100", r, tailscaleBypass{Interfaces: []string{"utun11"}, Prefixes: []netip.Prefix{tailnetIPv4}})
		local := strings.Index(pf, "pass out quick on en0 inet proto { tcp udp } from any to 192.168.31.0/24 user 501")
		if local < strings.Index(pf, "to 100.64.0.0/10") || local > strings.Index(pf, "route-to") || local < 0 {
			t.Fatal(pf)
		}
		if strings.Contains(pf, "to 10.0.0.0/8") || strings.Contains(pf, "to 192.168.0.0/16") {
			t.Fatal("blanket private bypass")
		}
		r.ProbeOnly = true
		if strings.Contains(liveRoutingRules(501, "utun100", r, tailscaleBypass{}), "192.168.31.0/24") {
			t.Fatal("probe scope widened")
		}
	}
	gateway, ok := localGatewayRoute("gateway: 192.168.99.1\ninterface: en0\n", []localNetworkRoute{lan})
	if !ok || gateway.Prefix.String() != "192.168.99.1/32" {
		t.Fatal(gateway)
	}
	if _, ok := localGatewayRoute("gateway: 10.0.0.1\ninterface: utun1\n", []localNetworkRoute{lan}); ok {
		t.Fatal("VPN gateway bypass")
	}
	d := json.NewDecoder(bytes.NewBufferString(`{"systemLAN":[]}`))
	d.DisallowUnknownFields()
	if d.Decode(&routeRequest{}) == nil {
		t.Fatal("client injected LAN")
	}
}
