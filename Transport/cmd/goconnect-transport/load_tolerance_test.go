package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestLeaseLoadToleranceAndOwnerProtection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		age   time.Duration
		owner string
		want  string
	}{
		{"incident-5905ms", 5905 * time.Millisecond, "alive", ""},
		{"delayed-20s", 20 * time.Second, "alive", ""},
		{"stopped-renewal", 31 * time.Second, "alive", "lease_expired"},
		{"dead-owner-fresh", time.Second, "missing", "owner_missing"},
		{"dead-owner-delayed", 5905 * time.Millisecond, "missing", "owner_missing"},
		{"reused-pid-delayed", 5905 * time.Millisecond, "changed", "owner_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := controlFixture(t)
			c, err := openControl(filepath.Join(dir, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.close()
			stamp := time.Now().Add(-tc.age)
			if err = os.Chtimes(filepath.Join(dir, "lease"), stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if tc.owner == "missing" {
				c.request.OwnerPID = 1 << 30
			}
			if tc.owner == "changed" {
				c.started.Sec--
			}
			d, _ := c.leaseDiagnostics()
			if d.Reason != tc.want {
				t.Fatalf("got %+v, want %q", d, tc.want)
			}
		})
	}
}

// Execute the production lifecycle and unwind with synthetic routing only.
// Clock acceleration models sustained contention without overloading the host.
func TestTransparentHighLoadTimeouts(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_CPU_PRESSURE") == "1" {
		// Opt-in process-local contention. At most two Go execution threads;
		// launch with nice(19), never saturate the machine hosting the real VPN.
		previous := runtime.GOMAXPROCS(2)
		stop := make(chan struct{})
		var workers sync.WaitGroup
		var work atomic.Uint64
		for i := 0; i < 8; i++ {
			workers.Add(1)
			go func(seed uint64) {
				defer workers.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					for j := 0; j < 4096; j++ {
						seed ^= seed << 13
						seed ^= seed >> 7
						seed ^= seed << 17
					}
					work.Add(seed)
				}
			}(uint64(i + 1))
		}
		t.Cleanup(func() {
			close(stop)
			workers.Wait()
			runtime.GOMAXPROCS(previous)
			t.Logf("CPU pressure drained, work checksum %d", work.Load())
		})
		t.Log("CPU pressure enabled: 8 busy workers, GOMAXPROCS=2")
	}
	for _, scenario := range []string{
		"gateway-recovers", "network-recovers", "route-table-recovers", "controller-recovers", "intermittent-recovers",
		"gateway-stays-blocked", "alternating-timeouts", "controller-stays-blocked", "query-error",
		"timeout-then-revoke", "timeout-then-owner-exit", "timeout-then-network-change", "timeout-then-gateway-change",
		"timeout-then-token-change", "timeout-then-route-missing", "timeout-then-core-exit", "timeout-then-cancel",
		"real-deadline-late-success",
	} {
		t.Run(scenario, func(t *testing.T) {
			dir := controlFixture(t)
			c, err := openControl(filepath.Join(dir, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.close()
			c.request.TransparentRouting, c.request.LiveRouting = true, true
			c.request.AppPaths, c.request.Gateway = nil, "2001:db8::20"
			state := transparentState{Device: "utun100", State: "starting"}
			stateDir := recoveryFixture(t, state)
			n := &lifecycleNetwork{routes: map[string]transparentRoute{}, gateway6: "fe80::1%en0"}
			realDeadline := scenario == "real-deadline-late-success"
			testLimit := 2 * time.Second
			if realDeadline {
				testLimit = 25 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), testLimit)
			defer cancel()
			log := newSessionExitDiagnostics()
			var records []diagnosticRecord
			log.emit = func(e string, v int64, d diagnosticDetails) {
				records = append(records, diagnosticRecord{Event: e, Value: v, Details: &d})
			}
			ops := defaultTransparentRuntime()
			clock, polls, attempt, faults := 0, 0, 0, 0
			active, cleaning := false, false
			var core *exec.Cmd
			lateEvents := make(chan diagnosticRecord, 4)
			ops.now = func() time.Time { clock++; return time.Unix(1000, 0).Add(time.Duration(clock) * 3 * time.Second) }
			if realDeadline {
				ops.now = time.Now
			}
			ops.ticks = func() (<-chan time.Time, func()) { tick := time.NewTicker(time.Millisecond); return tick.C, tick.Stop }
			shouldFail := func(kind string) bool {
				if !active || cleaning {
					return false
				}
				switch scenario {
				case "network-recovers":
					return kind == "network" && faults == 0
				case "route-table-recovers":
					return kind == "table" && faults == 0
				case "controller-recovers":
					return kind == "controller" && faults == 0
				case "controller-stays-blocked":
					return kind == "controller"
				case "gateway-stays-blocked", "query-error":
					return kind == "gateway"
				case "alternating-timeouts":
					return (attempt%2 == 1 && kind == "network") || (attempt%2 == 0 && kind == "controller")
				case "intermittent-recovers":
					return kind == "gateway" && attempt%2 == 1 && faults < 3
				default:
					return kind == "gateway" && faults == 0
				}
			}
			fail := func() error {
				faults++
				switch scenario {
				case "timeout-then-revoke":
					os.Remove(filepath.Join(dir, "lease"))
				case "timeout-then-owner-exit":
					c.request.OwnerPID = 1 << 30
				case "timeout-then-gateway-change":
					n.gateway6 = "fe80::2%en0"
				case "timeout-then-token-change":
					os.WriteFile(filepath.Join(dir, "lease"), []byte("revoked-token"), 0600)
				case "timeout-then-route-missing":
					delete(n.routes, lifecycleRouteKey(transparentRoute{Prefix: "0.0.0.0/1", Interface: "utun100"}))
				case "timeout-then-core-exit":
					core.Process.Kill()
				case "timeout-then-cancel":
					cancel()
				}
				if scenario == "query-error" {
					return syscall.EACCES
				}
				return context.DeadlineExceeded
			}
			ops.command = func(name string, args ...string) ([]byte, error) {
				if name == "/sbin/route" && args[1] == "delete" {
					cleaning = true
				}
				if name == "/sbin/route" && args[1] == "get" && shouldFail("gateway") {
					if realDeadline {
						faults++
						// Replay the actual 6 s deadline and 17.643 s late success.
						// The blocked boundary cannot execute any host routing command.
						return runSystemCommand(name, args, 6*time.Second, func(context.Context) ([]byte, error) {
							time.Sleep(17643 * time.Millisecond)
							return []byte("interface: en0\n"), nil
						}, func(e string, v int64, d diagnosticDetails) {
							lateEvents <- diagnosticRecord{Event: e, Value: v, Details: &d}
						})
					}
					return nil, fail()
				}
				if name == "/usr/sbin/netstat" && args[len(args)-1] == "inet" && shouldFail("table") {
					return nil, fail()
				}
				return n.command(name, args...)
			}
			ops.network = func(bool) (originalNetwork, error) {
				if active {
					attempt++
				}
				if shouldFail("network") {
					return originalNetwork{}, fail()
				}
				if scenario == "timeout-then-network-change" && faults > 0 {
					return originalNetwork{Interface: "en1", Gateway: "192.168.2.1"}, nil
				}
				return originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}, nil
			}
			ops.startCore = func(string, string, string) (*exec.Cmd, error) {
				cmd := exec.Command("/bin/sleep", "60")
				core = cmd
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				return cmd, cmd.Start()
			}
			ops.readiness = func(*http.Client, uint16, string, string) (bool, diagnosticDetails) {
				if shouldFail("controller") {
					_ = fail()
					return false, diagnosticDetails{Reason: "controller_timeout", TimeoutMS: 1000}
				}
				return true, diagnosticDetails{Reason: "success"}
			}
			ops.poll = func(*controlFiles, *http.Client, uint16) error {
				active = true
				polls++
				if polls >= 2 && (scenario == "gateway-recovers" || scenario == "network-recovers" || scenario == "route-table-recovers" || scenario == "controller-recovers" || realDeadline) {
					cancel()
				}
				if scenario == "intermittent-recovers" && faults == 3 {
					cancel()
				}
				return nil
			}
			ops.stopCore = func(cmd *exec.Cmd, done <-chan error) error { cleaning = true; return stopChild(cmd, done) }
			connection := transparentConnection{control: c, port: 19091, original: originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}, outer: originalNetwork{Interface: "en0", Gateway: "fe80::1%en0"}, device: "utun100", statePath: filepath.Join(stateDir, "state.json"), state: &state, exitLog: log, ops: ops}
			result := connection.run(ctx)
			if log.first == nil {
				t.Fatal("missing primary diagnostic")
			}
			want := "context_canceled"
			switch scenario {
			case "gateway-stays-blocked", "query-error":
				want = "gateway_query_failed"
			case "alternating-timeouts":
				want = "network_query_failed"
				if log.first.Stage == "health_core" {
					want = "core_unresponsive"
				}
			case "controller-stays-blocked":
				want = "core_unresponsive"
			case "timeout-then-revoke":
				want = "lease_missing"
			case "timeout-then-owner-exit":
				want = "owner_missing"
			case "timeout-then-network-change":
				want = "network_changed"
			case "timeout-then-gateway-change":
				want = "gateway_changed"
			case "timeout-then-token-change":
				want = "lease_token_mismatch"
			case "timeout-then-route-missing":
				want = "route_missing"
			case "timeout-then-core-exit":
				want = "core_exited"
			}
			if log.first.Reason != want {
				t.Fatalf("%s: got %+v want %s (faults=%d polls=%d, result=%v)", scenario, *log.first, want, faults, polls, result)
			}
			if faults == 0 {
				t.Fatal("fault injection did not execute")
			}
			if want == "context_canceled" && scenario != "timeout-then-cancel" && (polls < 2 || result != nil) {
				t.Fatal("did not remain connected through transient load", polls, result)
			}
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatal("protection did not terminate within test bound")
			}
			if len(state.Routes) != 0 {
				t.Fatal("synthetic owned routes not cleaned", state)
			}
			if scenario == "gateway-stays-blocked" || scenario == "alternating-timeouts" || scenario == "controller-stays-blocked" {
				if faults < 2 || clock*3 > 90 {
					t.Fatal("retry window absent or protection not bounded", faults, clock)
				}
			}
			if realDeadline {
				first := <-lateEvents
				if first.Event != "system.command_timeout" || first.Details.TimeoutMS != 6000 {
					t.Fatal("actual deadline not exercised", first)
				}
				select {
				case late := <-lateEvents:
					if late.Event != "system.command_late_result" || late.Details.Check != "success" || late.Details.OperationID != first.Details.OperationID || late.Details.DurationMS < 17643 {
						t.Fatal("late success lost", late)
					}
				case <-time.After(20 * time.Second):
					t.Fatal("late result did not arrive")
				}
			}
			assertDiagnosticPrivate(t, records)
		})
	}
}

