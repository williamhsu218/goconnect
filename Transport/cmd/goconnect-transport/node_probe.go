package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

var errNodeProbePath = errors.New("independent node test path unavailable")

// Only the temporary tester requests a physical interface. The normal session
// continues to use its existing gateway pin and system resolution.
func physicalNodeAddresses(ctx context.Context, host, iface string) ([]net.IP, error) {
	path, err := primaryPhysicalNetwork("")
	if err != nil || path.Interface != iface {
		return nil, errNodeProbePath
	}
	nic, err := net.InterfaceByName(iface)
	if err != nil || nic.Index <= 0 || nic.Flags&net.FlagUp == 0 {
		return nil, errNodeProbePath
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsGlobalUnicast() {
			return nil, errNodeProbePath
		}
		return []net.IP{ip}, nil
	}
	// The active session can own the system default resolver (including fake-IP).
	// Bootstrap only the node hostname with the DNS servers of the physical link.
	data, err := systemCommand("/usr/sbin/scutil", "--dns")
	if err != nil {
		return nil, err
	}
	resolvers, err := parseNativeResolvers(string(data))
	if err != nil {
		return nil, err
	}
	for _, server := range nodeProbeResolvers(resolvers, iface, host) {
		address := net.JoinHostPort(server.Addr().String(), strconv.Itoa(int(server.Port())))
		dialer := physicalSocketDialer(nic.Index, server.Addr().Is6())
		family := "4"
		if server.Addr().Is6() {
			family = "6"
		}
		resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network+family, address)
		}}
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		ips, lookupErr := resolver.LookupIP(attempt, "ip", strings.TrimSuffix(host, ".")+".")
		cancel()
		if lookupErr == nil {
			var valid []net.IP
			for _, ip := range ips {
				if ip.IsGlobalUnicast() {
					valid = append(valid, ip)
				}
			}
			if len(valid) > 0 {
				return valid, nil
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errNodeProbePath
}

// A matching supplemental domain belongs to its designated resolver. If it is
// on another interface, omit the sample instead of leaking it to default DNS.
func nodeProbeResolvers(resolvers []nativeResolver, iface, host string) []netip.AddrPort {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain := ""
	for _, r := range resolvers {
		if len(r.domain) > len(domain) && (host == r.domain || strings.HasSuffix(host, "."+r.domain)) {
			domain = r.domain
		}
	}
	var servers []netip.AddrPort
	seen := map[netip.AddrPort]bool{}
	for _, r := range resolvers {
		if r.domain != domain || r.iface != iface {
			continue
		}
		for _, addr := range r.servers {
			if addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() {
				continue
			}
			if addr.Is6() && addr.IsLinkLocalUnicast() {
				if addr.Zone() != "" && addr.Zone() != iface {
					continue
				}
				addr = addr.WithZone(iface)
			}
			server := netip.AddrPortFrom(addr, uint16(r.port))
			if !seen[server] {
				servers = append(servers, server)
				seen[server] = true
			}
		}
	}
	return servers
}
