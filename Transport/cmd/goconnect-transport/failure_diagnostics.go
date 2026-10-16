package main

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Only fixed labels and numeric metadata, never errors, argv or core output.
type diagnosticDetails struct {
	Stage             string              `json:"stage,omitempty"`
	Reason            string              `json:"reason,omitempty"`
	Check             string              `json:"check,omitempty"`
	Command           string              `json:"command,omitempty"`
	OperationID       uint64              `json:"operationID,omitempty"`
	DurationMS        int64               `json:"durationMs,omitempty"`
	TimeoutMS         int64               `json:"timeoutMs,omitempty"`
	ExitCode          *int                `json:"exitCode,omitempty"`
	Signal            int                 `json:"signal,omitempty"`
	Errno             int                 `json:"errno,omitempty"`
	RouteIndex        *int                `json:"routeIndex,omitempty"`
	Family            int                 `json:"family,omitempty"`
	PrefixBits        *int                `json:"prefixBits,omitempty"`
	Scoped            *bool               `json:"scoped,omitempty"`
	Interface         string              `json:"interface,omitempty"`
	ExpectedInterface string              `json:"expectedInterface,omitempty"`
	ActualInterface   string              `json:"actualInterface,omitempty"`
	GatewayChanged    bool                `json:"gatewayChanged,omitempty"`
	HTTPStatus        int                 `json:"httpStatus,omitempty"`
	CleanupFailures   int                 `json:"cleanupFailures,omitempty"`
	StopFailed        bool                `json:"stopFailed,omitempty"`
	Cleanup           []diagnosticDetails `json:"cleanup,omitempty"`
}

var diagnosticInterface = regexp.MustCompile(`^(en|bridge|bond|vnic|vmnet|utun|tun|lo)[0-9]{1,5}$`)

func allowedDiagnosticLabel(value, allowed string) string {
	if value == "" {
		return ""
	}
	for _, label := range strings.Fields(allowed) {
		if value == label {
			return value
		}
	}
	return "unknown"
}

func sanitizeDiagnosticDetails(d diagnosticDetails) diagnosticDetails {
	d.Stage = allowedDiagnosticLabel(d.Stage, "unknown control lock recovery apps network local tailscale company address device port runtime journal config core_start core_ready routing health_network health_gateway health_core lease cleanup core_stop remove worker service command")
	reasons := "unknown operation_failed panic context_canceled lease_missing lease_unreadable lease_token_mismatch lease_expired lease_future owner_missing owner_changed core_exited core_start_timeout network_changed network_query_failed gateway_changed gateway_query_failed core_unresponsive read_failed delete_failed recheck_failed delete_still_present invalid_route journal_write_failed directory_remove_failed core_stop_failed system_timeout command_failed command_slow command_late success tun_missing tun_down controller_timeout controller_connect_failed controller_http_failed controller_decode_failed tun_disabled"
	d.Reason = allowedDiagnosticLabel(d.Reason, reasons)
	d.Check = allowedDiagnosticLabel(d.Check, reasons)
	d.Command = allowedDiagnosticLabel(d.Command, "unknown route.get4 route.get6 route.add4 route.add6 route.delete4 route.delete6 route.other netstat.inet netstat.inet6 netstat.other ps ifconfig dscl dseditgroup codesign launchctl otool")
	for _, field := range []*string{&d.Interface, &d.ExpectedInterface, &d.ActualInterface} {
		if !diagnosticInterface.MatchString(*field) {
			*field = ""
		}
	}
	if d.ExitCode != nil {
		n := *d.ExitCode
		d.ExitCode = &n
	}
	if d.RouteIndex != nil {
		n := *d.RouteIndex
		d.RouteIndex = &n
	}
	if d.PrefixBits != nil {
		n := *d.PrefixBits
		d.PrefixBits = &n
	}
	if d.Scoped != nil {
		n := *d.Scoped
		d.Scoped = &n
	}
	cleanup := d.Cleanup
	d.Cleanup = nil
	for i, entry := range cleanup {
		if i >= 33 {
			break
		}
		entry.Cleanup = nil
		d.Cleanup = append(d.Cleanup, sanitizeDiagnosticDetails(entry))
	}
	return d
}

