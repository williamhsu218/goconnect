package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransparentRoutingModes(t *testing.T) {
	for _, mode := range []string{modeGlobal, modeWhitelist} {
		r := routeRequest{TransparentRouting: true, LiveRouting: true, RoutingMode: mode, AppPaths: []string{"/Applications/Selected.app"}, SOCKSPort: 12345, Token: strings.Repeat("x", 40)}
		cfg := makeTransparentConfig(r, "utun100", 12346, originalNetwork{Interface: "en0", Gateway: "192.168.31.1"}, nil, tailscaleBypass{})
		tun := cfg["tun"].(map[string]any)
		if tun["auto-route"] != false || cfg["find-process-mode"] != "strict" {
			t.Fatal("unexpected capture or process policy")
		}
		rules := cfg["rules"].([]string)
		wanted, otherwise := "GoConnect", "OriginalNetwork"
		if mode == modeGlobal {
			wanted, otherwise = otherwise, wanted
		}
		if !strings.HasSuffix(rules[len(rules)-2], ","+wanted) || rules[len(rules)-1] != "MATCH,"+otherwise {
			t.Fatal(rules)
		}
		proxies := cfg["proxies"].([]map[string]any)
		if proxies[1]["interface-name"] != "en0" || proxies[0]["server"] != "127.0.0.1" {
			t.Fatal("egress loop protection lost")
		}
	}
}
func TestTransparentProbeCannotCaptureDefaultRoutes(t *testing.T) {
	p := transparentCapturePrefixes(true)
	if len(p) != 2 || p[0] != "1.1.1.1/32" || p[1] != "203.0.113.0/24" {
		t.Fatal(p)
	}
}
func TestTransparentDirectDomainPrecedesAppRule(t *testing.T) {
	r := routeRequest{RoutingMode: modeWhitelist, AppPaths: []string{"/Applications/A.app"}, DirectDomains: []domainRule{{Domain: "example.com", IncludeSubdomains: true}}}
	rules := makeTransparentConfig(r, "utun100", 12346, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})["rules"].([]string)
	joined := strings.Join(rules, "\n")
	domain, app := strings.Index(joined, "DOMAIN-SUFFIX,example.com,OriginalNetwork"), strings.Index(joined, "PROCESS-PATH-REGEX")
	if domain < 0 || app < 0 || domain > app {
		t.Fatal(rules)
	}
}

func TestTransparentStandaloneExecutableRuleIsExact(t *testing.T) {
	service := "/Users/test/.local/bin/agy"
	r := routeRequest{RoutingMode: modeWhitelist, AppPaths: []string{service}}
	rules := makeTransparentConfig(r, "utun100", 12346, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})["rules"].([]string)
	want := "PROCESS-PATH-REGEX,^/Users/test/\\.local/bin/agy$,GoConnect"
	if rules[len(rules)-2] != want {
		t.Fatalf("standalone rule is not exact: %q", rules[len(rules)-2])
	}
}

func TestServiceDispatchDoesNotConsumeStatusFile(t *testing.T) {
	dir := controlFixture(t)
	path := filepath.Join(dir, "session.json")
	inspected, err := inspectControl(path, false)
	if err != nil {
		t.Fatal(err)
	}
	inspected.close()
	if _, err = os.Stat(filepath.Join(dir, "status")); !os.IsNotExist(err) {
		t.Fatal("dispatch created status file")
	}
	session, err := openControl(path)
	if err != nil {
		t.Fatal(err)
	}
	session.close()
}

func TestGatewayPinRejectsClonedAndScopedHostRoutes(t *testing.T) {
	original := originalNetwork{Interface: "en0", Gateway: "192.168.31.1"}
	for _, flags := range []string{"UGHWIi", "UGHScI", "UGHS"} {
		table := []byte("203.0.113.5 192.168.31.1 " + flags + " en0\n")
		want := flags == "UGHS"
		if got := staticGatewayInTable(table, "203.0.113.5", original); got != want {
			t.Fatalf("flags=%s got=%v", flags, got)
		}
	}
	table := []byte("203.0.113.5 192.168.31.1 UGHWIi en0\n203.0.113.5 192.168.31.1 UGHS en0\n")
	if !staticGatewayInTable(table, "203.0.113.5", original) {
		t.Fatal("scoped cache hid static pin")
	}
	if staticGatewayInTable(table, "203.0.113.6", original) {
		t.Fatal("unrelated host accepted")
	}
}

func TestDirectEgressRequiresAnInterfaceScopedDefault(t *testing.T) {
	original := originalNetwork{Interface: "en0", Gateway: "192.168.31.1"}
	ordinary := []byte("default 192.168.31.1 UGScg en0\n0/1 link#30 UScg utun100\n128/1 link#30 USc utun100\n")
	if scopedDefaultInTable(ordinary, original, original.Gateway) {
		t.Fatal("unscoped default incorrectly accepted for bound DIRECT socket")
	}
	scoped := append(ordinary, []byte("default 192.168.31.1 UGScIg en0\n")...)
	if !scopedDefaultInTable(scoped, original, original.Gateway) {
		t.Fatal("scoped direct gateway missing")
	}
	if scopedDefaultInTable(scoped, originalNetwork{Interface: "en1"}, original.Gateway) {
		t.Fatal("wrong interface accepted")
	}
	for _, r := range []transparentRoute{{Prefix: "0.0.0.0/0", Interface: "en0", Gateway: original.Gateway, Scoped: true}, {Prefix: "::/0", Interface: "en0", Gateway: "fe80::1%en0", Scoped: true}} {
		for _, action := range []string{"add", "delete"} {
			args := strings.Join(transparentRouteArguments(action, r), " ")
			if !strings.Contains(args, "-ifscope en0 -net "+r.Prefix) || strings.Contains(args, "-host") {
				t.Fatal("scoped default would mutate global route", args)
			}
		}
	}
}
