package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestPathHealthDegradesOnlyAfterConsecutiveProxyFailure(t *testing.T) {
	var tracker pathHealthTracker
	healthy := pathHealth{ProxyHTTPS: pathCheck{Result: "ok"}, ProxyDoH: pathCheck{Result: "ok"}, DirectHTTPS: pathCheck{Result: "connect"}}
	unhealthy := healthy
	unhealthy.ProxyDoH.Result = "dns_response"
	for _, check := range []struct {
		sample pathHealth
		state  string
	}{{healthy, "reachable"}, {unhealthy, "checking"}, {healthy, "reachable"}, {unhealthy, "checking"}, {unhealthy, "degraded"}, {healthy, "reachable"}} {
		if got := tracker.observe(check.sample).State; got != check.state {
			t.Fatalf("state %q, want %q", got, check.state)
		}
	}
}

func TestPathHealthActualHTTPSAndValidatedDoH(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-question", "wrong-id", "invalid-message", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead && r.URL.Path == "/" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/dns-query" || r.Header.Get("Content-Type") != "application/dns-message" {
					t.Error("unexpected probe request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://localhost:1/dns-query", http.StatusTemporaryRedirect)
					return
				}
				var query dnsmessage.Message
				body, err := io.ReadAll(r.Body)
				if err != nil || query.Unpack(body) != nil || len(query.Questions) != 1 || query.Questions[0] != pathProbeQuestion {
					t.Error("invalid DoH query")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				query.Response = true
				query.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: pathProbeQuestion.Name, Class: dnsmessage.ClassINET, TTL: 20},
					Body:   &dnsmessage.AResource{A: [4]byte{192, 0, 2, 42}},
				}}
				switch mode {
				case "wrong-question":
					query.Questions[0].Name = dnsmessage.MustNewName("other.example.")
				case "wrong-id":
					query.ID++
				}
				wire, err := query.Pack()
				if err != nil {
					t.Error(err)
				}
				if mode == "invalid-message" {
					wire = []byte("not a DNS reply")
				}
				w.Header().Set("Content-Type", "application/dns-message")
				_, _ = w.Write(wire)
			}))
			defer server.Close()
			client := server.Client()
			client.CheckRedirect = newPathProbeClient(nil).CheckRedirect
			client.Transport.(*http.Transport).DisableKeepAlives = true
			endpoint := pathProbeEndpoint{url: server.URL, provider: "cloudflare", family: 4}
			ok := checkProbeEndpoint(context.Background(), client, endpoint, false)
			if ok.Reason != "ok" || ok.ProbeTarget != "cloudflare" || ok.Family != 4 || ok.HTTPStatus != 204 ||
				ok.ConnectMS == nil || ok.TLSMS == nil || ok.ResponseMS == nil {
				t.Fatalf("HTTPS result or staged timing missing: %+v", ok)
			}
			want := "dns_response"
			if mode == "valid" {
				want = "ok"
			} else if mode == "redirect" {
				want = "http_status"
			}
			attempt := checkProbeEndpoint(context.Background(), client, endpoint, true)
			if attempt.Reason != want || attempt.ConnectMS == nil || attempt.TLSMS == nil || attempt.ResponseMS == nil {
				t.Fatalf("DoH got %+v, want %s with staged timings", attempt, want)
			}
		})
	}
}

func TestPathHealthExpectedEmptyHTTPSResponse(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		status, additionalCode int
		dns, untrusted         bool
		want                   string
	}{
		{"original-https", http.StatusBadRequest, http.StatusBadRequest, false, false, "ok"},
		{"original-server-error", http.StatusServiceUnavailable, http.StatusBadRequest, false, false, "http_status"},
		{"ordinary-https", http.StatusBadRequest, 0, false, false, "http_status"},
		{"doh", http.StatusBadRequest, http.StatusBadRequest, true, false, "http_status"},
		{"untrusted-certificate", http.StatusBadRequest, http.StatusBadRequest, false, true, "tls"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tc.dns && (r.Method != http.MethodHead || r.ContentLength != 0 || r.URL.RawQuery != "") {
					t.Error("HTTPS reachability probe must send an empty HEAD request")
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client := server.Client()
			if tc.untrusted {
				var dialer net.Dialer
				client = newPathProbeClient(dialer.DialContext)
			}
			defer client.CloseIdleConnections()
			endpoint := pathProbeEndpoint{url: server.URL, provider: "alidns", family: 4, additionalHTTPSStatus: tc.additionalCode}
			if attempt := checkProbeEndpoint(context.Background(), client, endpoint, tc.dns); attempt.Reason != tc.want {
				t.Fatalf("probe got %+v, want %s", attempt, tc.want)
			}
		})
	}
}

func TestPathHealthRecordsEachAttemptAndOnlyReachedStages(t *testing.T) {
	peer := make(chan net.Conn, 1)
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		if address == "1.1.1.1:443" {
			return nil, errors.New("fixture dial failure")
		}
		front, back := net.Pipe()
		peer <- back
		return front, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	check := checkPath(ctx, dial, probeIPv4, false)
	select {
	case back := <-peer:
		_ = back.Close()
	default:
	}
	if len(check.attempts) != 2 || check.Result != "tls" {
		t.Fatalf("missing ordered probe attempts: %+v", check)
	}
	first, second := check.attempts[0], check.attempts[1]
	if first.ProbeTarget != "cloudflare" || first.Reason != "connect" ||
		first.ConnectMS == nil || first.TLSMS != nil || first.ResponseMS != nil || first.TimedOut {
		t.Fatalf("unexpected connect timing: %+v", first)
	}
	if second.ProbeTarget != "google" || second.Reason != "tls" ||
		second.ConnectMS == nil || second.TLSMS == nil || second.ResponseMS != nil || !second.TimedOut {
		t.Fatalf("unexpected TLS timeout timing: %+v", second)
	}
}
