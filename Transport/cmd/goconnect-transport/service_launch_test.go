package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Parse with the same platform parser launchd uses, rather than matching XML
// text. A background policy silently throttles every descendant forwarding core.
func TestNetworkServiceLaunchPolicy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd policy is macOS-only")
	}
	revision := strings.Repeat("a", 64)
	cmd := exec.Command("/usr/bin/plutil", "-convert", "json", "-o", "-", "--", "-")
	cmd.Stdin = bytes.NewReader(networkServicePlist(revision))
	data, err := cmd.Output()
	if err != nil {
		t.Fatal("launchd property list is invalid", err)
	}
	var plist struct {
		Label            string
		ProcessType      string
		ProgramArguments []string
		RunAtLoad        bool
		KeepAlive        bool
		ThrottleInterval int
		ExitTimeOut      int
		Umask            int
	}
	if err := json.Unmarshal(data, &plist); err != nil {
		t.Fatal(err)
	}
	if plist.ProcessType != "Interactive" {
		t.Fatalf("live network forwarding must not inherit background throttling: %q", plist.ProcessType)
	}
	if plist.Label != serviceLabel || len(plist.ProgramArguments) != 2 ||
		plist.ProgramArguments[0] != filepath.Join(serviceBase, "versions", revision, "bin/GoConnectTransport") ||
		plist.ProgramArguments[1] != "service-run" {
		t.Fatal("service identity or immutable runtime target changed")
	}
	if !plist.RunAtLoad || !plist.KeepAlive || plist.ThrottleInterval != 10 || plist.ExitTimeOut != 20 || plist.Umask != 63 {
		t.Fatal("restart, shutdown or permission protection changed")
	}
}