func traceDetails(code string, value int64, d diagnosticDetails) {
	if diagnostics != nil {
		diagnostics.recordDetails(code, value, d)
	}
}

func diagnosticError(err error) diagnosticDetails {
	var failure *systemCommandFailure
	if errors.As(err, &failure) {
		return sanitizeDiagnosticDetails(failure.details)
	}
	d := diagnosticDetails{}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		d.Errno = int(errno)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		process := diagnosticProcessExit(exit.ProcessState)
		d.ExitCode, d.Signal = process.ExitCode, process.Signal
	}
	return d
}

func diagnosticProcessExit(state *os.ProcessState) diagnosticDetails {
	d := diagnosticDetails{}
	if state != nil {
		n := state.ExitCode()
		d.ExitCode = &n
		if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			d.Signal = int(status.Signal())
		}
	}
	return d
}

type sessionExitDiagnostics struct {
	stage           string
	first           *diagnosticDetails
	cleanupFailures int
	stopFailed      bool
	cleanup         []diagnosticDetails
	started         time.Time
	emit            func(string, int64, diagnosticDetails)
}

func newSessionExitDiagnostics() *sessionExitDiagnostics {
	return &sessionExitDiagnostics{stage: "control", started: time.Now(), emit: traceDetails}
}

func (d *sessionExitDiagnostics) capture(reason string, err error, extra diagnosticDetails) {
	if d.first != nil {
		return
	}
	fields := diagnosticError(err)
	fields.Stage = d.stage
	fields.Check = fields.Reason
	fields.Reason = reason
	fields.Interface = extra.Interface
	fields.ExpectedInterface = extra.ExpectedInterface
	fields.ActualInterface = extra.ActualInterface
	fields.GatewayChanged = extra.GatewayChanged
	if extra.Reason != "" {
		fields.Check = extra.Reason
	}
	fields.HTTPStatus = extra.HTTPStatus
	if extra.ExitCode != nil {
		fields.ExitCode = extra.ExitCode
	}
	if extra.Signal != 0 {
		fields.Signal = extra.Signal
	}
	if extra.DurationMS != 0 {
		fields.DurationMS = extra.DurationMS
	}
	if extra.TimeoutMS != 0 {
		fields.TimeoutMS = extra.TimeoutMS
	}
	if extra.Errno != 0 {
		fields.Errno = extra.Errno
	}
	fields = sanitizeDiagnosticDetails(fields)
	d.first = &fields
	d.emit("session.exit_first", 0, fields)
}

func (d *sessionExitDiagnostics) fail(reason string, err error) error {
	d.capture(reason, err, diagnosticDetails{})
	return err
}

func (d *sessionExitDiagnostics) beforeCleanup(primary error) {
	if d.first == nil {
		reason := "success"
		if primary != nil {
			reason = "operation_failed"
		}
		d.capture(reason, primary, diagnosticDetails{})
	}
}

// Called directly by defer: preserve a panic as the first cause without ever
// persisting its arbitrary payload. Cleanup and the original panic still run.
func (d *sessionExitDiagnostics) unwind(primary *error, cleanup func()) {
	if value := recover(); value != nil {
		d.capture("panic", nil, diagnosticDetails{})
		defer func() { panic(value) }()
	} else {
		d.beforeCleanup(*primary)
	}
	cleanup()
}

func (d *sessionExitDiagnostics) complete(result *error) {
	if value := recover(); value != nil {
		d.capture("panic", nil, diagnosticDetails{})
		d.finish(*result)
		panic(value)
	}
	d.finish(*result)
}

func (d *sessionExitDiagnostics) finish(result error) {
	d.beforeCleanup(result)
	fields := *d.first
	fields.CleanupFailures = d.cleanupFailures
	fields.StopFailed = d.stopFailed
	fields.Cleanup = d.cleanup
	d.emit("session.exit_final", time.Since(d.started).Milliseconds(), fields)
}

func (d *sessionExitDiagnostics) cleanupEvent(code string, value int64, fields diagnosticDetails) {
	d.emit(code, value, fields)
	if len(d.cleanup) < 33 {
		d.cleanup = append(d.cleanup, sanitizeDiagnosticDetails(fields))
	}
}
