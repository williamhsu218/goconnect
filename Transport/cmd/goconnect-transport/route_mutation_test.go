package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestRouteAddVerifiesOwnershipAndActualMutation(t *testing.T) {
	for _, outcome := range []string{"success", "already-present", "collision-zero-exit", "zero-exit-no-effect", "timeout", "post-query-failed", "journal-write-failed"} {
		t.Run(outcome, func(t *testing.T) {
			state := transparentState{Device: "utun100", State: "starting"}
			dir := recoveryFixture(t, state)
			path := filepath.Join(dir, "state.json")
			if outcome == "journal-write-failed" {
				if err := os.Mkdir(path+".tmp", 0700); err != nil {
					t.Fatal(err)
				}
			}
			added := false
			mutations := 0
			row := "0/1 utun100 UScg utun100\n"
			command := func(name string, args ...string) ([]byte, error) {
				if name == "/sbin/route" {
					mutations++
					added = true
					switch outcome {
					case "collision-zero-exit":
						return []byte("add net 0.0.0.0/1: File exists\n"), nil
					case "timeout":
						return nil, context.DeadlineExceeded
					}
					return nil, nil
				}
				if outcome == "already-present" {
					return []byte(row), nil
				}
				if added {
					if outcome == "post-query-failed" {
						return nil, syscall.EIO
					}
					if outcome != "zero-exit-no-effect" {
						return []byte(row), nil
					}
				}
				return nil, nil
			}
			err := addTransparentRouteWith(path, &state, transparentRoute{Prefix: "0.0.0.0/1", Interface: "utun100"}, command)
			if outcome == "success" {
				if err != nil || len(state.Routes) != 1 || state.Routes[0].Pending {
					t.Fatal(state, err)
				}
			} else {
				if err == nil {
					t.Fatal("failed mutation reported success")
				}
				pending := outcome == "timeout" || outcome == "post-query-failed"
				if pending != (len(state.Routes) == 1 && state.Routes[0].Pending) {
					t.Fatal("incorrect ownership after uncertain mutation", state, err)
				}
				if !pending && len(state.Routes) != 0 {
					t.Fatal("claimed foreign or uncreated route", state)
				}
				if (outcome == "already-present" || outcome == "journal-write-failed") && mutations != 0 {
					t.Fatal("mutated before ownership/journal check")
				}
				if outcome == "collision-zero-exit" && !errors.Is(err, syscall.EEXIST) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPendingLateRouteCannotBeForgottenOrAutomaticallyCollected(t *testing.T) {
	state := transparentState{Device: "utun100", State: "cleanupFailed", Routes: []transparentRoute{{Prefix: "0.0.0.0/1", Interface: "utun100", Pending: true}}}
	dir := recoveryFixture(t, state)
	command := func(string, ...string) ([]byte, error) {
		t.Fatal("unknown late mutation must not be queried away or deleted")
		return nil, nil
	}
	if err := cleanupTransparentRoutesWith(filepath.Join(dir, "state.json"), &state, command); err == nil || len(state.Routes) != 1 {
		t.Fatal(state, err)
	}
	err := recoverTransparentDirectory(dir, uint32(os.Getuid()), nil, nil, command, func(string, []byte) error { t.Fatal("unsafe recovery started"); return nil })
	if err == nil {
		t.Fatal("pending mutation recovered")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("evidence removed", err)
	}
}

func TestCleanupVerifiesZeroExitAndKeepsRetryOrder(t *testing.T) {
	state := transparentState{Device: "utun100", State: "ready", Routes: []transparentRoute{{Prefix: "0.0.0.0/1", Interface: "utun100"}, {Prefix: "128.0.0.0/1", Interface: "utun100"}}}
	original := append([]transparentRoute(nil), state.Routes...)
	dir := recoveryFixture(t, state)
	var deleted []string
	command := func(name string, args ...string) ([]byte, error) {
		if name == "/sbin/route" {
			deleted = append(deleted, strings.Join(args, " "))
			return []byte("delete net: entry in use\n"), nil
		}
		return []byte("0/1 utun100 UScg utun100\n128/1 utun100 UScg utun100\n"), nil
	}
	for i := 0; i < 2; i++ {
		if err := cleanupTransparentRoutesWith(filepath.Join(dir, "state.json"), &state, command); err == nil {
			t.Fatal("zero-exit no-op treated as successful cleanup")
		}
		if !reflect.DeepEqual(state.Routes, original) {
			t.Fatal("retry reversed journal ordering", state)
		}
	}
	if deleted[0] != deleted[2] || deleted[1] != deleted[3] {
		t.Fatal("retry changed cleanup order", deleted)
	}
}

func TestNativeInterfaceGatewayRowsAreOwnedButForeignRowsAreNot(t *testing.T) {
	for _, tc := range []struct {
		prefix, row string
		want        bool
	}{
		{"0.0.0.0/1", "0/1 utun100 UScg utun100", true},
		{"::/1", "::/1 utun100 UScg utun100", true},
		{"::/1", "::/1 utun101 UScg utun100", false},
		{"::/1", "::/1 utun100 UGScg utun100", false},
		{"::/1", "::/1 utun100 UScIg utun100", false},
	} {
		if got := ownedTransparentRouteInTable([]byte(tc.row), transparentRoute{Prefix: tc.prefix, Interface: "utun100"}); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestScopedRouteCollisionUsesTheInterfaceScope(t *testing.T) {
	r := transparentRoute{Prefix: "::/0", Interface: "en0", Gateway: "fe80::1%en0", Scoped: true}
	if routeDestinationInTable([]byte("default fe80::1%en1 UGcIg en1\n"), r) {
		t.Fatal("another interface's scoped default blocks this interface")
	}
	if !routeDestinationInTable([]byte("default fe80::2%en0 UGcIg en0\n"), r) {
		t.Fatal("conflicting default on the same scope was ignored")
	}
}

func TestInitialJournalFailureDoesNotBlockLaterSessions(t *testing.T) {
	base := t.TempDir()
	for _, prefix := range []string{"transparent-", "company-"} {
		_, err := initializeSessionDirectory(base, prefix, func(path string) error { os.WriteFile(path, []byte("partial"), 0600); return syscall.ENOSPC })
		if !errors.Is(err, syscall.ENOSPC) {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(base)
		if err != nil || len(entries) != 0 {
			t.Fatal("unreadable journal directory leaked", entries, err)
		}
	}
}

func TestIPv6DiscoveryFailureStopsDirectRouteInstallation(t *testing.T) {
	state := transparentState{Device: "utun100", State: "starting"}
	dir := recoveryFixture(t, state)
	err := ensureTransparentDirectRoutesWith(filepath.Join(dir, "state.json"), &state, originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}, func(name string, args ...string) ([]byte, error) {
		if name != "/usr/sbin/netstat" || !reflect.DeepEqual(args, []string{"-rn", "-f", "inet6"}) {
			t.Fatal("mutation continued after unknown IPv6 state", name, args)
		}
		return nil, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) || len(state.Routes) != 0 {
		t.Fatal(state, err)
	}
}
