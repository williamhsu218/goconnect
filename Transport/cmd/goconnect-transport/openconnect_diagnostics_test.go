package main

import (
	"strings"
	"testing"
)

func TestOpenConnectDiagnosticClassificationAndPrivacy(t *testing.T) {
	cases := map[string]string{
		"Received server disconnect: b0 'Idle Timeout'":          "openconnect.idle_timeout",
		"Received server disconnect: b0 'Session Timeout'":       "openconnect.session_expired",
		"Received server disconnect: b0 'PRIVATE_SERVER_TEXT'":   "openconnect.server_disconnect",
		"Session terminated by server; exiting.":                 "openconnect.server_disconnect",
		"Cookie was rejected by server; exiting.":                "openconnect.cookie_rejected",
		"CSTP Dead Peer Detection detected dead peer!":           "openconnect.dpd_timeout",
		"SSL_read failed: PRIVATE_DESTINATION":                   "openconnect.tls_read_failed",
		"SSL_write failed: PRIVATE_DESTINATION":                  "openconnect.tls_write_failed",
		"SSL connection failure: PRIVATE_DESTINATION":            "openconnect.tls_failed",
		"CSTP reconnect failed":                                  "openconnect.reconnect_failed",
		"Failed to reconnect to host PRIVATE_DESTINATION: error": "openconnect.reconnect_failed",
		"Reconnecting to host PRIVATE_DESTINATION":               "openconnect.reconnect_started",
	}
	for line, code := range cases {
		d := classifyOpenConnect(line)
		if len(d) != 1 || d[0].Code != code || d[0].Value != 0 {
			t.Fatalf("classification failed: %q", code)
		}
		if strings.Contains(d[0].Code, "PRIVATE") {
			t.Fatal("private text escaped")
		}
	}
	for _, line := range []string{"Set-Cookie: SECRET", "COOKIE='SECRET'", "https://PRIVATE/path", "Authorization: SECRET", "X-CSTP-Session-Timeout: SECRET", "<password>SECRET</password>"} {
		if len(classifyOpenConnect(line)) != 0 {
			t.Fatal("unrecognized output retained")
		}
	}
}

func TestOpenConnectTimingIsNumericOnly(t *testing.T) {
	d := classifyOpenConnect("CSTP connected. DPD 30, Keepalive 20")
	if len(d) != 2 || d[0].Value != 30 || d[1].Value != 20 {
		t.Fatal(d)
	}
	d = classifyOpenConnect("X-CSTP-Session-Timeout: 3600")
	if len(d) != 1 || d[0].Code != "openconnect.session_timeout_seconds" || d[0].Value != 3600 {
		t.Fatal(d)
	}
}
