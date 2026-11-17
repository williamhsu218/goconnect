package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
			if result := checkProbeEndpoint(context.Background(), client, server.URL, false); result != "ok" {
				t.Fatalf("HTTPS returned %s", result)
			}
			want := "dns_response"
			if mode == "valid" {
				want = "ok"
			} else if mode == "redirect" {
				want = "http_status"
			}
			if result := checkProbeEndpoint(context.Background(), client, server.URL, true); result != want {
				t.Fatalf("DoH returned %s, want %s", result, want)
			}
		})
	}
}
