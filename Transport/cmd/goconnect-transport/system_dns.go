package main

import (
	"errors"
	"net/netip"
	"sort"
	"strings"
)

func discoverSystemDNS() ([]netip.Addr, error) {
	data, err := systemCommand("/usr/sbin/scutil", "--dns")
	if err != nil {
		return nil, err
	}
	return parseSystemDNS(string(data))
}

// Read only nameserver addresses, not resolver domains or shell fragments.
// Zones are supplied by the original socket route; these addresses are used
// solely for narrow destination-port-53 PF exceptions.
func parseSystemDNS(data string) ([]netip.Addr, error) {
	unique := map[netip.Addr]bool{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.HasPrefix(fields[0], "nameserver[") || !strings.HasSuffix(fields[0], "]") || fields[1] != ":" {
			continue
		}
		address, err := netip.ParseAddr(fields[2])
		if err != nil {
			return nil, errors.New("invalid system DNS address")
		}
		address = address.WithZone("").Unmap()
		if address.IsUnspecified() || address.IsMulticast() {
			return nil, errors.New("invalid system DNS address")
		}
		if !address.IsLoopback() {
			unique[address] = true
		}
	}
	if len(unique) > 64 {
		return nil, errors.New("too many system resolvers")
	}
	result := make([]netip.Addr, 0, len(unique))
	for address := range unique {
		result = append(result, address)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Less(result[j]) })
	return result, nil
}
