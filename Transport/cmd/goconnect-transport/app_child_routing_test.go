package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppChildRulesRespectModeExplicitTargetsAndSessionOwner(t *testing.T) {
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r := routeRequest{TransparentRouting: true, LiveRouting: true, RoutingMode: mode,
			AppPaths: []string{"/Applications/Chosen.app", "/opt/vendor/cli"}, processOwnerUID: 501}
		if mode == modeWhitelist {
			r.DirectAppPaths = []string{"/Applications/Direct.app", "/opt/vendor/direct-cli"}
		}
		cfg := makeTransparentConfig(r, "utun100", 12345, originalNetwork{Interface: "en0"}, nil, tailscaleBypass{})
		rules := strings.Join(cfg["rules"].([]string), "\n")
		target, fallback := "GoConnect", "OriginalNetwork"
		if mode == modeGlobal {
			target, fallback = fallback, target
		}
		child := strings.Index(rules, "GOCONNECT-APP-CHILD,/Applications/Chosen.app,"+target+",501")
		exact := strings.Index(rules, processPathRule("/opt/vendor/cli", target))
		if child < 0 || exact < 0 || exact > child {
			t.Fatal("explicit CLI must precede derived child policy", rules)
		}
		if strings.Contains(rules, "GOCONNECT-APP-CHILD,/opt/vendor/cli") {
			t.Fatal("standalone CLI must not become an ancestry root")
		}
		if !strings.HasSuffix(rules, "MATCH,"+fallback) {
			t.Fatal("mode fallback changed", rules)
		}
		if mode == modeWhitelist {
			direct := strings.Index(rules, processPathRule("/opt/vendor/direct-cli", fallback))
			directChild := strings.Index(rules, "GOCONNECT-APP-CHILD,/Applications/Direct.app,"+fallback+",501")
			if direct < 0 || direct > exact || directChild < exact || directChild > child {
				t.Fatal("direct exclusions lost priority", rules)
			}
		}
	}
}

func TestAppChildRulesCannotTrustJSONUIDOrEnableOnProbe(t *testing.T) {
	var r routeRequest
	if err := json.Unmarshal([]byte(`{"transparentRouting":true,"liveRouting":true,"processOwnerUID":501,"appPaths":["/Applications/Chosen.app"]}`), &r); err != nil {
		t.Fatal(err)
	}
	cfg := makeTransparentConfig(r, "utun100", 12345, originalNetwork{}, nil, tailscaleBypass{})
	if strings.Contains(strings.Join(cfg["rules"].([]string), "\n"), "GOCONNECT-APP-CHILD") {
		t.Fatal("JSON spoof enabled child inheritance")
	}
	r.processOwnerUID = 501
	r.ProbeOnly = true
	cfg = makeTransparentConfig(r, "utun100", 12345, originalNetwork{}, nil, tailscaleBypass{})
	if strings.Contains(strings.Join(cfg["rules"].([]string), "\n"), "GOCONNECT-APP-CHILD") {
		t.Fatal("ProbeOnly enabled broad process association")
	}
}

func TestDirectAppScopeValidation(t *testing.T) {
	r := routeRequest{TransparentRouting: true, LiveRouting: true, RoutingMode: modeWhitelist, OwnerPID: 123, SOCKSPort: 1234, Token: strings.Repeat("a", 40), Gateway: "127.0.0.1", DirectAppPaths: []string{"/Applications/Missing.app", "/opt/vendor/cli"}}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", "/Applications/Bad,App.app", "/opt/bin/../cli", "/opt/bin/cli\nMATCH,GoConnect"} {
		candidate := r
		candidate.DirectAppPaths = []string{path}
		if candidate.validate() == nil {
			t.Fatal("invalid direct scope accepted", path)
		}
	}
	r.RoutingMode = modeGlobal
	if r.validate() == nil {
		t.Fatal("unused global direct scope accepted")
	}
}
