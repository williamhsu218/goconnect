package main

import (
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticRotationAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.jsonl")
	for i := 0; i < 20; i++ {
		if err := appendDiagnostic(path, []byte("{\"event\":\"test\"}\n"), 64); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{path, path + ".1"} {
		st, err := os.Stat(p)
		if err != nil || st.Size() > 64 || st.Mode().Perm() != 0600 {
			t.Fatalf("bad rotation: %v %v", st, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("unbounded rotation")
	}
}
func TestDiagnosticRejectUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "public"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			path := filepath.Join(dir, "worker.jsonl")
			if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				os.Symlink(target, path)
			case "hardlink":
				os.Link(target, path)
			case "fifo":
				unix.Mkfifo(path, 0600)
			case "public":
				os.WriteFile(path, nil, 0644)
			}
			if err := appendDiagnostic(path, []byte("x"), 64); err == nil {
				t.Fatal("unsafe file accepted")
			}
			if got, _ := os.ReadFile(target); string(got) != "untouched" {
				t.Fatal("modified target")
			}
		})
	}
}
func TestDiagnosticQueueBoundedAndTail(t *testing.T) {
	// A stopped consumer must never stall routing, even when overwhelmed.
	l := &diagnosticLog{queue: make(chan diagnosticRecord, 128)}
	start := time.Now()
	for i := 0; i < 10000; i++ {
		l.record("network.poll_failed", 1)
	}
	if time.Since(start) > time.Second || len(l.queue) != 128 || l.dropped.Load() != 9872 {
		t.Fatal("queue is not bounded/nonblocking")
	}
	dir := t.TempDir()
	live, err := startDiagnosticLog(dir, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if second, err := startDiagnosticLog(dir, "worker"); err == nil {
		second.close()
		t.Fatal("overlapping writers")
	}
	live.record("route.ready", 0)
	live.record("token=SECRET\n", 1)
	live.close()
	data := diagnosticTail(filepath.Join(dir, "worker.jsonl"))
	var r diagnosticRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &r); err != nil || r.Event != "route.ready" {
		t.Fatalf("unexpected log %q %v", data, err)
	}
	if strings.Contains(data, "SECRET") {
		t.Fatal("invalid code leaked")
	}
}
func TestServiceDiagnosticsRequestScope(t *testing.T) {
	if _, err := decodeServiceRequest([]byte(`{"action":"diagnostics"}`)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{"action":"diagnostics","session":"/etc/passwd"}`, `{"action":"diagnostics","path":"/etc/passwd"}`} {
		if _, err := decodeServiceRequest([]byte(input)); err == nil {
			t.Fatal("unexpected file selector allowed")
		}
	}
}
