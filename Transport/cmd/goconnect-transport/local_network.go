package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Discover attached networks and explicit private routes via local gateways.
// A private address alone never
// implies DIRECT: corporate private destinations still follow VPN policy.
type localNetworkRoute struct {
	Interface string
	Prefix    netip.Prefix
}

var physicalInterface = regexp.MustCompile(`^(en|bridge|bond|vnic|vmnet)[0-9]{1,5}$`)

func discoverLocalNetworks() ([]localNetworkRoute, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []localNetworkRoute
	for _, iface := range interfaces {
		if !physicalInterface.MatchString(iface.Name) || iface.Flags&net.FlagUp == 0 || iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		addresses, e := iface.Addrs()
		if e != nil {
			return nil, e
		}
		for _, address := range addresses {
			p, e := netip.ParsePrefix(address.String())
			if e != nil {
				continue
			}
			if r, ok := localAttachedRoute(iface.Name, p); ok {
				result = append(result, r)
			}
		}
	}
	// The active IPv4 gateway can be outside an interface's assigned prefix.
	data, err := systemCommand("/sbin/route", "-n", "get", "default")
	if err != nil {
		return nil, err
	}
	if gateway, ok := localGatewayRoute(string(data), result); ok {
		result = append(result, gateway)
	}
	attached := append([]localNetworkRoute(nil), result...)
	for _, family := range []string{"inet", "inet6"} {
		table, e := systemCommand("/usr/sbin/netstat", "-rn", "-f", family)
		if e != nil {
			return nil, e
		}
		result = append(result, localRoutedNetworks(string(table), attached)...)
	}
	unique := map[localNetworkRoute]bool{}
	for _, r := range result {
		unique[r] = true
	}
	if len(unique) > 128 {
		return nil, errors.New("too many directly attached networks")
	}
	result = nil
	for r := range unique {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]) < fmt.Sprint(result[j]) })
	return result, nil
}

func localAttachedRoute(iface string, p netip.Prefix) (localNetworkRoute, bool) {
	if !physicalInterface.MatchString(iface) || !p.IsValid() || p.Bits() == 0 || p.Addr().IsUnspecified() || p.Addr().IsMulticast() || p.Addr().IsLoopback() {
		return localNetworkRoute{}, false
	}
	return localNetworkRoute{iface, p.Masked()}, true
}

func localGatewayRoute(output string, attached []localNetworkRoute) (localNetworkRoute, bool) {
	var iface, gateway string
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if f[0] == "interface:" {
			iface = f[1]
		}
		if f[0] == "gateway:" {
			gateway = f[1]
		}
	}
	a, err := netip.ParseAddr(gateway)
	if err != nil {
		return localNetworkRoute{}, false
	}
	for _, r := range attached {
		if r.Interface == iface {
			return localAttachedRoute(iface, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return localNetworkRoute{}, false
}

// Explicit private routes via an on-link gateway are distinguishable from
// the default route. A remote VLAN reachable only via the default gateway
// cannot be inferred safely and must use a per-line IP/CIDR exception.
func localRoutedNetworks(table string, attached []localNetworkRoute) []localNetworkRoute {
	var result []localNetworkRoute
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] == "default" || !strings.Contains(f[2], "G") || strings.ContainsAny(f[2], "BR") {
			continue
		}
		gateway, err := netip.ParseAddr(f[1])
		if err != nil {
			continue
		}
		destination := f[0]
		if !strings.ContainsAny(destination, "/:") && !strings.Contains(f[2], "H") {
			count := len(strings.Split(destination, "."))
			if count < 4 {
				destination = destination + "/" + fmt.Sprint(count*8)
			}
		}
		prefix, ok := parseRoutePrefix(destination)
		if !ok || !entirelyPrivatePrefix(prefix) {
			continue
		}
		for _, local := range attached {
			if local.Interface == f[3] && local.Prefix.Contains(gateway) {
				if route, ok := localAttachedRoute(f[3], prefix); ok {
					result = append(result, route)
				}
				break
			}
		}
	}
	return result
}

// Check the whole network, not just its first address (10.0.0.0/7 also
// contains public 11/8 and must never become an inferred direct exception).
func entirelyPrivatePrefix(p netip.Prefix) bool {
	for _, network := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"} {
		scope := netip.MustParsePrefix(network)
		if p.Bits() >= scope.Bits() && scope.Contains(p.Addr()) {
			return true
		}
	}
	return false
}
