package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const systemCaptureAddress = "198.19.253.1"
const systemCaptureIPv6 = "fdfe:dcba:9875::1"
const systemInbound = "goconnect-system"

var privateNetworks = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("fc00::/7")}

// System capture is restricted to private unicast networks. Public system
// traffic, other login users, loopback and link-local traffic stay untouched.
func remotePrefix(value string) (netip.Prefix, bool) {
	p, ok := directIPPrefix(value)
	if !ok {
		return p, false
	}
	for _, base := range privateNetworks {
		if base.Addr().BitLen() == p.Addr().BitLen() && p.Bits() >= base.Bits() && base.Contains(p.Addr()) {
			return p, true
		}
	}
	return p, false
}

// Read the standard vpnc-script environment. Never interpret a malformed or
// excessive split list as permission to capture all private destinations.
func advertisedRemoteNetworks(env func(string) string) (include, exclude []string, err error) {
	for _, family := range []struct {
		key, address string
		v4           bool
	}{{"CISCO_SPLIT", "INTERNAL_IP4_ADDRESS", true}, {"CISCO_IPV6_SPLIT", "INTERNAL_IP6_ADDRESS", false}} {
		if env(family.address) == "" {
			continue
		}
		for _, direction := range []string{"INC", "EXC"} {
			key := family.key + "_" + direction
			count := 0
			if raw := env(key); raw != "" {
				count, err = strconv.Atoi(raw)
				if err != nil || count < 0 || count > 256 {
					return nil, nil, errors.New("invalid VPN split routes")
				}
			}
			// With no split include list OpenConnect is a full tunnel, scoped here to
			// private networks only; locally discovered routes always take precedence.
			if direction == "INC" && count == 0 {
				for _, p := range privateNetworks {
					if p.Addr().Is4() == family.v4 {
						include = append(include, p.String())
					}
				}
			}
			for i := 0; i < count; i++ {
				base := fmt.Sprintf("%s_%d", key, i)
				// Port/protocol-scoped includes cannot safely be widened into
				// an all-TCP/UDP CIDR rule. Leave those to ordinary App routing.
				if direction == "INC" {
					restricted := false
					for _, suffix := range []string{"_PROTOCOL", "_SRCPORT", "_DSTPORT"} {
						value := env(base + suffix)
						if value != "" && value != "0" {
							restricted = true
						}
					}
					if restricted {
						continue
					}
				}
				a, e := netip.ParseAddr(env(base + "_ADDR"))
				bits, e2 := strconv.Atoi(env(base + "_MASKLEN"))
				if e2 != nil && family.v4 {
					mask := net.ParseIP(env(base + "_MASK")).To4()
					if mask != nil {
						var size int
						bits, size = net.IPMask(mask).Size()
						if size == 32 {
							e2 = nil
						}
					}
				}
				if e != nil || e2 != nil || a.Is4() != family.v4 || bits < 0 || bits > a.BitLen() {
					return nil, nil, errors.New("invalid VPN split prefix")
				}
				p := netip.PrefixFrom(a, bits).Masked()
				for _, private := range privateNetworks {
					if p.Addr().BitLen() != private.Addr().BitLen() {
						continue
					}
					if !p.Contains(private.Addr()) && !private.Contains(p.Addr()) {
						continue
					}
					selected := p
					if p.Bits() < private.Bits() && p.Contains(private.Addr()) {
						selected = private
					}
					if _, ok := remotePrefix(selected.String()); !ok {
						continue
					}
					if direction == "INC" {
						include = append(include, selected.String())
					} else {
						exclude = append(exclude, selected.String())
					}
				}
			}
		}
	}
	return include, exclude, nil
}

func (r routeRequest) hasSystemCapture() bool {
	return r.LiveRouting && !r.ProbeOnly && len(r.RemoteNetworks) > 0
}

// The capture lock serializes workers. Use an unused numeric GID without
// creating an OpenDirectory group or adding any user to a privileged group.
func unusedCoreGID() (uint32, error) {
	data, err := systemCommand("/usr/bin/dscl", ".", "-list", "/Groups", "PrimaryGroupID")
	if err != nil {
		return 0, err
	}
	used, err := processGroupIDs()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			if n, e := strconv.ParseUint(f[1], 10, 32); e == nil {
				used[uint32(n)] = true
			}
		}
	}
	for n := uint32(60000); n < 61000; n++ {
		if !used[n] {
			return n, nil
		}
	}
	return 0, errors.New("没有可用的网络核心隔离标记")
}

