package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func recoveryFixture(t *testing.T, s transparentState) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "transparent-123")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveTransparentState(filepath.Join(dir, "state.json"), s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("PRIVATE_TOKEN_MUST_NOT_BE_BACKED_UP"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTransparentCleanupUsesExactOwnedRoutes(t *testing.T) {
	cases := []struct {
		name  string
		r     transparentRoute
		table string
		owned bool
	}{
		{"owned tun", transparentRoute{Prefix: "0.0.0.0/1", Interface: "utun100"}, "0/1 link#30 UScg utun100\n", true},
		{"covering default", transparentRoute{Prefix: "0.0.0.0/1", Interface: "utun100"}, "default link#30 UScg utun100\n", false},
		{"replaced tun", transparentRoute{Prefix: "0.0.0.0/1", Interface: "utun100"}, "0/1 link#31 UScg utun101\n", false},
		{"owned pin", transparentRoute{Prefix: "203.0.113.5/32", Interface: "en0", Gateway: "192.168.1.1"}, "203.0.113.5 192.168.1.1 UGHS en0\n", true},
		{"new gateway on same nic", transparentRoute{Prefix: "203.0.113.5/32", Interface: "en0", Gateway: "192.168.1.1"}, "203.0.113.5 192.168.1.2 UGHS en0\n", false},
		{"cloned host", transparentRoute{Prefix: "203.0.113.5/32", Interface: "en0", Gateway: "192.168.1.1"}, "203.0.113.5 192.168.1.1 UGHWIi en0\n", false},
		{"owned scoped v4", transparentRoute{Prefix: "0.0.0.0/0", Interface: "en0", Gateway: "192.168.1.1", Scoped: true}, "default 192.168.1.1 UGScIg en0\n", true},
		{"unscoped default", transparentRoute{Prefix: "0.0.0.0/0", Interface: "en0", Gateway: "192.168.1.1", Scoped: true}, "default 192.168.1.1 UGScg en0\n", false},
		{"owned scoped v6", transparentRoute{Prefix: "::/0", Interface: "en0", Gateway: "fe80::1%en0", Scoped: true}, "default fe80::1%en0 UGScIg en0\n", true},
		{"owned v6 capture", transparentRoute{Prefix: "8000::/1", Interface: "utun100"}, "8000::/1 link#30 USc utun100\n", true},
		{"gone v6", transparentRoute{Prefix: "::/1", Interface: "utun100"}, "Internet6:\nDestination Gateway Flags Netif\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownedTransparentRouteInTable([]byte(tc.table), tc.r); got != tc.owned {
				t.Fatalf("ownership=%v want %v", got, tc.owned)
			}
		})
	}
}

func TestTransparentCleanupMissingRoutesAndRepeatedCleanupSucceed(t *testing.T) {
	s := transparentState{Device: "utun100", State: "cleanupFailed", Routes: []transparentRoute{{Prefix: "::/1", Interface: "utun100"}, {Prefix: "0.0.0.0/1", Interface: "utun100"}}}
	dir := recoveryFixture(t, s)
	command := func(name string, args ...string) ([]byte, error) {
		if name != "/usr/sbin/netstat" {
			t.Fatalf("absent route must not be looked up or deleted: %s %v", name, args)
		}
		return []byte("Destination Gateway Flags Netif\n"), nil
	}
	for i := 0; i < 2; i++ {
		if err := cleanupTransparentRoutesWith(filepath.Join(dir, "state.json"), &s, command); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.Routes) != 0 || s.State != "stopped" {
		t.Fatal(s)
	}
}

func TestTransparentCleanupDeleteRaceAndRealFailures(t *testing.T) {
	for _, outcome := range []string{"already-gone", "still-present", "unreadable-table"} {
		t.Run(outcome, func(t *testing.T) {
			s := transparentState{Device: "utun100", State: "ready", Routes: []transparentRoute{{Prefix: "0.0.0.0/1", Interface: "utun100"}}}
			dir := recoveryFixture(t, s)
			deleted := false
			command := func(name string, args ...string) ([]byte, error) {
				if name == "/sbin/route" {
					deleted = true
					return nil, errors.New("delete failed")
				}
				if deleted && outcome == "already-gone" {
					return nil, nil
				}
				if deleted && outcome == "unreadable-table" {
					return nil, errors.New("table unavailable")
				}
				return []byte("0/1 link#30 UScg utun100\n"), nil
			}
			err := cleanupTransparentRoutesWith(filepath.Join(dir, "state.json"), &s, command)
			if outcome == "already-gone" {
				if err != nil || len(s.Routes) != 0 {
					t.Fatal(s, err)
				}
			} else if err == nil || len(s.Routes) != 1 || s.State != "cleanupFailed" {
				t.Fatal(s, err)
			}
		})
	}
}

