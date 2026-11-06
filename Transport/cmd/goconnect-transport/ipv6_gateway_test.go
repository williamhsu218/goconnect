package main

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestSubscriptionIPv6OnlyAndDualStackSelection(t *testing.T) {
	for _, tc := range []struct {
		ips  []net.IP
		want string
	}{
		{[]net.IP{net.ParseIP("2001:db8::20")}, "2001:db8::20"},
		{[]net.IP{net.ParseIP("2001:db8::20"), net.ParseIP("192.0.2.20")}, "192.0.2.20"},
	} {
		got, err := subscriptionGateway(tc.ips)
		if err != nil || got != tc.want {
			t.Fatal(got, err)
		}
	}
	if _, err := subscriptionGateway([]net.IP{nil, net.ParseIP("::"), net.ParseIP("fe80::1")}); err == nil {
		t.Fatal("accepted unusable node")
	}
}

func TestIPv6GatewayUsesMatchingPhysicalNextHopAndHostRoute(t *testing.T) {
	primary := originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}
	outer, err := gatewayPhysicalNetwork("2001:db8::20", primary, func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "-n get -inet6 -ifscope en0 default" {
			t.Fatal(args)
		}
		return []byte("gateway: fe80::1%en0\ninterface: en0\n"), nil
	})
	if err != nil || outer.Gateway != "fe80::1%en0" {
		t.Fatal(outer, err)
	}
	route := transparentRoute{Prefix: "2001:db8::20/128", Interface: "en0", Gateway: outer.Gateway}
	want := []string{"-n", "add", "-inet6", "-host", "2001:db8::20", "fe80::1%en0"}
	if !reflect.DeepEqual(transparentRouteArguments("add", route), want) {
		t.Fatal(transparentRouteArguments("add", route))
	}
	if err := validateRecoveryState(transparentState{Device: "utun100", State: "ready", Routes: []transparentRoute{route}}); err != nil {
		t.Fatal(err)
	}
	if !staticGatewayInTable([]byte("2001:db8::20 fe80::1%en0 UGHS en0\n"), "2001:db8::20", outer) {
		t.Fatal("IPv6 pin not recognized")
	}
	_, err = gatewayPhysicalNetwork("2001:db8::20", primary, func(string, ...string) ([]byte, error) {
		return []byte("gateway: fe80::1%utun3\ninterface: utun3\n"), nil
	})
	if err == nil {
		t.Fatal("accepted another VPN")
	}
}

func TestIPv6OnlyOriginalPhysicalNetwork(t *testing.T) {
	network, err := originalPhysicalNetworkWith(false, func() ([]net.Interface, error) { return nil, nil }, func(_ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "-inet6") {
			return []byte("gateway: fe80::1%en0\ninterface: en0\n"), nil
		}
		return nil, errors.New("no IPv4 default")
	})
	if err != nil || network.Interface != "en0" || network.Gateway != "fe80::1%en0" {
		t.Fatal(network, err)
	}
}

func TestGatewayRouteLookupUsesAddressFamilyAndDiagnosticLabel(t *testing.T) {
	for _, tc := range []struct{ gateway, family, label string }{
		{"192.0.2.20", "-inet", "route.get4"},
		{"2001:db8::20", "-inet6", "route.get6"},
	} {
		actual, err := gatewayRouteInterfaceWith(tc.gateway, func(name string, args ...string) ([]byte, error) {
			if name != "/sbin/route" || !reflect.DeepEqual(args, []string{"-n", "get", tc.family, tc.gateway}) {
				t.Fatalf("wrong gateway query: %s %v", name, args)
			}
			if label := systemCommandLabel(name, args); label != tc.label {
				t.Fatal(label)
			}
			return []byte("interface: en0\n"), nil
		})
		if err != nil || actual != "en0" {
			t.Fatal(actual, err)
		}
	}
}

func TestIPv6GatewayRouteLookupOnLoopback(t *testing.T) {
	// Read-only reproduction on macOS: the old family-less invocation exits 68.
	actual, err := gatewayRouteInterfaceWith("::1", systemCommand)
	if err != nil || actual != "lo0" {
		t.Fatal("IPv6 route lookup failed", actual, err)
	}
}

func TestIPv6GatewayPinNormalizesAddressesWithoutWeakeningOwnership(t *testing.T) {
	original := originalNetwork{Interface: "en0", Gateway: "fe80::1%en0"}
	for _, tc := range []struct {
		row  string
		want bool
	}{
		{"2001:db8::20 fe80::1%en0 UGHS en0", true},
		{"2001:0db8:0000:0000:0000:0000:0000:0020/128 fe80:0:0:0:0:0:0:1%en0 UGHS en0", true},
		{"2001:db8::/64 fe80::1%en0 UGHS en0", false},
		{"2001:db8::21 fe80::1%en0 UGHS en0", false},
		{"2001:db8::20 fe80::1%en1 UGHS en0", false},
		{"2001:db8::20 fe80::1 UGHS en0", false},
		{"2001:db8::20 fe80::1%en0 UGHS en1", false},
		{"2001:db8::20 fe80::1%en0 UGHScI en0", false},
		{"2001:db8::20 fe80::1%en0 UGHWIi en0", false},
		{"default fe80::1%en0 UGScg en0", false},
	} {
		if got := staticGatewayInTable([]byte(tc.row+"\n"), "2001:db8::20", original); got != tc.want {
			t.Fatalf("row=%q got=%v want=%v", tc.row, got, tc.want)
		}
	}
}

