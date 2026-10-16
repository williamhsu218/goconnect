package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func assertDiagnosticPrivate(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET", "203.0.113.5", "192.0.2.10", "https://", "/private/customer"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("private data in diagnostic: %s", data)
		}
	}
}

func TestSystemCommandFailureMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		code, signal int
	}{
		{"exit", "printf SECRET_STDOUT; printf SECRET_STDERR >&2; exit 23", 23, 0},
		{"signal", "kill -TERM $$", -1, int(syscall.SIGTERM)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var records []diagnosticDetails
			_, err := runSystemCommand("/sbin/route", []string{"-n", "delete", "203.0.113.5", "SECRET_ARG"}, time.Second,
				func(ctx context.Context) ([]byte, error) {
					return exec.CommandContext(ctx, "/bin/sh", "-c", tc.script).Output()
				},
				func(_ string, _ int64, d diagnosticDetails) { records = append(records, d) })
			d := diagnosticError(err)
			if err == nil || d.ExitCode == nil || *d.ExitCode != tc.code || d.Signal != tc.signal || d.Command != "route.delete4" || d.OperationID == 0 || d.TimeoutMS != 1000 || len(records) != 1 {
				t.Fatalf("missing actual process result: %+v, %v", d, err)
			}
			assertDiagnosticPrivate(t, records)
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("raw error exposed")
			}
		})
	}
}

func TestSystemCommandTimeoutAndLateResult(t *testing.T) {
	release := make(chan struct{})
	events := make(chan diagnosticRecord, 4)
	start := time.Now()
	data, err := runSystemCommand("/usr/sbin/netstat", []string{"-rn", "-f", "inet6", "SECRET"}, 20*time.Millisecond,
		func(context.Context) ([]byte, error) { <-release; return []byte("SECRET_OUTPUT"), syscall.EIO },
		func(code string, _ int64, d diagnosticDetails) { events <- diagnosticRecord{Event: code, Details: &d} })
	close(release)
	if data != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("deadline did not bound the caller: %v", err)
	}
	var records []diagnosticRecord
	for len(records) < 2 {
		select {
		case r := <-events:
			records = append(records, r)
		case <-time.After(3 * time.Second):
			t.Fatal("late result lost")
		}
	}
	first, late := records[0], records[1]
	if first.Event != "system.command_timeout" || late.Event != "system.command_late_result" || first.Details.OperationID == 0 || first.Details.OperationID != late.Details.OperationID || first.Details.Command != "netstat.inet6" || first.Details.DurationMS < 15 || first.Details.TimeoutMS != 20 || late.Details.Errno != int(syscall.EIO) || late.Details.Check != "command_failed" {
		t.Fatalf("timeout correlation missing: %+v", records)
	}
	assertDiagnosticPrivate(t, records)
}

func TestSystemCommandLateSuccessHasExplicitExitCode(t *testing.T) {
	release := make(chan struct{})
	late := make(chan diagnosticDetails, 1)
	_, err := runSystemCommand("/sbin/route", []string{"get"}, 15*time.Millisecond,
		func(context.Context) ([]byte, error) { <-release; return nil, nil },
		func(event string, _ int64, d diagnosticDetails) {
			if event == "system.command_late_result" {
				late <- d
			}
		})
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout fixture did not time out", err)
	}
	select {
	case d := <-late:
		if d.Check != "success" || d.ExitCode == nil || *d.ExitCode != 0 {
			t.Fatal("late success indistinguishable from missing result", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late success lost")
	}
}

func TestSystemCommandSlowSuccess(t *testing.T) {
	var event string
	var details diagnosticDetails
	output, err := runSystemCommand("/sbin/ifconfig", nil, 3*time.Second,
		func(context.Context) ([]byte, error) {
			time.Sleep(1050 * time.Millisecond)
			return []byte("SECRET_OUTPUT"), nil
		},
		func(code string, _ int64, d diagnosticDetails) { event, details = code, d })
	if err != nil || string(output) != "SECRET_OUTPUT" || event != "system.command_slow" || details.DurationMS < 1000 || details.Command != "ifconfig" {
		t.Fatalf("slow success lost: %v %+v", err, details)
	}
	assertDiagnosticPrivate(t, details)
}

func TestSessionExitPreservesFirstCauseAfterCleanupFailure(t *testing.T) {
	var records []diagnosticRecord
	d := newSessionExitDiagnostics()
	d.emit = func(code string, value int64, fields diagnosticDetails) {
		records = append(records, diagnosticRecord{Event: code, Value: value, Details: &fields})
	}
	d.stage = "health_network"
	primary := &systemCommandFailure{details: diagnosticDetails{Reason: "system_timeout", Command: "route.get4", OperationID: 17, DurationMS: 6011, TimeoutMS: 6000}, cause: context.DeadlineExceeded}
	d.fail("network_query_failed", primary)
	d.beforeCleanup(errors.New("SECRET_OTHER_FAILURE"))
	d.cleanupFailures, d.stopFailed = 1, true
	d.cleanupEvent("transparent.route_cleanup_failed", 0, diagnosticDetails{Stage: "cleanup", Reason: "read_failed", Command: "netstat.inet", Errno: int(syscall.EIO)})
	d.finish(errors.New("SECRET_CLEANUP_FAILURE"))
	if len(records) != 3 {
		t.Fatal(records)
	}
	first, final := records[0].Details, records[2].Details
	if first.Stage != "health_network" || first.Reason != "network_query_failed" || first.Check != "system_timeout" || first.OperationID != 17 || first.TimeoutMS != 6000 || first.CleanupFailures != 0 || final.Reason != first.Reason || final.Check != first.Check || final.CleanupFailures != 1 || !final.StopFailed || len(final.Cleanup) != 1 || final.Cleanup[0].Errno != int(syscall.EIO) {
		t.Fatalf("cleanup overwrote first cause: %+v %+v", first, final)
	}
	assertDiagnosticPrivate(t, records)
}

func TestSessionPanicRetainedWithoutChangingPropagation(t *testing.T) {
	for _, beforeCleanupRegistered := range []bool{true, false} {
		var records []diagnosticDetails
		d := newSessionExitDiagnostics()
		d.stage = "core_ready"
		d.emit = func(_ string, _ int64, fields diagnosticDetails) { records = append(records, fields) }
		cleaned := false
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			func() (result error) {
				defer d.complete(&result)
				if beforeCleanupRegistered {
					panic("SECRET_PANIC")
				}
				defer d.unwind(&result, func() {
					cleaned = true
					d.cleanupFailures = 1
					result = errors.New("SECRET_CLEANUP")
				})
				panic("SECRET_PANIC")
			}()
		}()
		if recovered != "SECRET_PANIC" || cleaned == beforeCleanupRegistered || len(records) != 2 || records[0].Reason != "panic" || records[1].Reason != "panic" || records[0].Stage != "core_ready" {
			t.Fatalf("panic propagation or first cause changed: %v %v %+v", recovered, cleaned, records)
		}
		assertDiagnosticPrivate(t, records)
	}
}