func TestHealthTimeoutWindowAndMutationProtection(t *testing.T) {
	start := time.Now()
	var window healthTimeoutWindow
	if !window.retry(start, start.Add(6*time.Second)) || !window.retry(start.Add(10*time.Second), start.Add(29*time.Second)) || window.retry(start.Add(29*time.Second), start.Add(30*time.Second)) {
		t.Fatal("timeout window extended or closed early")
	}
	window.reset()
	if !window.retry(start.Add(time.Minute), start.Add(time.Minute+6*time.Second)) {
		t.Fatal("complete recovery did not reset allowance")
	}
	for _, command := range []string{"route.get4", "route.get6", "netstat.inet", "netstat.inet6", "route.add4", "route.delete4", "unknown"} {
		err := &systemCommandFailure{details: diagnosticDetails{Command: command, Reason: "system_timeout"}, cause: context.DeadlineExceeded}
		want := command == "route.get4" || command == "route.get6" || command == "netstat.inet" || command == "netstat.inet6"
		if readOnlyProbeTimeout(err) != want {
			t.Fatal("mutation can be retried or read probe cannot", command)
		}
	}
	for _, err := range []error{context.Canceled, syscall.EACCES, errors.New("gateway changed")} {
		if readOnlyProbeTimeout(err) {
			t.Fatal("definitive failure deferred", err)
		}
	}
}
