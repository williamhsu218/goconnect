package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCanceledProbeDoesNotReportTrafficFallback(t *testing.T) {
	failure := errors.New("probe process stopped")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if message := routingCheckError(ctx, failure).Error(); message != "分流自检已取消。" {
		t.Fatal("canceled probe reported as routing failure", message)
	}
	if routingCheckError(context.Background(), failure) != failure {
		t.Fatal("real probe failure was suppressed")
	}
}

func TestPathRulesMatchBundleBoundaryAndEscapeRegex(t *testing.T) {
	rule := pathRule("/Applications/Example (QA)+.app")
	pattern := strings.TrimSuffix(strings.TrimPrefix(rule, "PROCESS-PATH-REGEX,"), ",GoConnect")
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/Applications/Example (QA)+.app/Contents/MacOS/App", "/Applications/Example (QA)+.app/Contents/Frameworks/Helper.app/Contents/MacOS/Helper"} {
		if !re.MatchString(path) {
			t.Fatal("missed selected bundle", path)
		}
	}
	for _, path := range []string{"/Applications/Example (QA)+.app.evil/Contents/App", "/Applications/Example QA.app/Contents/App", "/usr/bin/curl", ""} {
		if re.MatchString(path) {
			t.Fatal("matched unrelated executable", path)
		}
	}
}

func TestStandaloneExecutableRuleMatchesOnlyExactPath(t *testing.T) {
	rule := processPathRule("/Users/test/.local/bin/agy", "GoConnect")
	pattern := strings.TrimSuffix(strings.TrimPrefix(rule, "PROCESS-PATH-REGEX,"), ",GoConnect")
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("/Users/test/.local/bin/agy") {
		t.Fatal("exact executable missed")
	}
	for _, path := range []string{"/Users/test/.local/bin/agy-helper", "/Users/test/.local/bin/agy/child", "/tmp/agy", ""} {
		if re.MatchString(path) {
			t.Fatal("unrelated executable matched", path)
		}
	}
}

func TestConfigHasNoFallbackAndKeepsSystemDNS(t *testing.T) {
	r := routeRequest{OwnerPID: 123, Token: strings.Repeat("x", 40), SOCKSPort: 1234, AppPaths: []string{"/Applications/A.app"}, Gateway: "2001:db8::10"}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	config := makeConfig(r, "/App/Runtime", "utun100", 19999)
	rules := config["rules"].([]string)
	if rules[len(rules)-1] != "MATCH,REJECT" || rules[len(rules)-2] != "PROCESS-PATH-REGEX,^/Applications/A\\.app/Contents/.*,GoConnect" {
		t.Fatal("bad rule precedence")
	}
	if _, ok := config["proxy-groups"]; ok {
		t.Fatal("unexpected fallback group")
	}
	if config["dns"].(map[string]any)["enable"] != false {
		t.Fatal("system DNS must remain unchanged")
	}
	tun := config["tun"].(map[string]any)
	if tun["auto-route"] != false || tun["auto-detect-interface"] != false || config["ipv6"] != false || len(tun["inet6-address"].([]string)) != 0 {
		t.Fatal("ordinary applications would be captured or IPv6 availability altered")
	}
	if _, ok := tun["route-address"]; ok {
		t.Fatal("global routes must not be installed")
	}
	if strings.Contains(strings.Join(rules, "\n"), ",DIRECT") {
		t.Fatal("captured traffic must not silently fall back to direct")
	}
	r.AppPaths = []string{"/Applications/A.app,Other"}
	if r.validate() == nil {
		t.Fatal("rule injection accepted")
	}
	live := routeRequest{LiveRouting: true, OwnerPID: 123, Token: strings.Repeat("x", 40), SOCKSPort: 1234, AppPaths: []string{"/Users/test/.local/bin/agy"}, Gateway: "127.0.0.1"}
	if err := live.validate(); err != nil {
		t.Fatal("standalone live target rejected", err)
	}
	live.LiveRouting = false
	if live.validate() == nil {
		t.Fatal("legacy launcher accepted a standalone executable")
	}
}

func TestProbeScopesPFWithoutChangingCoreRules(t *testing.T) {
	r := routeRequest{Token: strings.Repeat("x", 40), SOCKSPort: 1234, AppPaths: []string{"/Applications/A.app"}, Gateway: "127.0.0.1"}
	production := makeConfig(r, "/Runtime", "utun100", 19999)
	r.ProbeOnly = true
	probe := makeConfig(r, "/Runtime", "utun100", 19999)
	tun := probe["tun"].(map[string]any)
	if tun["auto-route"] != false {
		t.Fatal("probe must not install a destination route")
	}
	rules := captureRules(501, []capturedApp{{GID: 61001}}, "utun100", true, false)
	if !strings.Contains(rules, "to 1.1.1.1 user 501 group 61001") {
		t.Fatal("probe scope is not limited")
	}
	if probe["ipv6"] != false || len(tun["inet6-address"].([]string)) != 0 {
		t.Fatal("probe must not route IPv6")
	}
	if strings.Join(production["rules"].([]string), "\n") != strings.Join(probe["rules"].([]string), "\n") {
		t.Fatal("probe and production must use the same process rules")
	}
}

