package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Synthetic kernel: native Darwin table formatting and real production route
// arguments. It is the only command boundary reachable by lifecycle tests.
type lifecycleNetwork struct {
	routes                map[string]transparentRoute
	gateway6              string
	adds, deletes         int
	noDelete, readFailure bool
	afterAdd              func(transparentRoute)
}

func lifecycleRouteKey(r transparentRoute) string {
	return fmt.Sprintf("%s|%s|%s|%t", r.Prefix, r.Interface, r.Gateway, r.Scoped)
}
func (n *lifecycleNetwork) command(name string, args ...string) ([]byte, error) {
	if name == "/usr/sbin/netstat" {
		if n.readFailure {
			return nil, context.DeadlineExceeded
		}
		family := args[len(args)-1]
		out := "default 192.168.1.1 UGScg en0\n"
		if family == "inet6" {
			out = "default " + n.gateway6 + " UGcg en0\n"
		}
		for _, r := range n.routes {
			if strings.TrimPrefix(routeFamily(r.Prefix), "-") != family {
				continue
			}
			dst, gw, flags := r.Prefix, r.Gateway, "US"
			if r.Scoped {
				dst = "default"
				flags += "GI"
			} else if gw != "" {
				dst = strings.Split(dst, "/")[0]
				flags += "GH"
			}
			if gw == "" {
				gw = r.Interface
			}
			out += fmt.Sprintf("%s %s %s %s\n", dst, gw, flags, r.Interface)
		}
		return []byte(out), nil
	}
	if name != "/sbin/route" {
		panic("unexpected real command boundary: " + name)
	}
	if args[1] == "get" {
		return []byte("interface: en0\n"), nil
	}
	r := transparentRoute{Interface: "en0"}
	for i, arg := range args {
		switch arg {
		case "-net":
			r.Prefix = args[i+1]
		case "-host":
			r.Prefix = args[i+1] + "/32"
			if args[2] == "-inet6" {
				r.Prefix = args[i+1] + "/128"
			}
		case "-ifscope":
			r.Scoped = true
			r.Interface = args[i+1]
		case "-interface":
			r.Interface = args[i+1]
		}
	}
	if r.Scoped || r.Interface == "en0" {
		r.Gateway = args[len(args)-1]
	}
	key := lifecycleRouteKey(r)
	if args[1] == "add" {
		n.adds++
		n.routes[key] = r
		if n.afterAdd != nil {
			n.afterAdd(r)
		}
	} else if args[1] == "delete" {
		n.deletes++
		if n.noDelete && r.Prefix == "0.0.0.0/1" {
			return []byte("delete net: entry in use"), nil
		}
		delete(n.routes, key)
	} else {
		panic("unexpected route action")
	}
	return nil, nil
}