func TestAbandonedSessionRecoversAfterCoreCrashWithoutLeakingConfiguration(t *testing.T) {
	s := transparentState{Device: "utun100", State: "ready", CorePID: 12345, Routes: []transparentRoute{
		{Prefix: "203.0.113.5/32", Interface: "en0", Gateway: "192.168.1.1"},
		{Prefix: "0.0.0.0/1", Interface: "utun100"},
		{Prefix: "::/1", Interface: "utun100"},
	}}
	dir := recoveryFixture(t, s)
	var backup []byte
	var deleted []string
	command := func(name string, args ...string) ([]byte, error) {
		if name == "/sbin/route" {
			deleted = append(deleted, strings.Join(args, " "))
			return nil, nil
		}
		// The TUN routes vanished on core exit; the owned gateway pin remains.
		return []byte("203.0.113.5 192.168.1.1 UGHS en0\n"), nil
	}
	err := recoverTransparentDirectory(dir, uint32(os.Getuid()), nil, nil, command, func(_ string, data []byte) error { backup = append([]byte(nil), data...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("stale directory remains", err)
	}
	if !reflect.DeepEqual(deleted, []string{"-n delete -inet -host 203.0.113.5 192.168.1.1"}) {
		t.Fatal(deleted)
	}
	var original transparentState
	if err = json.Unmarshal(backup, &original); err != nil || !reflect.DeepEqual(s, original) {
		t.Fatal("original journal was not preserved", err)
	}
	if strings.Contains(string(backup), "PRIVATE_TOKEN") {
		t.Fatal("secret leaked into backup")
	}
}

func TestSessionRecoveryRefusesLiveCoreOrReusedDevice(t *testing.T) {
	for _, tc := range []struct {
		name       string
		processes  []processIdentity
		interfaces []net.Interface
	}{
		{"recorded pid", []processIdentity{{PID: 12345, UID: 501}}, nil},
		{"legacy root core", []processIdentity{{PID: 555, UID: 0, Path: serviceBase + "/versions/revision/bin/mihomo"}}, nil},
		{"reused tun", nil, []net.Interface{{Name: "utun100"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := recoveryFixture(t, transparentState{Device: "utun100", State: "ready", CorePID: 12345})
			err := recoverTransparentDirectory(dir, uint32(os.Getuid()), tc.processes, tc.interfaces, func(string, ...string) ([]byte, error) { t.Fatal("must not mutate routes"); return nil, nil }, func(string, []byte) error { t.Fatal("must not overwrite backup"); return nil })
			if err == nil {
				t.Fatal("live session recovered")
			}
			if _, e := os.Stat(dir); e != nil {
				t.Fatal("live session removed", e)
			}
		})
	}
}

func TestSessionRecoveryPreservesFailedCleanupAndUnsafeJournal(t *testing.T) {
	for _, outcome := range []string{"table-failed", "backup-failed", "invalid-route", "linked-state"} {
		t.Run(outcome, func(t *testing.T) {
			s := transparentState{Device: "utun100", State: "cleanupFailed", Routes: []transparentRoute{{Prefix: "::/1", Interface: "utun100"}}}
			if outcome == "invalid-route" {
				s.Routes[0].Prefix = "10.0.0.0/8"
			}
			dir := recoveryFixture(t, s)
			if outcome == "linked-state" {
				os.Rename(filepath.Join(dir, "state.json"), filepath.Join(dir, "original.json"))
				if err := os.Symlink("original.json", filepath.Join(dir, "state.json")); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			err := recoverTransparentDirectory(dir, uint32(os.Getuid()), nil, nil, func(string, ...string) ([]byte, error) { called = true; return nil, errors.New("unreadable table") }, func(string, []byte) error {
				if outcome == "backup-failed" {
					return errors.New("backup unavailable")
				}
				return nil
			})
			if err == nil {
				t.Fatal("unsafe recovery succeeded")
			}
			if _, e := os.Stat(dir); e != nil {
				t.Fatal("evidence removed", e)
			}
			if outcome != "table-failed" && called {
				t.Fatal("mutated before journal/backup validation")
			}
		})
	}
}
