package main

import "time"

const reconnectTimeoutSeconds = 60
const transportRecoveryLimit = 75 * time.Second

// OpenConnect owns the cookie, backoff, TLS/DTLS and packet socket. Recover
// within that process; never replay a password or start a second session.
// Callers serialize this state with the diagnostic reader lock. The additional
// monotonic wall-clock limit includes time spent in a stalled TLS handshake,
// which OpenConnect's retry-sleep budget does not include.
type transportRecovery struct {
	connected bool
	since     time.Time
	terminal  bool
}

func (r *transportRecovery) observe(code string, now time.Time) string {
	if r.terminal {
		return ""
	}
	switch code {
	case "openconnect.server_disconnect", "openconnect.idle_timeout", "openconnect.session_expired",
		"openconnect.cookie_rejected", "openconnect.certificate_failed", "openconnect.authentication_failed":
		r.terminal = true
		return ""
	}
	if r.expire(now) {
		return "recovery_expired"
	}
	switch code {
	case "openconnect.cstp_connected":
		r.connected = true
		if !r.since.IsZero() {
			r.since = time.Time{}
			return "reconnected"
		}
	case "openconnect.tls_read_failed", "openconnect.tls_write_failed", "openconnect.tls_failed",
		"openconnect.dpd_timeout", "openconnect.reconnect_started", "openconnect.reconnect_failed", "openconnect.retry_wait_seconds":
		if r.connected && r.since.IsZero() {
			r.since = now
			return "reconnecting"
		}
	}
	return ""
}

func (r *transportRecovery) expire(now time.Time) bool {
	if !r.terminal && !r.since.IsZero() && now.Sub(r.since) >= transportRecoveryLimit {
		r.terminal = true
		return true
	}
	return false
}
