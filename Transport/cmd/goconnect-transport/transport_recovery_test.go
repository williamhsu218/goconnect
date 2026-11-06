package main

import (
	"testing"
	"time"
)

func TestReconnectFailedStartsBoundedRecovery(t *testing.T) {
	now := time.Now()
	var r transportRecovery
	if r.observe("openconnect.reconnect_failed", now) != "" {
		t.Fatal("initial failure started recovery")
	}
	r.observe("openconnect.cstp_connected", now)
	if r.observe("openconnect.reconnect_failed", now) != "reconnecting" {
		t.Fatal("standalone reconnect failure did not start recovery")
	}
	if r.observe("openconnect.reconnect_failed", now.Add(70*time.Second)) != "" {
		t.Fatal("duplicate failure restarted recovery")
	}
	if r.observe("openconnect.cstp_connected", now.Add(transportRecoveryLimit)) != "recovery_expired" {
		t.Fatal("late success bypassed original deadline")
	}
	if r.observe("openconnect.reconnect_failed", now.Add(time.Hour)) != "" {
		t.Fatal("terminal state revived")
	}
}

func TestTransportRecoveryBoundaries(t *testing.T) {
	now := time.Now()
	var r transportRecovery
	if r.observe("openconnect.tls_failed", now) != "" {
		t.Fatal("initial authentication is not recovery")
	}
	r.observe("openconnect.cstp_connected", now)
	for i := 0; i < 2; i++ {
		if r.observe("openconnect.tls_read_failed", now) != "reconnecting" {
			t.Fatal("outage not reported")
		}
		if r.observe("openconnect.retry_wait_seconds", now.Add(70*time.Second)) != "" {
			t.Fatal("retry restarted budget")
		}
		if r.observe("openconnect.cstp_connected", now.Add(74*time.Second)) != "reconnected" {
			t.Fatal("recovery not reported")
		}
		now = now.Add(100 * time.Second)
	}
	r.observe("openconnect.dpd_timeout", now)
	if r.expire(now.Add(transportRecoveryLimit - time.Nanosecond)) {
		t.Fatal("early expiry")
	}
	if r.observe("openconnect.cstp_connected", now.Add(transportRecoveryLimit)) != "recovery_expired" {
		t.Fatal("late success extended deadline")
	}
	if r.observe("openconnect.cstp_connected", now.Add(time.Hour)) != "" || r.expire(now.Add(time.Hour)) {
		t.Fatal("terminal state revived")
	}
}

func TestTransportRecoveryTerminalReasons(t *testing.T) {
	for _, code := range []string{"server_disconnect", "idle_timeout", "session_expired", "cookie_rejected", "certificate_failed", "authentication_failed"} {
		t.Run(code, func(t *testing.T) {
			var r transportRecovery
			now := time.Now()
			r.observe("openconnect.cstp_connected", now)
			r.observe("openconnect.tls_failed", now)
			r.observe("openconnect."+code, now)
			if r.observe("openconnect.cstp_connected", now) != "" || r.expire(now.Add(time.Hour)) {
				t.Fatal("terminal reason was treated as recoverable")
			}
		})
	}
}
