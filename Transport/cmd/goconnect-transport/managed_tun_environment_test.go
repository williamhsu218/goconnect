package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedTunChildEnvironmentIsScopedAndOverridesHostDetection(t *testing.T) {
	t.Setenv("SKIP_SYSTEM_IPV6_CHECK", "0")
	t.Setenv("GOCONNECT_PARENT_ONLY", "must-not-leak")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$SKIP_SYSTEM_IPV6_CHECK\" \"${GOCONNECT_PARENT_ONLY-unset}\" > \"$2/child-environment.txt\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bin/mihomo"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd, err := defaultTransparentRuntime().startCore(dir, dir, filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "child-environment.txt"))
	if err != nil || string(data) != "1\nunset\n" {
		t.Fatalf("unexpected child environment: %q, %v", data, err)
	}
	if os.Getenv("SKIP_SYSTEM_IPV6_CHECK") != "0" {
		t.Fatal("parent environment was modified")
	}
}

// Use the actual bundled parser without enabling TUN or altering any OS route.
// This catches Mihomo silently dropping inet6-address on an IPv4-only Mac.
func TestLiveManagedTunKeepsIPv6Configuration(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("packaged runtime required")
	}
	t.Setenv("SKIP_SYSTEM_IPV6_CHECK", "0")
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("isolated-ipv6-", 4)
	config := makeSharedCompanyConfig(routeRequest{SOCKSPort: 9, Token: token}, "utun998", port)
	config["tun"].(map[string]any)["enable"] = false
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd, err := defaultTransparentRuntime().startCore(runtime, dir, file)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		if err := stopChild(cmd, done); err != nil {
			t.Error(err)
		}
	}()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/configs", port), nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var body struct {
			Tun struct {
				Enable       bool     `json:"enable"`
				Inet6Address []string `json:"inet6-address"`
			} `json:"tun"`
		}
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("controller response: %d, %v", response.StatusCode, err)
		}
		if body.Tun.Enable {
			t.Fatal("test must never enable an OS TUN")
		}
		if len(body.Tun.Inet6Address) != 1 || body.Tun.Inet6Address[0] != systemCaptureIPv6+"/126" {
			t.Fatalf("bundled parser discarded managed IPv6 configuration: %v", body.Tun.Inet6Address)
		}
		return
	}
	t.Fatal("isolated controller did not become ready")
}
