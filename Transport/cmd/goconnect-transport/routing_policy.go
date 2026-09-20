package main

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const modeWhitelist = "whitelist"
const modeGlobal = "global"

var tailnetIPv4 = netip.MustParsePrefix("100.64.0.0/10")
var tailnetIPv6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
var safeTunInterface = regexp.MustCompile(`^(utun|tun)[0-9]{1,5}$`)

type tailscaleBypass struct {
	Interfaces []string
	Prefixes   []netip.Prefix
}

// Discover routes from the kernel, never execute a user-writable VPN CLI as root.
// Requiring a tunnel interface prevents a CGNAT address on a physical NIC from
// turning that entire NIC into an accidental global bypass.
func discoverTailscale() (tailscaleBypass, error) {
	state := tailscaleBypass{Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6}}
	interfaces, err := net.Interfaces()
	if err != nil {
		return state, err
	}
	for _, iface := range interfaces {
		if !safeTunInterface.MatchString(iface.Name) || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, e := iface.Addrs()
		if e != nil {
			return state, e
		}
		for _, address := range addresses {
			prefix, e := netip.ParsePrefix(address.String())
			if e == nil && (tailnetIPv4.Contains(prefix.Addr()) || tailnetIPv6.Contains(prefix.Addr())) {
				state.Interfaces = append(state.Interfaces, iface.Name)
				break
			}
		}
	}
	if len(state.Interfaces) > 0 {
		for _, family := range []string{"inet", "inet6"} {
			data, e := systemCommand("/usr/sbin/netstat", "-rn", "-f", family)
			if e != nil {
				return state, e
			}
			state.Prefixes = append(state.Prefixes, tailscaleRoutePrefixes(string(data), state.Interfaces)...)
		}
	}
	sort.Strings(state.Interfaces)
	state.Prefixes = mergePrefixes(nil, state.Prefixes)
	return state, nil
}

func parseRoutePrefix(value string) (netip.Prefix, bool) {
	if value == "default" {
		return netip.Prefix{}, false
	}
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return netip.Prefix{}, false
	}
	address := strings.Split(parts[0], "%")[0]
	ipv4 := !strings.Contains(address, ":")
	bits := 128
	if ipv4 {
		octets := strings.Split(address, ".")
		if len(octets) > 4 {
			return netip.Prefix{}, false
		}
		bits = 32
		for len(octets) < 4 {
			octets = append(octets, "0")
		}
		address = strings.Join(octets, ".")
	}
	if len(parts) == 2 {
		var err error
		bits, err = strconv.Atoi(parts[1])
		if err != nil {
			return netip.Prefix{}, false
		}
	}
	prefix, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", address, bits))
	if err != nil || bits == 0 {
		return netip.Prefix{}, false
	}
	prefix = prefix.Masked()
	if prefix.Addr().IsMulticast() || prefix.Addr().IsLinkLocalUnicast() || prefix.Addr().IsLoopback() {
		return netip.Prefix{}, false
	}
	return prefix, true
}

func tailscaleRoutePrefixes(table string, interfaces []string) []netip.Prefix {
	allowed := map[string]bool{}
	for _, name := range interfaces {
		allowed[name] = true
	}
	var prefixes []netip.Prefix
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !allowed[fields[3]] {
			continue
		}
		if prefix, ok := parseRoutePrefix(fields[0]); ok {
			prefixes = append(prefixes, prefix)
		}
	}
	return mergePrefixes(nil, prefixes)
}

func mergePrefixes(previous, current []netip.Prefix) []netip.Prefix {
	unique := map[netip.Prefix]bool{}
	for _, p := range append(append([]netip.Prefix{}, previous...), current...) {
		unique[p.Masked()] = true
	}
	var result []netip.Prefix
	for p := range unique {
		covered := false
		for other := range unique {
			if p != other && other.Bits() <= p.Bits() && other.Contains(p.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func (r routeRequest) mode() string {
	if r.RoutingMode == "" {
		return modeWhitelist
	}
	return r.RoutingMode
}

func routingRules(uid uint32, apps []capturedApp, device string, request routeRequest, tailscale tailscaleBypass) string {
	if request.LiveRouting {
		return liveRoutingRules(uid, device, request, tailscale)
	}
	var rules strings.Builder
	captureUser := request.mode() != modeWhitelist
	selectors := []string{fmt.Sprintf("user %d", uid)}
	if !captureUser {
		selectors = nil
		for _, app := range apps {
			selectors = append(selectors, fmt.Sprintf("user %d group %d", uid, app.GID))
		}
	}
	// Direct applications bypass before any capture/block, including IPv6. A
	// non-tagged instance is separately REJECTed by the core's package rules.
	if request.mode() == modeGlobal {
		for _, app := range apps {
			for _, family := range []string{"inet", "inet6"} {
				fmt.Fprintf(&rules, "pass out quick on ! lo0 %s proto { tcp udp } from any to any user %d group %d flags any no state\n", family, uid, app.GID)
			}
		}
	}
	for _, selector := range selectors {
		for _, iface := range tailscale.Interfaces {
			for _, family := range []string{"inet", "inet6"} {
				fmt.Fprintf(&rules, "pass out quick on %s %s proto { tcp udp } from any to any %s flags any no state\n", iface, family, selector)
			}
		}
		// Keep learned subnet destinations away from the corporate VPN even if
		// Tailscale disconnects during this session. Never bypass every RFC1918 net.
		for _, prefix := range tailscale.Prefixes {
			family := "inet6"
			if prefix.Addr().Is4() {
				family = "inet"
			}
			fmt.Fprintf(&rules, "block return out quick on ! lo0 %s proto { tcp udp } from any to %s %s\n", family, prefix, selector)
		}
	}
	if !captureUser {
		rules.WriteString(captureRules(uid, apps, device, request.ProbeOnly, false))
		return rules.String()
	}
	// The already authenticated OpenConnect socket must keep its original path.
	gateway := netip.MustParseAddr(request.Gateway)
	family := "inet6"
	if gateway.Is4() {
		family = "inet"
	}
	fmt.Fprintf(&rules, "pass out quick on ! lo0 %s proto { tcp udp } from any to %s user %d flags any no state\n", family, gateway, uid)
	target := "any"
	if request.ProbeOnly {
		target = probeTarget
	}
	fmt.Fprintf(&rules, "pass out quick on ! lo0 route-to (%s %s) inet proto { tcp udp } from any to %s user %d flags any no state\n", device, captureAddress, target, uid)
	fmt.Fprintf(&rules, "block return out quick on ! lo0 inet proto { tcp udp } from any to %s user %d\n", target, uid)
	if !request.ProbeOnly {
		fmt.Fprintf(&rules, "block return out quick on ! lo0 inet6 proto { tcp udp } from any to any user %d\n", uid)
	}
	return rules.String()
}
