package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// The same lifecycle runs in production and isolated fault-injection tests.
// Only the OS/network boundary is substituted in tests; session control, route
// journal, readiness transitions, exit diagnostics and unwind remain real.
type transparentRuntime struct {
	command         func(string, ...string) ([]byte, error)
	network         func(bool) (originalNetwork, error)
	readiness       func(*http.Client, uint16, string, string) (bool, diagnosticDetails)
	startCore       func(string, string, string) (*exec.Cmd, error)
	stopCore        func(*exec.Cmd, <-chan error) error
	ticks           func() (<-chan time.Time, func())
	now             func() time.Time
	poll            func(*controlFiles, *http.Client, uint16) error
	removeDirectory func(string) error
	installDNS      func(*managedDNSPlan) (managedDNSLease, error)
	checkDNS        func(uint16) error
}

// These cores own a dual-stack internal TUN even on IPv4-only physical networks.
// Mihomo otherwise discards inet6-address during its host capability check.
// Scope the documented override to this child; never change the host environment.
func managedTunEnvironment() []string {
	return []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C", "SKIP_SYSTEM_IPV6_CHECK=1"}
}

func defaultTransparentRuntime() transparentRuntime {
	return transparentRuntime{
		command: systemCommand, network: originalPhysicalNetwork, readiness: coreReadiness, stopCore: stopChild, now: time.Now,
		removeDirectory: os.RemoveAll,
		installDNS:      installManagedDNS,
		checkDNS:        managedDNSReady,
		ticks:           func() (<-chan time.Time, func()) { t := time.NewTicker(time.Second); return t.C, t.Stop },
		poll: func(c *controlFiles, client *http.Client, port uint16) error {
			return c.refreshConnections(client, port)
		},
		startCore: func(runtime, dir, file string) (*exec.Cmd, error) {
			cmd := exec.Command(filepath.Join(runtime, "bin/mihomo"), "-d", dir, "-f", file)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Env = managedTunEnvironment()
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			return cmd, cmd.Start()
		},
	}
}

var errSessionEnded = errors.New("session lease ended")

type transparentConnection struct {
	port                       uint16
	required                   []transparentRoute
	control                    *controlFiles
	original, outer            originalNetwork
	device, statePath, runtime string
	state                      *transparentState
	local                      []localNetworkRoute
	tails                      tailscaleBypass
	exitLog                    *sessionExitDiagnostics
	ops                        transparentRuntime
}

