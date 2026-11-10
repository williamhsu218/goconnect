package main

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func fakeIPConflictRequest() routeRequest {
	return routeRequest{
		TransparentRouting: true, LiveRouting: true, RoutingMode: modeWhitelist,
		Gateway:    "203.0.113.20",
		managedDNS: &managedDNSPlan{port: 15353, resolvers: []nativeResolver{{servers: []netip.Addr{netip.MustParseAddr("192.168.1.1")}, port: 53}}},
	}
}

func TestManagedDNSFakeIPActivationMatchesConflictValidation(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		probe, dns bool
		fake       bool
	}{
		{"whitelist", modeWhitelist, false, true, true},
		{"default-whitelist", "", false, true, true},
		{"global", modeGlobal, false, true, false},
		{"whitelist-probe", modeWhitelist, true, true, false},
		{"global-probe", modeGlobal, true, true, false},
		{"no-managed-dns", modeWhitelist, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fakeIPConflictRequest()
			r.RoutingMode, r.ProbeOnly = tc.mode, tc.probe
			if !tc.dns {
				r.managedDNS = nil
			}
			cfg := map[string]any{}
			configureManagedDNS(cfg, r, originalNetwork{Interface: "en0"})
			dns, _ := cfg["dns"].(map[string]any)
			if (dns["enhanced-mode"] == "fake-ip") != tc.fake || usesManagedDNSFakeIP(r) != tc.fake {
				t.Fatal("fake-IP activation and preflight disagree", dns)
			}
			if tc.fake && dns["fake-ip-range"] != managedDNSFakeIPRange {
				t.Fatal("configuration no longer uses the validated pool", dns)
			}
			err := validateTransparentAddresses(r, transparentAddressSpace{interfaces: []netip.Prefix{netip.MustParsePrefix("198.18.17.0/24")}})
			if (err != nil) != tc.fake || errors.Is(err, errManagedDNSFakeIPConflict) != tc.fake {
				t.Fatal("inactive fake-IP mode gained a restriction, or active mode lost it", err)
			}
		})
	}
}

func TestTransparentFakeIPInterfaceOverlapBoundaries(t *testing.T) {
	for _, tc := range []struct {
		prefix   string
		conflict bool
	}{
		{"198.18.0.0/16", true},
		{"198.18.0.1/16", true}, // Interface addresses may retain host bits.
		{"198.18.128.0/17", true},
		{"198.18.17.2/24", true},
		{"198.18.0.0/32", true},
		{"198.18.255.255/32", true},
		{"198.18.0.0/15", true}, // Also overlaps the original managed TUN pool.
		{"198.17.255.255/32", false},
		{"198.19.0.0/32", false},
		{"198.19.0.0/24", false},
		{"10.0.0.0/8", false},
		{"2001:db8::/32", false},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			err := validateTransparentAddresses(fakeIPConflictRequest(), transparentAddressSpace{interfaces: []netip.Prefix{netip.MustParsePrefix(tc.prefix)}})
			if (err != nil) != tc.conflict {
				t.Fatal("wrong overlap result", err)
			}
		})
	}
}

// These are already-discovered/configured destinations, not new route or DNS
// discovery. Tests deliberately inject them rather than touching host state.
func fakeIPReservedPathCases() []struct {
	name string
	set  func(*routeRequest, *transparentAddressSpace)
} {
	return []struct {
		name string
		set  func(*routeRequest, *transparentAddressSpace)
	}{
		{"local", func(_ *routeRequest, s *transparentAddressSpace) {
			s.local = []localNetworkRoute{{Interface: "vmnet1", Prefix: netip.MustParsePrefix("198.18.0.0/24")}}
		}},
		{"tailscale", func(_ *routeRequest, s *transparentAddressSpace) {
			s.tailscale.Prefixes = []netip.Prefix{netip.MustParsePrefix("198.18.20.0/24")}
		}},
		{"direct-ip", func(r *routeRequest, _ *transparentAddressSpace) {
			r.DirectDomains = []domainRule{{Domain: "198.18.20.0/24"}}
		}},
		{"primary-gateway", func(r *routeRequest, _ *transparentAddressSpace) { r.Gateway = "198.18.0.2" }},
		{"physical-gateway", func(_ *routeRequest, s *transparentAddressSpace) { s.original.Gateway = "198.18.0.3" }},
		{"outer-gateway", func(_ *routeRequest, s *transparentAddressSpace) { s.outer.Gateway = "198.18.0.4" }},
		{"default-dns", func(r *routeRequest, _ *transparentAddressSpace) {
			r.managedDNS.resolvers[0].servers = []netip.Addr{netip.MustParseAddr("198.18.0.53")}
		}},
		{"supplemental-dns", func(r *routeRequest, _ *transparentAddressSpace) {
			r.managedDNS.resolvers = append(r.managedDNS.resolvers, nativeResolver{domain: "corp.example", servers: []netip.Addr{netip.MustParseAddr("198.18.0.53")}})
		}},
		{"ipv4-mapped-dns", func(r *routeRequest, _ *transparentAddressSpace) {
			r.managedDNS.resolvers[0].servers = []netip.Addr{netip.MustParseAddr("::ffff:198.18.0.53")}
		}},
		{"company-network", func(r *routeRequest, _ *transparentAddressSpace) {
			// Current company policy independently restricts scopes to private
			// networks; still validate the supplied reservation against the pool.
			r.Company = &companyReservation{Networks: []string{"198.18.0.0/16"}}
		}},
		{"company-gateway", func(r *routeRequest, _ *transparentAddressSpace) {
			r.Company = &companyReservation{GatewayIPs: []string{"198.18.0.10"}}
		}},
		{"company-dns", func(r *routeRequest, _ *transparentAddressSpace) {
			r.Company = &companyReservation{DNSServers: []string{"198.18.0.53"}}
		}},
	}
}