func TestCaptureLabelsAndRulesAreAppAndUserScoped(t *testing.T) {
	if appGroupName(501, "/Applications/A.app") == appGroupName(502, "/Applications/A.app") || appGroupName(501, "/Applications/A.app") == appGroupName(501, "/Applications/B.app") {
		t.Fatal("routing labels collide")
	}
	apps := []capturedApp{{GID: 61001}, {GID: 61002}}
	rules := captureRules(501, apps, "utun123", false, false)
	if strings.Contains(rules, "keep state") || strings.Count(rules, "flags any no state") != len(apps) {
		t.Fatal("a reused socket port could inherit another application's route")
	}
	for _, line := range strings.Split(strings.TrimSpace(rules), "\n") {
		if !strings.Contains(line, "proto { tcp udp }") || !strings.Contains(line, "user 501 group 6100") || !strings.Contains(line, "on ! lo0") {
			t.Fatal("rule affects unattributable protocols, other users, or loopback", line)
		}
	}
	blocked := captureRules(501, apps, "utun123", false, true)
	if strings.Contains(blocked, "pass ") || strings.Contains(blocked, "route-to") {
		t.Fatal("blocked mode would forward traffic")
	}
}

func TestCommandsCannotLaunchArbitraryProgramsOrInvokeProductionProbes(t *testing.T) {
	for _, command := range []routeCommand{{Sequence: 1, Action: "probe", Network: "udp", Target: "arbitrary-host"}, {Sequence: 1, Action: "probe", Network: "tcp", Target: "tailscale-dns"}} {
		if command.validate(1, true) == nil {
			t.Fatal("unbounded probe target accepted")
		}
	}
	for _, command := range []routeCommand{{Sequence: 1, AppIndex: -1, Action: "launch"}, {Sequence: 1, AppIndex: 1, Action: "launch"}, {Sequence: 1, Action: "probe", Network: "tcp"}, {Sequence: 1, Action: "launch", Network: "shell"}, {Sequence: 1, Action: "launch", LocalPort: 50000}, {Action: "launch"}} {
		if command.validate(1, false) == nil {
			t.Fatal("unsafe production command accepted", command)
		}
	}
	for _, command := range []routeCommand{{Sequence: 1, Action: "probe", Network: "tcp", LocalPort: 50000}, {Sequence: 1, Action: "probe", Network: "udp", LocalPort: 53}} {
		if command.validate(1, true) == nil {
			t.Fatal("unsupported source port accepted", command)
		}
	}
	if (routeCommand{Sequence: 1, Action: "launch"}).validate(1, false) != nil || (routeCommand{Sequence: 1, Action: "probe", Network: "udp", Helper: true}).validate(1, true) != nil {
		t.Fatal("valid command rejected")
	}
}

func TestProcessCoverageRequiresCorrectUIDGroupAndBundleBoundary(t *testing.T) {
	app := capturedApp{Path: "/Applications/A.app", Binary: "/Applications/A.app/Contents/MacOS/A", GID: 61001}
	processes := []processIdentity{{501, 61001, 1, app.Path + "/Contents/MacOS/A"}, {501, 20, 2, app.Path + "/Contents/Helper"}, {502, 61001, 3, app.Path + "/Contents/MacOS/A"}, {501, 61001, 4, app.Path + ".other/Contents/MacOS/A"}}
	managed, unmanaged := appProcessCounts(app, 501, processes)
	if managed != 1 || unmanaged != 1 {
		t.Fatal("coverage incorrectly reported", managed, unmanaged)
	}
	if !appMainRunning(app, 501, processes) || appMainRunning(app, 501, processes[1:]) {
		t.Fatal("main process requires the selected user and routing group")
	}
	if appMainRunning(app, 501, []processIdentity{{501, 61001, 5, app.Path + "/Contents/Helpers/Worker"}}) {
		t.Fatal("a leftover helper must not prevent reopening the application")
	}
}

