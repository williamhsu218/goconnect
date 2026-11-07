package company

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const (
	maxPolicyNetworks   = 32
	maxPolicyDomains    = 32
	maxPolicyDNSServers = 4
	maxDomainLength     = 253
	maxLabelLength      = 63
)

var (
	rfc1918Blocks = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}

	forbiddenTLDs = map[string]struct{}{
		"local":       {},
		"localhost":   {},
		"localdomain": {},
		"arpa":        {},
	}
)

// Policy defines the approved RFC1918 IPv4 networks, domain suffixes, and literal IPv4 DNS servers.
type Policy struct {
	Networks   []string
	Domains    []string
	DNSServers []string

	prefixes []netip.Prefix
	domains  []string
	dnsAddrs []netip.Addr
}

// NewPolicy validates and constructs an isolated company split-tunnel policy instance.
func NewPolicy(cfg Policy) (*Policy, error) {
	if len(cfg.Networks) == 0 || len(cfg.Networks) > maxPolicyNetworks {
		return nil, fmt.Errorf("company policy requires 1..%d canonical RFC1918 IPv4 CIDRs, got %d", maxPolicyNetworks, len(cfg.Networks))
	}
	if len(cfg.Domains) > maxPolicyDomains {
		return nil, fmt.Errorf("company policy allows at most %d domains, got %d", maxPolicyDomains, len(cfg.Domains))
	}
	if len(cfg.DNSServers) > maxPolicyDNSServers {
		return nil, fmt.Errorf("company policy allows at most %d literal IPv4 DNS servers, got %d", maxPolicyDNSServers, len(cfg.DNSServers))
	}

	prefixes := make([]netip.Prefix, 0, len(cfg.Networks))
	normNetworks := make([]string, 0, len(cfg.Networks))
	for _, raw := range cfg.Networks {
		if raw == "" || strings.TrimSpace(raw) != raw {
			return nil, fmt.Errorf("invalid network CIDR %q", raw)
		}
		pfx, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid network CIDR %q: %w", raw, err)
		}
		if !pfx.Addr().Is4() || pfx.Bits() < 1 || pfx.Bits() > 32 {
			return nil, fmt.Errorf("network CIDR %q must be IPv4 with prefix 1..32", raw)
		}
		if pfx != pfx.Masked() || pfx.String() != raw {
			return nil, fmt.Errorf("network CIDR %q must be canonical", raw)
		}
		if !isRFC1918Prefix(pfx) {
			return nil, fmt.Errorf("network CIDR %q is outside RFC1918 private IPv4 ranges", raw)
		}
		for _, existing := range prefixes {
			if existing.Overlaps(pfx) {
				return nil, fmt.Errorf("network CIDR %q duplicates or overlaps %q", raw, existing.String())
			}
		}
		prefixes = append(prefixes, pfx)
		normNetworks = append(normNetworks, pfx.String())
	}

	domains := make([]string, 0, len(cfg.Domains))
	for _, raw := range cfg.Domains {
		if raw == "" || strings.TrimSpace(raw) != raw {
			return nil, fmt.Errorf("invalid policy domain %q: must not be empty or whitespace-padded", raw)
		}
		if strings.ToLower(raw) != raw {
			return nil, fmt.Errorf("invalid policy domain %q: must be lowercase", raw)
		}
		trimmed := strings.TrimSuffix(raw, ".")
		if strings.HasSuffix(trimmed, ".") {
			return nil, fmt.Errorf("invalid policy domain %q: multiple trailing dots", raw)
		}
		d, err := normalizeAndValidateDomain(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid policy domain %q: %w", raw, err)
		}
		for _, existing := range domains {
			if existing == d || hasDomainSuffix(existing, d) || hasDomainSuffix(d, existing) {
				return nil, fmt.Errorf("policy domain %q duplicates or overlaps %q", d, existing)
			}
		}
		domains = append(domains, d)
	}

	dnsAddrs := make([]netip.Addr, 0, len(cfg.DNSServers))
	normDNS := make([]string, 0, len(cfg.DNSServers))
	for _, raw := range cfg.DNSServers {
		if raw == "" || strings.TrimSpace(raw) != raw {
			return nil, fmt.Errorf("invalid DNS server %q", raw)
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil || !addr.Is4() || addr.String() != raw {
			return nil, fmt.Errorf("DNS server %q must be a canonical literal IPv4 address", raw)
		}
		if addr.IsUnspecified() || addr.IsMulticast() || addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
			return nil, fmt.Errorf("DNS server %q is not a valid unicast IPv4 address", raw)
		}
		for _, existing := range dnsAddrs {
			if existing == addr {
				return nil, fmt.Errorf("duplicate DNS server %q", raw)
			}
		}
		dnsAddrs = append(dnsAddrs, addr)
		normDNS = append(normDNS, addr.String())
	}

	return &Policy{
		Networks:   normNetworks,
		Domains:    append([]string(nil), domains...),
		DNSServers: normDNS,
		prefixes:   prefixes,
		domains:    domains,
		dnsAddrs:   dnsAddrs,
	}, nil
}

