package main

import (
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExplicitProxyNeverCapturesSystem(t *testing.T) {
	r := proxyFrontRequest{Port: 31001, Token: strings.Repeat("x", 40), DirectDomains: []domainRule{{Domain: "example.org", IncludeSubdomains: true}}}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	cfg := makeProxyFrontConfig(r, 31002, []localNetworkRoute{{Interface: "en0", Prefix: netip.MustParsePrefix("192.168.31.0/24")}})
	for _, key := range []string{"tun", "dns", "sniffer"} {
		if cfg[key].(map[string]any)["enable"] != false {
			t.Fatalf("%s captures system", key)
		}
	}
	if cfg["bind-address"] != "127.0.0.1" || cfg["allow-lan"] != false || cfg["find-process-mode"] != "off" {
		t.Fatal("unsafe ingress")
	}
	rules := cfg["rules"].([]string)
	if rules[len(rules)-1] != "MATCH,GoConnect" {
		t.Fatal("unexpected direct fallback")
	}
	joined := strings.Join(rules, "\n")
	for _, want := range []string{"DOMAIN-SUFFIX,example.org,DIRECT", "IP-CIDR,192.168.31.0/24,DIRECT,no-resolve"} {
		if !strings.Contains(joined, want) {
			t.Fatal(want)
		}
	}
	if strings.Contains(joined, "PROCESS") {
		t.Fatal("process classification enabled")
	}
}

func TestLiveNativeServiceRuntimeDependencies(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("packaged runtime required")
	}
	target := t.TempDir()
	if err := copyServiceRuntime(runtime, target); err != nil {
		t.Fatal(err)
	}
	before, err := runtimeRevision(runtime)
	if err != nil {
		t.Fatal(err)
	}
	after, err := runtimeRevision(target)
	if err != nil || before != after {
		t.Fatalf("copy integrity: %v", err)
	}
	if out, err := exec.Command(filepath.Join(target, "bin/openconnect"), "--version").CombinedOutput(); err != nil {
		t.Fatalf("native runtime could not load: %s %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(target, "share/cacert.pem"), []byte("changed roots"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := runtimeRevision(target)
	if err != nil || changed == before {
		t.Fatal("CA roots missing from pinned revision")
	}
}

func TestStopChildAlsoStopsOrphanedHookGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "/bin/sleep 60 & exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	done := make(chan error, 1)
	// Simulate OpenConnect leader exiting while its script child still exists.
	done <- cmd.Wait()
	start := time.Now()
	if err := stopChild(cmd, done); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("hook group cleanup exceeded bound")
	}
	if !errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH) {
		t.Fatal("hook survived leader cleanup")
	}
}
