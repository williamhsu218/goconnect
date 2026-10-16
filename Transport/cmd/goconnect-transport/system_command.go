package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

type systemCommandFailure struct {
	details diagnosticDetails
	cause   error
}

func (e *systemCommandFailure) Error() string { return "system command failed: " + e.details.Command }
func (e *systemCommandFailure) Unwrap() error { return e.cause }

var commandOperation atomic.Uint64

func systemCommandLabel(name string, args []string) string {
	switch name {
	case "/sbin/route":
		action, family := "", "4"
		for _, a := range args {
			if a == "-inet6" {
				family = "6"
			}
			if a == "get" || a == "add" || a == "delete" {
				action = a
			}
		}
		if action == "" {
			return "route.other"
		}
		return "route." + action + family
	case "/usr/sbin/netstat":
		for i, a := range args {
			if a == "-f" && i+1 < len(args) && (args[i+1] == "inet" || args[i+1] == "inet6") {
				return "netstat." + args[i+1]
			}
		}
		return "netstat.other"
	}
	for _, command := range []string{"ps", "ifconfig", "dscl", "dseditgroup", "codesign", "launchctl", "otool"} {
		if name == "/bin/"+command || name == "/sbin/"+command || name == "/usr/bin/"+command || name == "/usr/sbin/"+command {
			return command
		}
	}
	if strings.HasSuffix(name, "/usr/bin/otool") {
		return "otool"
	}
	return "unknown"
}

func systemCommand(name string, args ...string) ([]byte, error) {
	return runSystemCommand(name, args, 6*time.Second, func(ctx context.Context) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
		return cmd.Output()
	}, traceDetails)
}

// The outer deadline stays bounded even for children blocked in the kernel.
// Late results share an operation ID and never include command output.
func runSystemCommand(name string, args []string, limit time.Duration, run func(context.Context) ([]byte, error), emit func(string, int64, diagnosticDetails)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	started := time.Now()
	base := diagnosticDetails{Stage: "command", Command: systemCommandLabel(name, args), OperationID: commandOperation.Add(1), TimeoutMS: limit.Milliseconds()}
	type output struct {
		data []byte
		err  error
	}
	done := make(chan output, 1)
	// Publish the timeout before a late result, including the race where both
	// completion and the deadline become ready together.
	decision := make(chan bool, 1)
	go func() {
		data, err := run(ctx)
		done <- output{data, err}
		if <-decision {
			d := diagnosticError(err)
			d.Check = "command_failed"
			if err == nil {
				d.Check = "success"
				code := 0
				d.ExitCode = &code
			}
			d.Stage = "command"
			d.Reason = "command_late"
			d.Command = base.Command
			d.OperationID = base.OperationID
			d.DurationMS = time.Since(started).Milliseconds()
			d.TimeoutMS = base.TimeoutMS
			emit("system.command_late_result", 0, d)
		}
	}()
	select {
	case result := <-done:
		decision <- false
		d := diagnosticError(result.err)
		d.Stage = base.Stage
		d.Command = base.Command
		d.OperationID = base.OperationID
		d.TimeoutMS = base.TimeoutMS
		d.DurationMS = time.Since(started).Milliseconds()
		if result.err != nil {
			d.Reason = "command_failed"
			event := "system.command_failed"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				d.Reason = "system_timeout"
				event = "system.command_timeout"
			}
			emit(event, 0, d)
			return result.data, &systemCommandFailure{details: d, cause: result.err}
		}
		if d.DurationMS >= 1000 {
			d.Reason = "command_slow"
			emit("system.command_slow", 0, d)
		}
		return result.data, nil
	case <-ctx.Done():
		base.DurationMS = time.Since(started).Milliseconds()
		base.Reason = "system_timeout"
		emit("system.command_timeout", 0, base)
		decision <- true
		return nil, &systemCommandFailure{details: base, cause: context.DeadlineExceeded}
	}
}
