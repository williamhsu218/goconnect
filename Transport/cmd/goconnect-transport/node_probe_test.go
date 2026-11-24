package main

import (
	"net/netip"
	"testing"
)

func TestNodeProbeOnlyUsesThePhysicalDomainsResolvers(t *testing.T) {
	public := netip.MustParseAddr("192.0.2.53")
	private := netip.MustParseAddr("198.51.100.53")
	resolvers := []nativeResolver{
		{port: 5353, servers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{iface: "en3", port: 53, servers: []netip.Addr{public}},
		{iface: "en3", scoped: true, port: 53, servers: []netip.Addr{public}},
		{iface: "utun1", domain: "company.test", port: 5353, servers: []netip.Addr{private}},
		{iface: "en3", domain: "lab.test", port: 5354, servers: []netip.Addr{private}},
	}
	for _, tc := range []struct{ host, want string }{
		{"node.example", "192.0.2.53:53"}, {"NODE.LAB.TEST.", "198.51.100.53:5354"}, {"node.company.test", ""},
	} {
		got := nodeProbeResolvers(resolvers, "en3", tc.host)
		if tc.want == "" && len(got) == 0 {
			continue
		}
		if len(got) != 1 || got[0].String() != tc.want {
			t.Errorf("%s: got %v", tc.host, got)
		}
	}
}

func TestCLIProbeAcceptsOnlyFixedServices(t *testing.T) {
	for _, host := range []string{"api.openai.com", "chatgpt.com", "api.anthropic.com", "cloudcode-pa.googleapis.com", "daily-cloudcode-pa.sandbox.googleapis.com"} {
		if cliProbeURL(host) == "" {
			t.Errorf("missing %s", host)
		}
	}
	for _, host := range []string{"", "127.0.0.1", "https://example.com", "api.openai.com/other"} {
		if cliProbeURL(host) != "" {
			t.Error("unexpected target", host)
		}
	}
}