// AllowsIP reports whether addr belongs to one of the policy's approved RFC1918 IPv4 networks.
// DNS server exceptions outside approved CIDRs are never authorized for general IP access.
func (p *Policy) AllowsIP(addr netip.Addr) bool {
	if p == nil || !addr.IsValid() || !addr.Is4() {
		return false
	}
	for _, pfx := range p.prefixes {
		if pfx.Contains(addr) {
			return true
		}
	}
	return false
}

// AllowsDomain reports whether host is within the policy's approved domain suffix scope.
// Case and a single trailing root dot are normalized at query time.
func (p *Policy) AllowsDomain(host string) bool {
	if p == nil || len(p.domains) == 0 {
		return false
	}
	norm, err := normalizeQueryHostname(host)
	if err != nil {
		return false
	}
	for _, domain := range p.domains {
		if hasDomainSuffix(norm, domain) {
			return true
		}
	}
	return false
}

func isRFC1918Prefix(pfx netip.Prefix) bool {
	for _, block := range rfc1918Blocks {
		if pfx.Bits() >= block.Bits() && block.Contains(pfx.Addr()) {
			return true
		}
	}
	return false
}

func hasDomainSuffix(host, domain string) bool {
	if host == domain {
		return true
	}
	if len(host) <= len(domain) {
		return false
	}
	prefixLen := len(host) - len(domain)
	return host[prefixLen-1] == '.' && host[prefixLen:] == domain
}

func normalizeQueryHostname(host string) (string, error) {
	if host == "" || strings.TrimSpace(host) != host {
		return "", errors.New("empty or whitespace-padded hostname")
	}
	trimmed := strings.TrimSuffix(host, ".")
	if strings.HasSuffix(trimmed, ".") {
		return "", errors.New("multiple trailing dots")
	}
	lower := strings.ToLower(trimmed)
	return normalizeAndValidateDomain(lower)
}

func normalizeAndValidateDomain(lower string) (string, error) {
	if len(lower) == 0 || len(lower) > maxDomainLength {
		return "", errors.New("domain length out of bounds")
	}
	if strings.Contains(lower, "*") {
		return "", errors.New("wildcards are not allowed")
	}
	if _, err := netip.ParseAddr(lower); err == nil {
		return "", errors.New("IP literals are not allowed as domains")
	}
	if strings.HasPrefix(lower, ".") || strings.HasSuffix(lower, ".") {
		return "", errors.New("leading or trailing dot not allowed")
	}

	labels := strings.Split(lower, ".")
	if len(labels) < 2 {
		return "", errors.New("single-label domains are not allowed")
	}

	for _, label := range labels {
		if len(label) == 0 || len(label) > maxLabelLength {
			return "", errors.New("invalid label length")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("label cannot start or end with hyphen")
		}
		for i := 0; i < len(label); i++ {
			ch := label[i]
			isLower := ch >= 'a' && ch <= 'z'
			isDigit := ch >= '0' && ch <= '9'
			if !isLower && !isDigit && ch != '-' {
				return "", errors.New("invalid character in domain label")
			}
		}
	}

	tld := labels[len(labels)-1]
	if _, forbidden := forbiddenTLDs[tld]; forbidden {
		return "", fmt.Errorf("reserved/local TLD %q is not allowed", tld)
	}
	allDigits := true
	for i := 0; i < len(tld); i++ {
		if tld[i] < '0' || tld[i] > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "", errors.New("numeric TLD is not allowed")
	}

	return lower, nil
}
