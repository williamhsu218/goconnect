package main

import (
	"context"
	"errors"
	"time"
)

const healthTimeoutGrace = 30 * time.Second

// Only read-only health probes may defer a timeout. Never retry a mutation or
// accept an unconfirmed route change. One window spans all probe stages; only
// a fully successful health pass resets it, preventing alternating timeouts
// from keeping an unverified session alive indefinitely.
type healthTimeoutWindow struct{ since time.Time }

func (w *healthTimeoutWindow) retry(started, now time.Time) bool {
	if w.since.IsZero() {
		w.since = started
	}
	return now.Sub(w.since) < healthTimeoutGrace
}

func (w *healthTimeoutWindow) reset() { w.since = time.Time{} }

func readOnlyProbeTimeout(err error) bool {
	var failure *systemCommandFailure
	if errors.As(err, &failure) {
		switch failure.details.Command {
		case "route.get4", "route.get6", "netstat.inet", "netstat.inet6":
			return failure.details.Reason == "system_timeout"
		default:
			return false
		}
	}
	return errors.Is(err, context.DeadlineExceeded)
}