// This entire chain is before current-user capture. A separate TUN gives
// kernel sockets an explicit policy without weakening unknown-app rejection.
func systemRoutingRules(r routeRequest, ts tailscaleBypass) string {
	if !r.hasSystemCapture() || r.systemDevice == "" || r.coreGID == 0 {
		return ""
	}
	var b strings.Builder
	for _, family := range []string{"inet", "inet6"} {
		fmt.Fprintf(&b, "pass out quick on ! lo0 %s proto { tcp udp } from any to any user 0 group %d flags any no state\n", family, r.coreGID)
		for _, iface := range ts.Interfaces {
			fmt.Fprintf(&b, "pass out quick on %s %s proto { tcp udp } from any to any user < 500 flags any no state\n", iface, family)
		}
	}
	bypass := func(value, iface, extra string, block bool) {
		p, ok := directIPPrefix(value)
		if !ok {
			return
		}
		family := "inet6"
		if p.Addr().Is4() {
			family = "inet"
		}
		if block {
			fmt.Fprintf(&b, "block return out quick on ! lo0 %s proto { tcp udp } from any to %s user < 500\n", family, p)
			return
		}
		fmt.Fprintf(&b, "pass out quick on %s %s proto { tcp udp } from any to %s %s user < 500 flags any no state\n", iface, family, p, extra)
	}
	for _, p := range ts.Prefixes {
		bypass(p.String(), "", "", true)
	}
	bypass(r.Gateway, "! lo0", "", false)
	for _, route := range r.systemLAN {
		bypass(route.Prefix.String(), route.Interface, "", false)
	}
	for _, dns := range r.systemDNS {
		bypass(dns.String(), "! lo0", "port 53", false)
	}
	for _, rule := range r.DirectDomains {
		bypass(rule.Domain, "! lo0", "", false)
	}
	for _, excluded := range r.RemoteExcluded {
		bypass(excluded, "! lo0", "", false)
	}
	for _, value := range r.RemoteNetworks {
		p, ok := remotePrefix(value)
		if !ok {
			continue
		}
		family, address := "inet6", systemCaptureIPv6
		if p.Addr().Is4() {
			family, address = "inet", systemCaptureAddress
		}
		fmt.Fprintf(&b, "pass out quick on ! lo0 route-to (%s %s) %s proto { tcp udp } from any to %s user < 500 flags any no state\n", r.systemDevice, address, family, p)
		fmt.Fprintf(&b, "block return out quick on ! lo0 %s proto { tcp udp } from any to %s user < 500\n", family, p)
	}
	return b.String()
}

func addSystemListener(config map[string]any, r routeRequest) {
	if !r.hasSystemCapture() {
		return
	}
	rules := directDomainRules(r.DirectDomains)
	rules = append(rules, "MATCH,GoConnect")
	config["sub-rules"] = map[string]any{systemInbound: rules}
	config["listeners"] = []map[string]any{{"name": systemInbound, "type": "tun", "device": r.systemDevice,
		"rule": systemInbound, "stack": "gvisor", "auto-route": false, "auto-detect-interface": false,
		"mtu": 1400, "dns-hijack": []string{}, "inet4-address": []string{systemCaptureAddress + "/30"}, "inet6-address": []string{systemCaptureIPv6 + "/126"}}}
}

func chooseOtherDevice(first string) (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	used := map[string]bool{first: true}
	for _, iface := range interfaces {
		used[iface.Name] = true
	}
	for n := 100; n < 1000; n++ {
		name := fmt.Sprintf("utun%d", n)
		if !used[name] {
			return name, nil
		}
	}
	return "", errors.New("no free system capture interface")
}
func systemDeviceReady(r routeRequest) bool {
	if !r.hasSystemCapture() {
		return true
	}
	iface, err := net.InterfaceByName(r.systemDevice)
	return err == nil && iface.Flags&net.FlagUp != 0
}