func TestTransparentFakeIPReservedPathsAndModeScope(t *testing.T) {
	for _, tc := range fakeIPReservedPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			r, space := fakeIPConflictRequest(), transparentAddressSpace{}
			tc.set(&r, &space)
			err := validateTransparentAddresses(r, space)
			if !errors.Is(err, errManagedDNSFakeIPConflict) || !strings.Contains(err.Error(), "198.18.0.0/16") {
				t.Fatal("reserved path inside the active pool was not clearly rejected", err)
			}
			for _, mode := range []string{"global", "probe", "no-dns"} {
				other := r
				switch mode {
				case "global":
					other.RoutingMode = modeGlobal
				case "probe":
					other.ProbeOnly = true
				case "no-dns":
					other.managedDNS = nil
				}
				if err := validateTransparentAddresses(other, space); err != nil {
					t.Fatal(mode, "gained a fake-IP restriction", err)
				}
			}
		})
	}
}

func TestTransparentManagedTUNAddressChecksRemainActive(t *testing.T) {
	for _, mode := range []string{"whitelist", "global", "probe", "no-dns"} {
		for _, source := range []string{"interface-v4", "interface-v6", "remote-v6", "invalid-remote"} {
			t.Run(mode+"/"+source, func(t *testing.T) {
				r, space := fakeIPConflictRequest(), transparentAddressSpace{}
				switch mode {
				case "global":
					r.RoutingMode = modeGlobal
				case "probe":
					r.ProbeOnly = true
				case "no-dns":
					r.managedDNS = nil
				}
				switch source {
				case "interface-v4":
					space.interfaces = []netip.Prefix{netip.MustParsePrefix(systemCaptureAddress + "/32")}
				case "interface-v6":
					space.interfaces = []netip.Prefix{netip.MustParsePrefix(systemCaptureIPv6 + "/128")}
				case "remote-v6":
					r.RemoteNetworks = []string{"fc00::/7"}
				case "invalid-remote":
					r.RemoteNetworks = []string{"not-a-prefix"}
				}
				if err := validateTransparentAddresses(r, space); err == nil || errors.Is(err, errManagedDNSFakeIPConflict) {
					t.Fatal("original TUN/address validation was lost", err)
				}
			})
		}
	}
}

func TestTransparentFakeIPPreflightCannotEnterSessionStart(t *testing.T) {
	cases := fakeIPReservedPathCases()
	cases = append(cases, struct {
		name string
		set  func(*routeRequest, *transparentAddressSpace)
	}{"interface", func(_ *routeRequest, s *transparentAddressSpace) {
		s.interfaces = []netip.Prefix{netip.MustParsePrefix("198.18.0.0/16")}
	}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, space := fakeIPConflictRequest(), transparentAddressSpace{}
			tc.set(&r, &space)
			before := append([]netip.Prefix(nil), space.interfaces...)
			reads, starts := 0, 0
			err := withValidatedTransparentAddresses(r, space, func() ([]netip.Prefix, error) {
				reads++
				return space.interfaces, nil
			}, func() ([]netip.Prefix, error) { return nil, nil }, func() error {
				starts++ // Production places chooseDevice/core/routes/DNS here.
				return nil
			})
			if !errors.Is(err, errManagedDNSFakeIPConflict) || reads != 1 || starts != 0 {
				t.Fatal("failed preflight entered new-session operations", err, reads, starts)
			}
			if !reflect.DeepEqual(before, space.interfaces) {
				t.Fatal("preflight modified its interface snapshot")
			}
		})
	}
	t.Run("discovery-error", func(t *testing.T) {
		want := errors.New("interface snapshot failed")
		err := withValidatedTransparentAddresses(fakeIPConflictRequest(), transparentAddressSpace{}, func() ([]netip.Prefix, error) {
			return nil, want
		}, func() ([]netip.Prefix, error) { t.Fatal("read routes despite snapshot error"); return nil, nil }, func() error { t.Fatal("started despite snapshot error"); return nil })
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
	})
	t.Run("safe-start-and-error-propagation", func(t *testing.T) {
		r := fakeIPConflictRequest()
		r.RemoteNetworks = []string{"10.20.0.0/16"}
		space := transparentAddressSpace{
			local:     []localNetworkRoute{{Interface: "en0", Prefix: netip.MustParsePrefix("192.168.1.0/24")}},
			tailscale: tailscaleBypass{Prefixes: []netip.Prefix{tailnetIPv4, tailnetIPv6}},
			original:  originalNetwork{Interface: "en0", Gateway: "192.168.1.1"},
		}
		starts, want := 0, errors.New("synthetic start result")
		err := withValidatedTransparentAddresses(r, space, func() ([]netip.Prefix, error) {
			return []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")}, nil
		}, func() ([]netip.Prefix, error) { return nil, nil }, func() error { starts++; return want })
		if !errors.Is(err, want) || starts != 1 {
			t.Fatal("safe startup or its error was swallowed", err, starts)
		}
	})
}

func TestTransparentFakeIPExistingKernelRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, entry string
		conflict    bool
	}{
		{"static-non-private", "198.18.42/24 192.0.2.1 UGSc en0", true},
		{"other-tunnel", "198.18.42/24 10.0.0.1 UGSc utun12", true},
		{"interface-route", "198.18.42/24 link#20 UCS utun12", true},
		{"cached-host", "198.18.42.7 aa:bb:cc:dd:ee:ff UHLWI en0 1190", true},
		{"reject", "198.18.42/24 127.0.0.1 UGRS lo0", true},
		{"blackhole", "198.18.42/24 127.0.0.1 UGBS lo0", true},
		{"covering-prefix", "198.18/15 192.0.2.1 UGSc en0", true},
		{"upper-bound", "198.18.255.255 192.0.2.1 UGHS en0", true},
		{"adjacent-prefix", "198.19/16 192.0.2.1 UGSc en0", false},
		{"private-route", "10.42/24 192.0.2.1 UGSc en0", false},
		{"explicit-default", "0.0.0.0/0 192.0.2.1 UGScg en0", false},
		{"default", "default 192.0.2.1 UGScg en0", false},
		{"ipv6", "2001:db8::/32 fe80::1 UGSc en0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attached := []localNetworkRoute{{Interface: "en0", Prefix: netip.MustParsePrefix("192.0.2.0/24")}}
			table := "Routing tables\nInternet:\nDestination Gateway Flags Netif Expire\ndefault 192.0.2.1 UGScg en0\n" + tc.entry + "\n"
			space := transparentAddressSpace{local: append(attached, localRoutedNetworks(table, attached)...)}
			if tc.name == "static-non-private" && len(space.local) != 1 {
				t.Fatal("fixture must expose the non-RFC1918 gap in DIRECT discovery")
			}
			reads, starts := 0, 0
			err := withValidatedTransparentAddresses(fakeIPConflictRequest(), space, func() ([]netip.Prefix, error) {
				return []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24")}, nil
			}, func() ([]netip.Prefix, error) {
				reads++
				return transparentIPv4RoutePrefixes(table), nil
			}, func() error { starts++; return nil })
			if errors.Is(err, errManagedDNSFakeIPConflict) != tc.conflict || reads != 1 || (starts == 0) != tc.conflict {
				t.Fatal("wrong existing-route preflight result", err, reads, starts)
			}
		})
	}
}

func TestTransparentFakeIPRouteDiscoveryFailureAndModeScope(t *testing.T) {
	for _, mode := range []string{"whitelist", "global", "probe", "no-dns"} {
		t.Run(mode, func(t *testing.T) {
			r := fakeIPConflictRequest()
			switch mode {
			case "global":
				r.RoutingMode = modeGlobal
			case "probe":
				r.ProbeOnly = true
			case "no-dns":
				r.managedDNS = nil
			}
			reads, starts, want := 0, 0, errors.New("route snapshot failed")
			err := withValidatedTransparentAddresses(r, transparentAddressSpace{}, func() ([]netip.Prefix, error) {
				return nil, nil
			}, func() ([]netip.Prefix, error) { reads++; return nil, want }, func() error { starts++; return nil })
			if mode == "whitelist" {
				if !errors.Is(err, want) || starts != 0 || reads != 1 {
					t.Fatal("failed route discovery entered new-session operations", err, reads, starts)
				}
			} else if err != nil || starts != 1 || reads != 0 {
				t.Fatal("inactive fake-IP mode gained a route discovery dependency", err, reads, starts)
			}
		})
	}
}