func (connection *transparentConnection) run(ctx context.Context) (result error) {
	c, original, outerOriginal := connection.control, connection.original, connection.outer
	device, statePath, runtime := connection.device, connection.statePath, connection.runtime
	state, local, ts, exitLog, ops := connection.state, connection.local, connection.tails, connection.exitLog, connection.ops
	r := c.request
	port := connection.port
	dir := filepath.Dir(statePath)
	var e error
	checkLease := func() error {
		if ctx.Err() != nil {
			exitLog.stage = "lease"
			exitLog.capture("context_canceled", ctx.Err(), diagnosticDetails{})
			return errSessionEnded
		}
		if d, err := c.leaseDiagnostics(); d.Reason != "" {
			exitLog.stage = "lease"
			exitLog.capture(d.Reason, err, d)
			return errSessionEnded
		}
		return nil
	}
	routeCommand := func(name string, args ...string) ([]byte, error) {
		if name == "/sbin/route" && len(args) > 1 && args[1] == "add" {
			if err := checkLease(); err != nil {
				return nil, err
			}
		}
		return ops.command(name, args...)
	}
	var child *exec.Cmd
	var done chan error
	var dnsLease managedDNSLease
	defer exitLog.unwind(&result, func() {
		if errors.Is(result, errSessionEnded) {
			result = nil
		}
		if result != nil {
			trace("transparent.session_failed", 1)
		}
		// Remove the temporary resolver while its listener still exists. Closing
		// the dynamic-store session also restores DNS after abnormal worker exit.
		var dnsRestoreErr error
		if dnsLease != nil {
			dnsRestoreErr = dnsLease.close()
		}
		// Restore the network before waiting for a potentially blocked core. Retain
		// a quarantine record if either cleanup or child termination fails.
		cleanupErr := cleanupTransparentRoutesWithDiagnostics(statePath, state, ops.command, exitLog.cleanupEvent)
		exitLog.cleanupFailures = len(state.Routes)
		if cleanupErr != nil && exitLog.cleanupFailures == 0 {
			exitLog.cleanupFailures = 1
		}
		if dnsRestoreErr != nil {
			exitLog.cleanupFailures++
			exitLog.cleanupEvent("transparent.dns_restore_failed", 1, diagnosticDetails{Stage: "cleanup", Reason: "dns_restore_failed"})
			if cleanupErr == nil {
				cleanupErr = dnsRestoreErr
			}
		}
		var stopErr error
		if child != nil {
			stopErr = ops.stopCore(child, done)
		}
		if stopErr != nil {
			exitLog.stopFailed = true
			d := diagnosticError(stopErr)
			d.Stage = "core_stop"
			d.Reason = "core_stop_failed"
			exitLog.cleanupEvent("transparent.core_stop_failed", 1, d)
		}
		if cleanupErr != nil {
			result = cleanupErr
		} else if stopErr != nil {
			result = stopErr
		} else {
			if removeErr := ops.removeDirectory(dir); removeErr != nil {
				result = removeErr
				d := diagnosticError(removeErr)
				d.Stage = "remove"
				d.Reason = "directory_remove_failed"
				exitLog.cleanupFailures++
				exitLog.cleanupEvent("transparent.cleanup_directory_failed", 1, d)
			}
		}
	})
	exitLog.stage = "config"
	config, e := json.Marshal(makeTransparentConfig(r, device, port, original, local, ts))
	if e != nil {
		return e
	}
	file := filepath.Join(dir, "config.json")
	if e = os.WriteFile(file, config, 0600); e != nil {
		return e
	}
	exitLog.stage = "core_start"
	if e = checkLease(); e != nil {
		return e
	}
	cmd, e := ops.startCore(runtime, dir, file)
	if e != nil {
		return e
	}
	child = cmd
	done = make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	state.CorePID = cmd.Process.Pid
	exitLog.stage = "journal"
	if e = saveTransparentState(statePath, *state); e != nil {
		return e
	}
	client := &http.Client{Timeout: time.Second}
	ticks, stopTick := ops.ticks()
	defer stopTick()
	start := ops.now()
	active := false
	lastHealth := time.Time{}
	failures := 0
	var uncertain healthTimeoutWindow
	for {
		select {
		case <-ctx.Done():
			exitLog.stage = "lease"
			exitLog.capture("context_canceled", ctx.Err(), diagnosticDetails{})
			return nil
		case e := <-done:
			done <- e
			exitLog.stage = "health_core"
			code := cmd.ProcessState.ExitCode()
			exitLog.capture("core_exited", e, diagnosticDetails{ExitCode: &code})
			trace("transparent.core_exited", int64(cmd.ProcessState.ExitCode()))
			return errors.New("透明分流核心已退出")
		case <-ticks:
			if checkLease() != nil {
				return nil
			}
			if !active {
				exitLog.stage = "core_ready"
				healthy, health := ops.readiness(client, port, r.Token, device)
				if !healthy {
					if ops.now().Sub(start) > 15*time.Second {
						exitLog.capture("core_start_timeout", nil, health)
						return errors.New("透明分流入口启动超时")
					}
					continue
				}
				if !r.ProbeOnly {
					exitLog.stage = "network"
					current, e := ops.network(false)
					if e != nil {
						return exitLog.fail("network_query_failed", fmt.Errorf("原网络检查失败: %w", e))
					}
					if current != original {
						exitLog.capture("network_changed", nil, diagnosticDetails{ExpectedInterface: original.Interface, ActualInterface: current.Interface, GatewayChanged: current.Gateway != original.Gateway})
						return errors.New("原网络已改变，请重新连接")
					}
					// Keep the tunnel's outer connection on its original physical path.
					exitLog.stage = "health_gateway"
					actual, e := gatewayRouteInterfaceWith(r.Gateway, routeCommand)
					if e != nil {
						return exitLog.fail("gateway_query_failed", e)
					}
					if actual != original.Interface {
						exitLog.capture("gateway_changed", nil, diagnosticDetails{ExpectedInterface: original.Interface, ActualInterface: actual})
						return errors.New("VPN 服务器未使用原网络，请先结束冲突的 VPN")
					}
					if e = pinTransparentGatewayWith(statePath, state, r.Gateway, outerOriginal, routeCommand); e != nil {
						return e
					}
				} else {
					// Documentation-only address: verifies the outer gateway
					// remains pinned even when its enclosing prefix enters TUN.
					if e = pinTransparentGatewayWith(statePath, state, "203.0.113.5", original, routeCommand); e != nil {
						return e
					}
				}
				exitLog.stage = "routing"
				if e = ensureTransparentDirectRoutesWith(statePath, state, original, routeCommand); e != nil {
					return e
				}
				for _, prefix := range transparentCapturePrefixes(r.ProbeOnly) {
					if e = addTransparentRouteWith(statePath, state, transparentRoute{Prefix: prefix, Interface: device}, routeCommand); e != nil {
						return fmt.Errorf("无法添加透明路由 %s: %w", prefix, e)
					}
				}
				outer := r.Gateway
				if r.ProbeOnly {
					outer = "203.0.113.5"
				}
				exitLog.stage = "health_gateway"
				if ok, e := transparentGatewayReadyWith(outer, outerOriginal, routeCommand); !ok {
					if e != nil {
						return exitLog.fail("gateway_query_failed", e)
					}
					exitLog.capture("gateway_changed", nil, diagnosticDetails{ExpectedInterface: original.Interface})
					return errors.New("接管路由后 VPN 网关绕行失效，已停止连接")
				}
				scopes, e := transparentDirectRoutes(original, routeCommand)
				if e != nil {
					return exitLog.fail("network_query_failed", e)
				}
				connection.required = append(append([]transparentRoute{}, state.Routes...), scopes...)
				exitLog.stage = "routing"
				if details, err := verifyTransparentRoutePlan(connection.required, routeCommand); err != nil {
					exitLog.capture("route_missing", err, details)
					return err
				}
				if e = checkLease(); e != nil {
					return e
				}
				if r.managedDNS != nil {
					exitLog.stage = "dns_install"
					dnsLease, e = ops.installDNS(r.managedDNS)
					if e != nil {
						return exitLog.fail("dns_install_failed", e)
					}
				}
				active = true
				state.State = "ready"
				exitLog.stage = "journal"
				if e = saveTransparentState(statePath, *state); e != nil {
					return e
				}
				c.snapshot.SystemDevice = device
				c.snapshot.RemoteNetworks = r.RemoteNetworks
				c.snapshot.TailscaleActive = len(ts.Interfaces) > 0
				c.snapshot.TailscaleRoutes = len(ts.Prefixes)
				c.snapshot.Message = "透明 App 分流已启用，无需应用代理设置。"
				if dnsLease != nil {
					c.snapshot.Message = "透明分流及系统 DNS 保护已启用；直连域名与内网保留原解析。"
				}
				c.write("ready")
			} else if ops.now().Sub(lastHealth) > 5*time.Second {
				lastHealth = ops.now()
				if dnsLease != nil && (!dnsLease.intact() || ops.checkDNS(r.managedDNS.port) != nil) {
					exitLog.stage = "health_dns"
					return exitLog.fail("dns_unavailable", errors.New("系统 DNS 保护失效，已停止本次连接并恢复原解析"))
				}
				deferTimeout := func(err error, details diagnosticDetails) bool {
					if !readOnlyProbeTimeout(err) || !uncertain.retry(lastHealth, ops.now()) {
						return false
					}
					if details.Reason == "" {
						details = diagnosticError(err)
					}
					if details.Reason == "" {
						details.Reason = "system_timeout"
					}
					details.Stage = exitLog.stage
					traceDetails("transparent.health_deferred", 0, details)
					return true
				}
				if !r.ProbeOnly {
					exitLog.stage = "health_network"
					now, e := ops.network(true)
					if e != nil {
						if deferTimeout(e, diagnosticDetails{}) {
							continue
						}
						return exitLog.fail("network_query_failed", fmt.Errorf("原网络检查失败: %w", e))
					}
					if now != original {
						exitLog.capture("network_changed", nil, diagnosticDetails{ExpectedInterface: original.Interface, ActualInterface: now.Interface, GatewayChanged: now.Gateway != original.Gateway})
						return errors.New("原网络已改变，已停止本次连接；请重新连接")
					}
					exitLog.stage = "health_gateway"
					ok, e := transparentGatewayReadyWith(r.Gateway, outerOriginal, routeCommand)
					if e != nil {
						if deferTimeout(e, diagnosticDetails{}) {
							continue
						}
						return exitLog.fail("gateway_query_failed", fmt.Errorf("VPN 外层检查失败: %w", e))
					}
					if !ok {
						exitLog.capture("gateway_changed", nil, diagnosticDetails{ExpectedInterface: outerOriginal.Interface})
						return errors.New("VPN 外层路径改变，已停止连接以避免回流")
					}
				}
				exitLog.stage = "routing"
				if details, err := verifyTransparentRoutePlan(connection.required, routeCommand); err != nil {
					if deferTimeout(err, details) {
						continue
					}
					exitLog.capture("route_missing", err, details)
					return err
				}
				exitLog.stage = "health_core"
				healthy, health := ops.readiness(client, port, r.Token, device)
				if healthy {
					failures = 0
					if !uncertain.since.IsZero() {
						traceDetails("transparent.health_recovered", 0, diagnosticDetails{Stage: "health_core", Reason: "success", DurationMS: ops.now().Sub(uncertain.since).Milliseconds(), TimeoutMS: healthTimeoutGrace.Milliseconds()})
					}
					uncertain.reset()
				} else {
					if health.Reason == "controller_timeout" {
						if deferTimeout(context.DeadlineExceeded, health) {
							continue
						}
						exitLog.capture("core_unresponsive", nil, health)
						return errors.New("分流核心持续超时，已停止本次连接")
					}
					failures++
					traceDetails("transparent.health_failed", int64(failures), health)
				}
				if failures >= 3 {
					exitLog.capture("core_unresponsive", nil, health)
					trace("transparent.core_unresponsive", int64(failures))
					return errors.New("分流核心连续未响应，已恢复原网络并停止本次连接")
				}
			}
			if active {
				// Poll only metadata and counts; leave forwarding and route ownership unchanged.
				_ = ops.poll(c, client, port)
			}
		}
	}

}