func TestCleanupFailureDiagnosticsDistinguishStages(t *testing.T) {
	for _, stage := range []string{"read_failed", "recheck_failed", "journal_write_failed"} {
		t.Run(stage, func(t *testing.T) {
			s := transparentState{Device: "utun100", State: "ready", Routes: []transparentRoute{{Prefix: "8000::/1", Interface: "utun100"}}}
			dir := recoveryFixture(t, s)
			path := filepath.Join(dir, "state.json")
			if stage == "journal_write_failed" {
				path = filepath.Join(dir, "missing", "state.json")
				s.Routes = nil
			}
			var records []diagnosticDetails
			deleted := false
			err := cleanupTransparentRoutesWithDiagnostics(path, &s, func(name string, _ ...string) ([]byte, error) {
				if name == "/sbin/route" {
					deleted = true
					return nil, syscall.EPERM
				}
				if stage == "read_failed" || deleted {
					return nil, &systemCommandFailure{details: diagnosticDetails{Command: "netstat.inet6", Reason: "system_timeout", OperationID: 31, DurationMS: 6001, TimeoutMS: 6000}, cause: context.DeadlineExceeded}
				}
				return []byte("8000::/1 link#30 USc utun100\n"), nil
			}, func(_ string, _ int64, d diagnosticDetails) { records = append(records, d) })
			if err == nil || len(records) == 0 {
				t.Fatal("failure fixture was not exercised")
			}
			last := records[len(records)-1]
			if last.Stage != "cleanup" || last.Reason != stage {
				t.Fatal("failure stage lost", records)
			}
			if stage == "journal_write_failed" {
				if last.Errno != int(syscall.ENOENT) {
					t.Fatal("journal errno lost", last)
				}
			} else if last.Check != "system_timeout" || last.Command != "netstat.inet6" || last.OperationID != 31 || last.Family != 6 || last.RouteIndex == nil || *last.RouteIndex != 0 || last.Interface != "utun100" {
				t.Fatal("route failure metadata lost", last)
			}
			if stage == "recheck_failed" && (len(records) != 2 || records[0].Reason != "delete_failed" || records[0].Errno != int(syscall.EPERM)) {
				t.Fatal("delete and recheck failures conflated", records)
			}
			assertDiagnosticPrivate(t, records)
		})
	}
}