func TestTransparentProductionLifecycleFaults(t *testing.T) {
	for _, outcome := range []string{"disconnect", "lease-revoked", "cancel-during-route", "lease-revoked-during-route", "network-changed-before-ready", "ipv6-next-hop-changed", "capture-missing", "borrowed-direct-missing", "core-exit", "core-health-failed", "intermittent-health", "startup-timeout", "core-start-failed", "config-write-failed", "cleanup-no-effect", "cleanup-query-timeout", "directory-remove-failed", "core-stop-failed", "panic-after-ready"} {
		t.Run(outcome, func(t *testing.T) {
			controlDir := controlFixture(t)
			control, err := openControl(filepath.Join(controlDir, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer control.close()
			control.request.TransparentRouting = true
			control.request.LiveRouting = true
			control.request.AppPaths = nil
			control.request.Gateway = "2001:db8::20"
			state := transparentState{Device: "utun100", State: "starting"}
			dir := recoveryFixture(t, state)
			path := filepath.Join(dir, "state.json")
			if outcome == "config-write-failed" {
				os.Remove(filepath.Join(dir, "config.json"))
				os.Mkdir(filepath.Join(dir, "config.json"), 0700)
			}
			borrowed := transparentRoute{Prefix: "0.0.0.0/0", Interface: "en0", Gateway: "192.168.1.1", Scoped: true}
			network := &lifecycleNetwork{routes: map[string]transparentRoute{lifecycleRouteKey(borrowed): borrowed}, gateway6: "fe80::1%en0"}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var records []diagnosticRecord
			log := newSessionExitDiagnostics()
			log.emit = func(event string, value int64, d diagnosticDetails) {
				records = append(records, diagnosticRecord{Event: event, Value: value, Details: &d})
			}
			var core *exec.Cmd
			var coreDone <-chan error
			readiness, polls, clock := 0, 0, 0
			network.afterAdd = func(r transparentRoute) {
				if r.Prefix == "0.0.0.0/1" {
					if outcome == "cancel-during-route" {
						cancel()
					}
					if outcome == "lease-revoked-during-route" {
						os.Remove(filepath.Join(controlDir, "lease"))
					}
				}
			}
			ops := defaultTransparentRuntime()
			ops.command = network.command
			if outcome == "directory-remove-failed" {
				ops.removeDirectory = func(string) error { return syscall.EACCES }
			}
			ops.now = func() time.Time { clock++; return time.Unix(1000, 0).Add(time.Duration(clock) * 6 * time.Second) }
			ops.ticks = func() (<-chan time.Time, func()) {
				tick := time.NewTicker(2 * time.Millisecond)
				return tick.C, tick.Stop
			}
			ops.network = func(bool) (originalNetwork, error) {
				if outcome == "network-changed-before-ready" {
					return originalNetwork{Interface: "en1", Gateway: "192.168.2.1"}, nil
				}
				return originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}, nil
			}
			ops.startCore = func(string, string, string) (*exec.Cmd, error) {
				if outcome == "core-start-failed" {
					return nil, syscall.EIO
				}
				core = exec.Command("/bin/sleep", "60")
				core.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				return core, core.Start()
			}
			ops.stopCore = func(cmd *exec.Cmd, done <-chan error) error {
				coreDone = done
				if outcome == "core-stop-failed" {
					return syscall.EIO
				}
				return stopChild(cmd, done)
			}
			defer func() {
				if outcome == "core-stop-failed" && core != nil {
					_ = stopChild(core, coreDone)
				}
			}()
			ops.readiness = func(*http.Client, uint16, string, string) (bool, diagnosticDetails) {
				readiness++
				if outcome == "startup-timeout" || ((outcome == "core-health-failed" || outcome == "intermittent-health") && readiness > 1) {
					if outcome == "intermittent-health" && readiness == 4 {
						return true, diagnosticDetails{Reason: "success"}
					}
					return false, diagnosticDetails{Reason: "tun_missing"}
				}
				return true, diagnosticDetails{Reason: "success"}
			}
			ops.poll = func(c *controlFiles, _ *http.Client, _ uint16) error {
				polls++
				c.write("ready")
				if polls > 1 {
					return nil
				}
				switch outcome {
				case "disconnect", "cleanup-no-effect", "cleanup-query-timeout", "directory-remove-failed", "core-stop-failed":
					network.noDelete = outcome == "cleanup-no-effect"
					network.readFailure = outcome == "cleanup-query-timeout"
					cancel()
				case "lease-revoked":
					os.Remove(filepath.Join(controlDir, "lease"))
				case "ipv6-next-hop-changed":
					network.gateway6 = "fe80::2%en0"
				case "capture-missing":
					delete(network.routes, lifecycleRouteKey(transparentRoute{Prefix: "0.0.0.0/1", Interface: "utun100"}))
				case "borrowed-direct-missing":
					delete(network.routes, lifecycleRouteKey(borrowed))
				case "core-exit":
					core.Process.Kill()
				case "panic-after-ready":
					panic("SECRET_PANIC")
				}
				return nil
			}
			connection := transparentConnection{control: control, port: 19091, original: originalNetwork{Interface: "en0", Gateway: "192.168.1.1"}, outer: originalNetwork{Interface: "en0", Gateway: "fe80::1%en0"}, device: "utun100", statePath: path, state: &state, exitLog: log, ops: ops}
			var recovered any
			var terminalError error
			result := func() (result error) {
				defer func() { recovered = recover() }()
				defer log.complete(&result)
				defer func() { terminalError = result }()
				defer control.finishSession(&result)
				result = connection.run(ctx)
				return result
			}()
			if len(records) < 2 {
				t.Fatal("lifecycle lost exit chain", records, result)
			}
			first, final := records[0].Details, records[len(records)-1].Details
			want := "context_canceled"
			switch outcome {
			case "lease-revoked", "lease-revoked-during-route":
				want = "lease_missing"
			case "network-changed-before-ready":
				want = "network_changed"
			case "ipv6-next-hop-changed":
				want = "gateway_changed"
			case "capture-missing", "borrowed-direct-missing":
				want = "route_missing"
			case "core-exit":
				want = "core_exited"
			case "core-health-failed", "intermittent-health":
				want = "core_unresponsive"
			case "startup-timeout":
				want = "core_start_timeout"
			case "core-start-failed", "config-write-failed":
				want = "operation_failed"
			case "panic-after-ready":
				want = "panic"
			}
			if first.Reason != want || final.Reason != want {
				t.Fatalf("wrong first/final cause: %+v %+v, %v", first, final, result)
			}
			retained := outcome == "cleanup-no-effect" || outcome == "cleanup-query-timeout" || outcome == "directory-remove-failed" || outcome == "core-stop-failed"
			_, statErr := os.Stat(dir)
			if retained == os.IsNotExist(statErr) {
				t.Fatal("wrong journal retention", outcome, statErr)
			}
			if outcome == "core-stop-failed" && !final.StopFailed {
				t.Fatal("unconfirmed core death was lost")
			}
			if (outcome == "cleanup-no-effect" || outcome == "cleanup-query-timeout" || outcome == "directory-remove-failed") && (final.CleanupFailures == 0 || result == nil) {
				t.Fatal("cleanup failure reported success", final, result)
			}
			if outcome == "intermittent-health" && readiness != 7 {
				t.Fatal("healthy check did not reset failure counter", readiness)
			}
			if outcome == "panic-after-ready" && recovered != "SECRET_PANIC" {
				t.Fatal("panic swallowed", recovered)
			}
			if outcome == "panic-after-ready" && terminalError == nil {
				t.Fatal("panic would publish a successful stopped status")
			}
			snapshot, snapshotErr := readRouteSnapshot(filepath.Join(controlDir, "status"))
			wantState := "stopped"
			if terminalError != nil {
				wantState = "failed"
			}
			if snapshotErr != nil || snapshot.State != wantState {
				t.Fatal("wrong terminal status", snapshot.State, wantState, snapshotErr)
			}
			if !retained && len(state.Routes) != 0 {
				t.Fatal("owned routes retained after clean exit", state)
			}
			if outcome != "borrowed-direct-missing" {
				if _, ok := network.routes[lifecycleRouteKey(borrowed)]; !ok {
					t.Fatal("cleanup deleted borrowed system route")
				}
			}
			if outcome == "cancel-during-route" || outcome == "lease-revoked-during-route" {
				if network.adds != 3 {
					t.Fatal("continued mutation after revocation", network.adds)
				}
			}
			assertDiagnosticPrivate(t, records)
		})
	}
}
