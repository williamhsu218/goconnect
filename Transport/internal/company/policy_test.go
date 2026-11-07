package company

import (
	"fmt"
	"net/netip"
	"testing"
)

func TestNewPolicyValidation(t *testing.T) {
	t.Parallel()

	valid, err := NewPolicy(Policy{
		Networks:   []string{"10.10.0.0/16", "172.16.5.0/24", "192.168.100.10/32"},
		Domains:    []string{"corp.example.com", "internal.acme.org"},
		DNSServers: []string{"10.10.0.53", "198.51.100.53"},
	})
	if err != nil {
		t.Fatalf("expected valid policy, got error: %v", err)
	}
	if len(valid.Networks) != 3 || len(valid.Domains) != 2 || len(valid.DNSServers) != 2 {
		t.Fatalf("unexpected policy counts: %+v", valid)
	}

	var manyNetworks []string
	for i := 0; i < 33; i++ {
		manyNetworks = append(manyNetworks, fmt.Sprintf("10.0.%d.0/24", i))
	}

	var manyDomains []string
	for i := 0; i < 33; i++ {
		manyDomains = append(manyDomains, fmt.Sprintf("d%d.corp.example.com", i))
	}

	invalidCases := []struct {
		name string
		cfg  Policy
	}{
		{
			name: "empty networks no auto private fallback",
			cfg:  Policy{Domains: []string{"corp.example.com"}, DNSServers: []string{"10.0.0.53"}},
		},
		{
			name: "too many networks",
			cfg:  Policy{Networks: manyNetworks, Domains: []string{"corp.example.com"}, DNSServers: []string{"10.0.0.53"}},
		},
		{
			name: "non-canonical CIDR host bits set",
			cfg:  Policy{Networks: []string{"10.0.0.1/24"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "public IPv4 CIDR rejected",
			cfg:  Policy{Networks: []string{"8.8.8.0/24"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "cgnat CIDR rejected",
			cfg:  Policy{Networks: []string{"100.64.0.0/10"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "loopback CIDR rejected",
			cfg:  Policy{Networks: []string{"127.0.0.0/8"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "ipv6 CIDR rejected",
			cfg:  Policy{Networks: []string{"fd00::/8"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "duplicate CIDR rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24", "10.0.0.0/24"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "overlapping CIDR rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/16", "10.0.1.0/24"}, Domains: []string{"corp.example.com"}},
		},
		{
			name: "single label domain rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"corp"}},
		},
		{
			name: "local TLD rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"printer.local"}},
		},
		{
			name: "localhost TLD rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"app.localhost"}},
		},
		{
			name: "public IP as domain rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"8.8.8.8"}},
		},
		{
			name: "private IP as domain rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"10.0.0.1"}},
		},
		{
			name: "wildcard domain rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"*.corp.example.com"}},
		},
		{
			name: "uppercase domain in config rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"Corp.Example.com"}},
		},
		{
			name: "duplicate domain rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"corp.example.com", "corp.example.com"}},
		},
		{
			name: "overlapping domain suffix rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: []string{"example.com", "corp.example.com"}},
		},
		{
			name: "too many domains rejected",
			cfg:  Policy{Networks: []string{"10.0.0.0/24"}, Domains: manyDomains},
		},
		{
			name: "too many DNS servers rejected",
			cfg: Policy{
				Networks:   []string{"10.0.0.0/24"},
				Domains:    []string{"corp.example.com"},
				DNSServers: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"},
			},
		},
		{
			name: "duplicate DNS server rejected",
			cfg: Policy{
				Networks:   []string{"10.0.0.0/24"},
				Domains:    []string{"corp.example.com"},
				DNSServers: []string{"10.0.0.53", "10.0.0.53"},
			},
		},
		{
			name: "hostname DNS server rejected",
			cfg: Policy{
				Networks:   []string{"10.0.0.0/24"},
				Domains:    []string{"corp.example.com"},
				DNSServers: []string{"dns.corp.example.com"},
			},
		},
		{
			name: "ipv6 DNS server rejected",
			cfg: Policy{
				Networks:   []string{"10.0.0.0/24"},
				Domains:    []string{"corp.example.com"},
				DNSServers: []string{"2001:4860:4860::8888"},
			},
		},
		{
			name: "unspecified DNS server rejected",
			cfg: Policy{
				Networks:   []string{"10.0.0.0/24"},
				Domains:    []string{"corp.example.com"},
				DNSServers: []string{"0.0.0.0"},
			},
		},
	}

	for _, tc := range invalidCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPolicy(tc.cfg); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestAllowsIPAndDNSExceptionBoundary(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(Policy{
		Networks:   []string{"10.20.0.0/16", "192.168.50.10/32"},
		Domains:    []string{"corp.example.com"},
		DNSServers: []string{"198.51.100.53", "10.99.0.53"},
	})
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}

	if !p.AllowsIP(netip.MustParseAddr("10.20.1.5")) {
		t.Fatal("expected 10.20.1.5 to be allowed")
	}
	if !p.AllowsIP(netip.MustParseAddr("192.168.50.10")) {
		t.Fatal("expected 192.168.50.10/32 to be allowed")
	}

	// Unapproved RFC1918 and public IPs must be denied (no all-private or public fallback).
	if p.AllowsIP(netip.MustParseAddr("10.21.0.1")) {
		t.Fatal("expected unapproved RFC1918 10.21.0.1 to be denied")
	}
	if p.AllowsIP(netip.MustParseAddr("172.16.0.1")) {
		t.Fatal("expected unapproved RFC1918 172.16.0.1 to be denied")
	}
	if p.AllowsIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("expected public IP 8.8.8.8 to be denied")
	}
	if p.AllowsIP(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("expected IPv6 address to be denied")
	}

	// DNS server outside approved CIDRs is only authorized for TCP/53 DNS queries, NEVER business IP access.
	if p.AllowsIP(netip.MustParseAddr("198.51.100.53")) {
		t.Fatal("external DNS server 198.51.100.53 must not be allowed for general IP access")
	}
	if p.AllowsIP(netip.MustParseAddr("10.99.0.53")) {
		t.Fatal("outside-subnet DNS server 10.99.0.53 must not be allowed for general IP access")
	}
}

func TestAllowsDomainSuffixAndNormalization(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(Policy{
		Networks:   []string{"10.0.0.0/24"},
		Domains:    []string{"corp.example.com", "svc.internal.net"},
		DNSServers: []string{"10.0.0.53"},
	})
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}

	allowed := []string{
		"corp.example.com",
		"corp.example.com.",
		"CORP.EXAMPLE.COM.",
		"api.corp.example.com",
		"deep.sub.Api.Corp.Example.Com.",
		"svc.internal.net",
		"db.svc.internal.net.",
	}
	for _, host := range allowed {
		if !p.AllowsDomain(host) {
			t.Errorf("expected domain %q to be allowed", host)
		}
	}

	denied := []string{
		"",
		"example.com",
		"badcorp.example.com",
		"corp.example.com..",
		".corp.example.com",
		"*.corp.example.com",
		"corp.example.com.evil.com",
		"10.0.0.1",
		"localhost",
		"host.local",
	}
	for _, host := range denied {
		if p.AllowsDomain(host) {
			t.Errorf("expected domain %q to be denied", host)
		}
	}
}