func TestDiagnosticSanitizationAndPriority(t *testing.T) {
	l := &diagnosticLog{queue: make(chan diagnosticRecord, 1), critical: make(chan diagnosticRecord, 2)}
	l.record("process.heartbeat", 0)
	l.record("process.heartbeat", 0)
	code, index, bits, scoped := 9, 0, 32, true
	d := diagnosticDetails{Stage: "SECRET_STAGE", Reason: "SECRET_REASON", Check: "https://SECRET", Command: "/private/customer/SECRET", Interface: "SECRET", ExpectedInterface: "en8", ActualInterface: "utun100", ExitCode: &code, RouteIndex: &index, PrefixBits: &bits, Scoped: &scoped,
		Cleanup: []diagnosticDetails{{Reason: "SECRET", Cleanup: []diagnosticDetails{{Reason: "SECRET_NESTED"}}}}}
	l.recordDetails("session.exit_first", 0, d)
	l.recordDetails("session.exit_final", 0, d)
	code, index, bits, scoped = 99, 99, 99, false
	if len(l.queue) != 1 || len(l.critical) != 2 || l.dropped.Load() != 1 {
		t.Fatal("ordinary pressure consumed reserved exit slots")
	}
	for len(l.critical) > 0 {
		r := <-l.critical
		if r.Details.Stage != "unknown" || r.Details.Interface != "" || r.Details.ExpectedInterface != "en8" || r.Details.ActualInterface != "utun100" || *r.Details.ExitCode != 9 || *r.Details.RouteIndex != 0 || *r.Details.PrefixBits != 32 || !*r.Details.Scoped || len(r.Details.Cleanup[0].Cleanup) != 0 {
			t.Fatalf("unsafe or aliased metadata: %+v", r.Details)
		}
		assertDiagnosticPrivate(t, r)
	}
}

func TestOriginalNetworkRetainsTimeoutCause(t *testing.T) {
	commands := 0
	_, err := originalPhysicalNetworkWith(true, func() ([]net.Interface, error) {
		return []net.Interface{{Name: "en0", Flags: net.FlagUp}, {Name: "en8", Flags: net.FlagUp}}, nil
	}, func(string, ...string) ([]byte, error) {
		commands++
		if commands == 1 {
			return nil, &systemCommandFailure{details: diagnosticDetails{Reason: "system_timeout", Command: "route.get4", OperationID: 23}, cause: context.DeadlineExceeded}
		}
		return nil, errors.New("SECRET_LATER_ERROR")
	})
	if commands != 2 || diagnosticError(err).OperationID != 23 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("interface fallback hid the original command timeout", err)
	}
}

// Exercise the entire causal chain with a blocked fake system command and
// synthetic route table; no privileged service, interface or real route changes.
func TestDiagnosticTimeoutCleanupSnapshot(t *testing.T) {
	directory := t.TempDir()
	l, err := startDiagnosticLog(directory, "worker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.close)
	d := newSessionExitDiagnostics()
	d.emit = l.recordDetails
	d.stage = "health_gateway"
	release, completed := make(chan struct{}), make(chan struct{})
	_, primary := runSystemCommand("/sbin/route", []string{"get", "192.0.2.10"}, 15*time.Millisecond,
		func(context.Context) ([]byte, error) { defer close(completed); <-release; return nil, syscall.EIO }, l.recordDetails)
	close(release)
	<-completed
	d.fail("gateway_query_failed", primary)
	d.beforeCleanup(primary)
	s := transparentState{Device: "utun100", State: "ready", Routes: []transparentRoute{{Prefix: "0.0.0.0/0", Interface: "en8", Gateway: "192.0.2.10", Scoped: true}}}
	session := recoveryFixture(t, s)
	cleanupErr := cleanupTransparentRoutesWithDiagnostics(filepath.Join(session, "state.json"), &s, func(name string, _ ...string) ([]byte, error) {
		if name == "/sbin/route" {
			return nil, syscall.EPERM
		}
		return []byte("default 192.0.2.10 UGScIg en8\n"), nil
	}, d.cleanupEvent)
	if cleanupErr == nil {
		t.Fatal("fixture did not fail cleanup")
	}
	d.cleanupFailures = len(s.Routes)
	d.finish(cleanupErr)
	l.close()
	select {
	case <-l.done:
	case <-time.After(3 * time.Second):
		t.Fatal("diagnostic writer did not finish")
	}
	// Rotate all ordinary records out after removing the synthetic session.
	if err = os.RemoveAll(session); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "worker.jsonl")
	for i := 0; i < 3; i++ {
		if err = appendDiagnostic(path, []byte("{}\n"), 4); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "worker.exit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var final diagnosticRecord
	if err = json.Unmarshal(data, &final); err != nil {
		t.Fatal(err)
	}
	f := final.Details
	if final.Event != "session.exit_final" || f.Reason != "gateway_query_failed" || f.Check != "system_timeout" || f.Command != "route.get4" || f.OperationID == 0 || f.CleanupFailures != 1 || len(f.Cleanup) != 1 {
		t.Fatalf("causal chain not retained: %s", data)
	}
	c := f.Cleanup[0]
	if c.Reason != "delete_still_present" || c.Errno != int(syscall.EPERM) || c.Interface != "en8" || c.RouteIndex == nil || *c.RouteIndex != 0 || c.Family != 4 || c.PrefixBits == nil || *c.PrefixBits != 0 || c.Scoped == nil || !*c.Scoped {
		t.Fatalf("cleanup failure cannot be located: %+v", c)
	}
	assertDiagnosticPrivate(t, final)
}
