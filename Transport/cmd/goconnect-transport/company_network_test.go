package main

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestCompanyPlanBoundaries(t *testing.T) {
	local := []localNetworkRoute{{Interface: "en0", Prefix: netip.MustParsePrefix("192.168.31.0/24")}}
	ts := tailscaleBypass{Prefixes: []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("10.45.0.0/16")}}
	for _, input := range [][]string{nil, {"0.0.0.0/0"}, {"8.8.8.8"}, {"192.168.0.0/16"}, {"192.168.31.95"}, {"10.45.1.4"}, {"100.109.1.124"}} {
		if _, err := companyPlan(input, local, ts); err == nil {
			t.Errorf("accepted unsafe scope %v", input)
		}
	}
	got, err := companyPlan([]string{"192.168.14.0/24", "192.168.14.0/24", "fd12::5"}, local, ts)
	if err != nil || !reflect.DeepEqual(got, []string{"192.168.14.0/24", "fd12::5/128"}) {
		t.Fatalf("%v %v", got, err)
	}
}
func TestCompanyGatewayAndAddressFamilies(t *testing.T) {
	networks := []string{"192.168.14.0/24"}
	if validateCompanyGateway(networks, netip.MustParseAddr("192.168.14.1")) == nil {
		t.Fatal("allowed tunnel recursion")
	}
	if err := validateCompanyGateway(networks, netip.MustParseAddr("203.0.113.7")); err != nil {
		t.Fatal(err)
	}
	if validateCompanyFamilies(networks, []string{"fd12::1"}) == nil {
		t.Fatal("IPv4 route without IPv4 tunnel")
	}
	if validateCompanyFamilies([]string{"fd12::/64"}, []string{"10.0.0.2"}) == nil {
		t.Fatal("IPv6 route without IPv6 tunnel")
	}
	if err := validateCompanyFamilies(networks, []string{"10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedCompanyConfigUsesExistingSessionOnly(t *testing.T) {
	r := routeRequest{CompanyShared: true, SOCKSPort: 19222, Token: strings.Repeat("x", 40), OwnerPID: 501, RemoteNetworks: []string{"192.168.14.70"}, Gateway: "203.0.113.5", VPNAddresses: []string{"10.0.0.2"}}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	cfg := makeSharedCompanyConfig(r, "utun987", 19888)
	tun := cfg["tun"].(map[string]any)
	if tun["auto-route"] != false || tun["auto-detect-interface"] != false || cfg["find-process-mode"] != "off" {
		t.Fatal("unexpected automatic capture")
	}
	proxies := cfg["proxies"].([]map[string]any)
	if len(proxies) != 1 || proxies[0]["server"] != "127.0.0.1" || proxies[0]["port"] != r.SOCKSPort {
		t.Fatal("second upstream session")
	}
	rules := cfg["rules"].([]string)
	if rules[len(rules)-1] != "MATCH,REJECT" {
		t.Fatal("unscoped traffic accepted")
	}
	r.CompanyShared = false
	r.AppPaths = nil
	if r.validate() == nil {
		t.Fatal("legacy request accepted without explicit capability")
	}
}
