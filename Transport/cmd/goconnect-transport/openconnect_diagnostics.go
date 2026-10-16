package main

// Reduce core output to an allowlist. No arbitrary server text, addresses,
// credentials, cookies, HTTP bodies or command lines cross this boundary.
import (
	"regexp"
	"strconv"
	"strings"
)

type coreDiagnostic struct {
	Code  string
	Value int
}

var cstpTiming = regexp.MustCompile(`^CSTP connected\. DPD ([0-9]{1,9}), Keepalive ([0-9]{1,9})$`)
var cstpTimeout = regexp.MustCompile(`^X-CSTP-(Session-Timeout|Session-Timeout-Remaining|Idle-Timeout): ([0-9]{1,9})$`)

func classifyOpenConnect(line string) []coreDiagnostic {
	line = strings.TrimSpace(line)
	if match := cstpTiming.FindStringSubmatch(line); match != nil {
		dpd, _ := strconv.Atoi(match[1])
		keepalive, _ := strconv.Atoi(match[2])
		return []coreDiagnostic{{"openconnect.dpd_seconds", dpd}, {"openconnect.keepalive_seconds", keepalive}}
	}
	if match := cstpTimeout.FindStringSubmatch(line); match != nil {
		codes := map[string]string{"Session-Timeout": "openconnect.session_timeout_seconds", "Session-Timeout-Remaining": "openconnect.session_remaining_seconds", "Idle-Timeout": "openconnect.idle_timeout_seconds"}
		value, _ := strconv.Atoi(match[2])
		return []coreDiagnostic{{codes[match[1]], value}}
	}
	lower := strings.ToLower(line)
	code := ""
	switch {
	case strings.HasPrefix(lower, "received server disconnect:"):
		code = "openconnect.server_disconnect"
		// Match only fixed protocol reasons, never retain the quoted server text.
		if strings.HasSuffix(lower, "'idle timeout'") {
			code = "openconnect.idle_timeout"
		}
		if strings.HasSuffix(lower, "'session timeout'") {
			code = "openconnect.session_expired"
		}
	case lower == "session terminated by server; exiting.", lower == "received server disconnect":
		code = "openconnect.server_disconnect"
	case lower == "cookie was rejected by server; exiting.":
		code = "openconnect.cookie_rejected"
	case lower == "server terminated connection (session expired)", lower == "vpn session expired":
		code = "openconnect.session_expired"
	case strings.HasPrefix(lower, "cstp dead peer detection detected dead peer"):
		code = "openconnect.dpd_timeout"
	case strings.HasPrefix(lower, "ssl read error"), strings.HasPrefix(lower, "ssl_read"):
		code = "openconnect.tls_read_failed"
	case strings.HasPrefix(lower, "ssl write error"), strings.HasPrefix(lower, "ssl_write"):
		code = "openconnect.tls_write_failed"
	case strings.HasPrefix(lower, "ssl connection failure"), lower == "creating ssl connection failed":
		code = "openconnect.tls_failed"
	case lower == "cstp reconnect failed", lower == "reconnect failed", strings.HasPrefix(lower, "failed to reconnect to host "):
		code = "openconnect.reconnect_failed"
	case strings.HasPrefix(lower, "reconnecting to host "), strings.HasPrefix(lower, "reconnecting to vpn "):
		code = "openconnect.reconnect_started"
	case strings.Contains(lower, "certificate") && (strings.Contains(lower, "fail") || strings.Contains(lower, "invalid")):
		code = "openconnect.certificate_failed"
	case strings.Contains(lower, "authentication failed"), strings.Contains(lower, "login failed"):
		code = "openconnect.authentication_failed"
	}
	if code == "" {
		return nil
	}
	return []coreDiagnostic{{code, 0}}
}

func emitCoreDiagnostic(code string, value int) {
	emit(map[string]any{"event": "diagnostic", "code": code, "value": value})
}