func TestDualStackGatewayHealthDetectsIPv6OnlyChanges(t *testing.T) {
	primary := originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}
	original := originalNetwork{Interface: "en0", Gateway: "fe80::1%en0"}
	pin := "2001:db8::20 fe80::1%en0 UGHS en0\n"
	for _, tc := range []struct {
		name, table, actual string
		want                bool
	}{
		{"healthy", "default fe80::1%en0 UGcg en0\n" + pin, "en0", true},
		{"equivalent formatting", "default fe80:0:0:0:0:0:0:1%en0 UGcg en0\n2001:db8:0:0:0:0:0:20/128 fe80:0:0:0:0:0:0:1%en0 UGHS en0\n", "en0", true},
		{"changed native router with stale scoped default", "default fe80::2%en0 UGcg en0\ndefault fe80::1%en0 UGScIg en0\n" + pin, "en0", false},
		{"only stale scoped default", "default fe80::1%en0 UGScIg en0\n" + pin, "en0", false},
		{"missing pin", "default fe80::1%en0 UGcg en0\n", "en0", false},
		{"replaced pin", "default fe80::1%en0 UGcg en0\n2001:db8::20 fe80::2%en0 UGHS en0\n", "en0", false},
		{"foreign forwarding interface", "default fe80::1%en0 UGcg en0\n" + pin, "utun100", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The IPv4 health check still succeeds in all of these scenarios.
			now, err := originalPhysicalNetworkWith(true, func() ([]net.Interface, error) {
				return []net.Interface{{Name: "en0", Flags: net.FlagUp}}, nil
			}, func(string, ...string) ([]byte, error) { return []byte("gateway: 192.168.1.1\ninterface: en0\n"), nil })
			if err != nil || now != primary {
				t.Fatal("fixture lost stable IPv4 path", now, err)
			}
			got, err := transparentGatewayReadyWith("2001:db8::20", original, func(name string, args ...string) ([]byte, error) {
				if name == "/usr/sbin/netstat" {
					if !reflect.DeepEqual(args, []string{"-rn", "-f", "inet6"}) {
						t.Fatal(args)
					}
					return []byte(tc.table), nil
				}
				if name != "/sbin/route" || !reflect.DeepEqual(args, []string{"-n", "get", "-inet6", "2001:db8::20"}) {
					t.Fatal(name, args)
				}
				return []byte("interface: " + tc.actual + "\n"), nil
			})
			if err != nil || got != tc.want {
				t.Fatal("incorrect IPv6 health", got, err)
			}
		})
	}
}

func TestGatewayHealthPreservesQueryFailures(t *testing.T) {
	original := originalNetwork{Interface: "en0", Gateway: "fe80::1%en0"}
	for _, command := range []string{"/usr/sbin/netstat", "/sbin/route"} {
		failure := &systemCommandFailure{details: diagnosticDetails{Reason: "system_timeout", Command: "route.get6", OperationID: 42}, cause: context.DeadlineExceeded}
		ok, err := transparentGatewayReadyWith("2001:db8::20", original, func(name string, _ ...string) ([]byte, error) {
			if name == command {
				return nil, failure
			}
			return []byte("default fe80::1%en0 UGcg en0\n2001:db8::20 fe80::1%en0 UGHS en0\n"), nil
		})
		if ok || !errors.Is(err, context.DeadlineExceeded) || diagnosticError(err).OperationID != 42 {
			t.Fatal("query failure hidden", ok, err)
		}
	}
}

func TestIPv4GatewayReadinessRemainsValid(t *testing.T) {
	ok, err := transparentGatewayReadyWith("192.0.2.20", originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}, func(name string, args ...string) ([]byte, error) {
		if name == "/usr/sbin/netstat" {
			if !reflect.DeepEqual(args, []string{"-rn", "-f", "inet"}) {
				t.Fatal(args)
			}
			return []byte("default 192.168.1.1 UGScg en0\n192.0.2.20 192.168.1.1 UGHS en0\n"), nil
		}
		if name != "/sbin/route" || !reflect.DeepEqual(args, []string{"-n", "get", "-inet", "192.0.2.20"}) {
			t.Fatal(name, args)
		}
		return []byte("interface: en0\n"), nil
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}
