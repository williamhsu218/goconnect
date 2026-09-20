package main

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

func directIPPrefix(value string) (netip.Prefix, bool) {
	p, err := netip.ParsePrefix(value)
	if err != nil {
		a, e := netip.ParseAddr(value)
		if e != nil || a.Zone() != "" || a.Is4In6() {
			return netip.Prefix{}, false
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	a := p.Addr()
	if p.Bits() == 0 || p != p.Masked() || a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() || a.Is4In6() {
		return netip.Prefix{}, false
	}
	if a.Is4() && (a.As4()[0] == 0 || a.As4()[0] >= 224) {
		return netip.Prefix{}, false
	}
	return p, true
}

func localDiscoveryRules(uid uint32, routes []localNetworkRoute) string {
	interfaces := map[string]bool{}
	for _, r := range routes {
		interfaces[r.Interface] = true
	}
	names := []string{}
	for name := range interfaces {
		names = append(names, name)
	}
	sort.Strings(names)
	var rules strings.Builder
	for _, name := range names {
		// Link-local and administratively scoped discovery only, on an actual LAN
		// interface; do not bypass all globally routable multicast on every tunnel.
		fmt.Fprintf(&rules, "pass out quick on %s inet proto udp from any to { 224.0.0.0/24, 239.255.0.0/16, 255.255.255.255 } user %d no state\n", name, uid)
		fmt.Fprintf(&rules, "pass out quick on %s inet6 proto udp from any to { ff01::/16, ff02::/16, ff05::/16 } user %d no state\n", name, uid)
	}
	return rules.String()
}
