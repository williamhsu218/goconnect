package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompanyInstallVerifiesMutationAndRetainsUnconfirmedIntent(t *testing.T) {
	for _, outcome := range []string{"success", "zero-exit-no-effect", "timeout", "collision"} {
		t.Run(outcome, func(t *testing.T) {
			s := companyState{Device: "utun100", Networks: []string{"10.20.0.0/16"}, State: "starting"}
			path := filepath.Join(t.TempDir(), "state.json")
			added := false
			err := installCompanyRoutesWith(path, &s, func(name string, args ...string) ([]byte, error) {
				if name == "/sbin/route" {
					added = true
					if outcome == "timeout" {
						return nil, context.DeadlineExceeded
					}
					if outcome == "collision" {
						return []byte("add net: File exists\n"), nil
					}
					return nil, nil
				}
				if added && outcome != "zero-exit-no-effect" {
					return []byte("10.20/16 utun100 UScg utun100\n"), nil
				}
				return nil, nil
			})
			if outcome == "success" {
				if err != nil || len(s.Added) != 1 || len(s.Pending) != 0 || s.State != "ready" {
					t.Fatal(s, err)
				}
			} else {
				if err == nil {
					t.Fatal("failed route reported success")
				}
				if (outcome == "timeout") != (len(s.Pending) == 1) {
					t.Fatal(s, err)
				}
				if outcome != "timeout" && len(s.Added) != 0 {
					t.Fatal("foreign route was claimed", s)
				}
			}
		})
	}
}

func TestCompanyPendingIntentCannotBeRecoveredOrDeleted(t *testing.T) {
	dir := companyRecoveryFixture(t)
	path := filepath.Join(dir, "state.json")
	s := companyState{Device: "utun100", State: "cleanupFailed", Networks: []string{"10.20.0.0/16"}, Added: []string{"10.20.0.0/16"}, Pending: []string{"10.20.0.0/16"}}
	if err := saveCompanyState(path, s); err != nil {
		t.Fatal(err)
	}
	command := func(string, ...string) ([]byte, error) {
		t.Fatal("unconfirmed route must never be deleted")
		return nil, nil
	}
	if err := cleanupCompanyWith(path, s, command); err == nil {
		t.Fatal("pending cleanup succeeded")
	}
	if err := recoverCompanyDirectory(dir, uint32(os.Getuid()), nil, nil, command, func(string, []byte) error { t.Fatal("unsafe recovery"); return nil }); err == nil {
		t.Fatal("pending recovery succeeded")
	}
}

func TestBorrowedNonstaticScopedRouteIsHealthy(t *testing.T) {
	routes := []transparentRoute{{Prefix: "::/0", Interface: "en0", Gateway: "fe80::1%en0", Scoped: true}}
	if _, err := verifyTransparentRoutePlan(routes, func(string, ...string) ([]byte, error) { return []byte("default fe80::1%en0 UGcIg en0\n"), nil }); err != nil {
		t.Fatal(err)
	}
}

func companyRecoveryFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "company-123")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	state := companyState{Device: "utun100", State: "cleanupFailed", Networks: []string{"10.20.0.0/16", "fd12:3456::/48"}, Added: []string{"10.20.0.0/16", "fd12:3456::/48"}}
	if err := saveCompanyState(filepath.Join(dir, "state.json"), state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("PRIVATE_TOKEN"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCompanyRecoveryMissingReplacedAndExactRoutes(t *testing.T) {
	for _, table := range []string{"", "10.20/16 link#31 USc utun101\n", "10/8 link#30 USc utun100\n", "10.20/16 link#30 USc utun100\n"} {
		dir := companyRecoveryFixture(t)
		deletes := 0
		err := recoverCompanyDirectory(dir, uint32(os.Getuid()), nil, nil, func(name string, args ...string) ([]byte, error) {
			if name == "/sbin/route" {
				deletes++
				if strings.Join(args, " ") != "-n delete -inet -net 10.20.0.0/16 -interface utun100" {
					t.Fatal(args)
				}
				return nil, nil
			}
			if deletes > 0 {
				return nil, nil
			}
			return []byte(table), nil
		}, func(_ string, b []byte) error {
			if strings.Contains(string(b), "PRIVATE_TOKEN") {
				t.Fatal("backed up credentials")
			}
			var state companyState
			return json.Unmarshal(b, &state)
		})
		if err != nil {
			t.Fatal(err)
		}
		expected := 0
		if table == "10.20/16 link#30 USc utun100\n" {
			expected = 1
		}
		if deletes != expected {
			t.Fatal("incorrect route ownership", table, deletes)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("directory not released", err)
		}
	}
}

func TestCompanyRecoveryPreservesLiveUnsafeAndFailedSessions(t *testing.T) {
	for _, outcome := range []string{"live-core", "live-device", "backup-failed", "table-failed", "delete-failed", "linked-state", "invalid-state"} {
		t.Run(outcome, func(t *testing.T) {
			dir := companyRecoveryFixture(t)
			var processes []processIdentity
			var interfaces []net.Interface
			if outcome == "live-core" {
				processes = []processIdentity{{UID: 0, Path: serviceBase + "/versions/old/bin/mihomo"}}
			}
			if outcome == "live-device" {
				interfaces = []net.Interface{{Name: "utun100"}}
			}
			if outcome == "linked-state" {
				os.Rename(filepath.Join(dir, "state.json"), filepath.Join(dir, "old.json"))
				if err := os.Symlink("old.json", filepath.Join(dir, "state.json")); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "invalid-state" {
				os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"device":"en0","state":"ready"}`), 0600)
			}
			called := false
			err := recoverCompanyDirectory(dir, uint32(os.Getuid()), processes, interfaces, func(name string, args ...string) ([]byte, error) {
				called = true
				if outcome == "table-failed" || name == "/sbin/route" {
					return nil, errors.New("unavailable")
				}
				return []byte("10.20/16 link#30 USc utun100\nfd12:3456::/48 link#30 USc utun100\n"), nil
			}, func(string, []byte) error {
				if outcome == "backup-failed" {
					return errors.New("no backup")
				}
				return nil
			})
			if err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if _, e := os.Stat(dir); e != nil {
				t.Fatal("evidence removed", e)
			}
			if called && outcome != "table-failed" && outcome != "delete-failed" {
				t.Fatal("mutated before checks")
			}
		})
	}
}