func TestTelemetryAttributesHelpersAndExcludesDestinations(t *testing.T) {
	var connections coreConnections
	err := json.Unmarshal([]byte(`{"connections":[
	 {"metadata":{"processPath":"/Applications/A.app/Contents/Helpers/Worker","host":"private.example","destinationIP":"10.20.30.40"},"chains":["GoConnect"]},
	 {"metadata":{"processPath":"/Applications/A.app/Contents/MacOS/Excluded"},"chains":["DIRECT"]},
	 {"metadata":{"processPath":"/Applications/A.app.fake/Contents/MacOS/Other"},"chains":["DIRECT"]},
	 {"metadata":{"processPath":""},"chains":["DIRECT"]}]}`), &connections)
	if err != nil {
		t.Fatal(err)
	}
	summary := summarizeConnections(connections, []string{"/Applications/A.app"}, routeSnapshot{})
	if summary.VPN != 1 || summary.Direct != 3 || summary.Unknown != 1 || !summary.VPNObserved || summary.Apps[0].VPN != 1 || summary.Apps[0].Direct != 1 {
		t.Fatalf("incorrect app attribution: %+v", summary)
	}
	data, _ := json.Marshal(summary)
	if strings.Contains(string(data), "private.example") || strings.Contains(string(data), "10.20.30.40") {
		t.Fatal("destination leaked into GUI state")
	}
	summary = summarizeConnections(coreConnections{}, []string{"/Applications/A.app"}, summary)
	if summary.VPN != 0 || summary.Direct != 0 || summary.Apps[0].VPN != 0 || !summary.VPNObserved {
		t.Fatal("active counts should reset while session evidence persists")
	}
}

func TestTelemetryAttributesStandaloneExecutableExactly(t *testing.T) {
	var connections coreConnections
	err := json.Unmarshal([]byte(`{"connections":[
	 {"metadata":{"processPath":"/Users/test/.local/bin/agy"},"chains":["GoConnect"]},
	 {"metadata":{"processPath":"/Users/test/.local/bin/agy-helper"},"chains":["DIRECT"]}]}`), &connections)
	if err != nil {
		t.Fatal(err)
	}
	summary := summarizeConnections(connections, []string{"/Users/test/.local/bin/agy"}, routeSnapshot{})
	if summary.Apps[0].VPN != 1 || summary.Apps[0].Direct != 0 {
		t.Fatalf("standalone attribution was not exact: %+v", summary.Apps[0])
	}
}

func controlFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Example.app")
	if err = os.MkdirAll(filepath.Join(app, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := routeRequest{OwnerPID: os.Getpid(), ControlDirectory: dir, Token: strings.Repeat("x", 40), SOCKSPort: 1234, AppPaths: []string{app}, Gateway: "192.0.2.10"}
	data, _ := json.Marshal(r)
	if err = os.WriteFile(filepath.Join(dir, "session.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "lease"), []byte(r.Token), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLeaseRevocationAndExpiry(t *testing.T) {
	dir := controlFixture(t)
	control, err := openControl(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.close()
	if !control.alive() {
		t.Fatal("fresh lease rejected")
	}
	old := time.Now().Add(-10 * time.Second)
	_ = os.Chtimes(filepath.Join(dir, "lease"), old, old)
	if control.alive() {
		t.Fatal("stale owner lease accepted")
	}
	_ = os.Remove(filepath.Join(dir, "lease"))
	if control.alive() {
		t.Fatal("revoked lease accepted")
	}
}

func TestRootControlRefusesSymlinksAndUnknownFields(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		dir := controlFixture(t)
		victim := filepath.Join(dir, "untouched")
		_ = os.WriteFile(victim, []byte("keep"), 0o600)
		_ = os.Symlink(victim, filepath.Join(dir, "status"))
		if c, err := openControl(filepath.Join(dir, "session.json")); err == nil {
			c.close()
			t.Fatal("status symlink accepted")
		}
		data, _ := os.ReadFile(victim)
		if string(data) != "keep" {
			t.Fatal("symlink target overwritten")
		}
	})
	t.Run("lease", func(t *testing.T) {
		dir := controlFixture(t)
		_ = os.Rename(filepath.Join(dir, "lease"), filepath.Join(dir, "real-lease"))
		_ = os.Symlink("real-lease", filepath.Join(dir, "lease"))
		if c, err := openControl(filepath.Join(dir, "session.json")); err == nil {
			c.close()
			t.Fatal("lease symlink accepted")
		}
	})
	t.Run("schema", func(t *testing.T) {
		dir := controlFixture(t)
		path := filepath.Join(dir, "session.json")
		data, _ := os.ReadFile(path)
		data = append([]byte(`{"command":"evil",`), data[1:]...)
		_ = os.WriteFile(path, data, 0o600)
		if c, err := openControl(path); err == nil {
			c.close()
			t.Fatal("unknown command accepted")
		}
	})
}
