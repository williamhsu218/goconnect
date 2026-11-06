package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestNodeTotalsAuthenticateAndRejectMissingOrInvalidSamples(t *testing.T) {
	for _, sample := range []struct {
		body   string
		status int
		valid  bool
	}{
		{`{"downloadTotal":0,"uploadTotal":0}`, 200, true},
		{`{"downloadTotal":8192,"uploadTotal":123}`, 200, true},
		{`{"connections":[]}`, 200, false},
		{`{"downloadTotal":-1,"uploadTotal":0}`, 200, false},
		{`not json`, 200, false},
		{`{"downloadTotal":9,"uploadTotal":8}`, 401, false},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/connections" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
				t.Error("incorrect statistics request")
			}
			w.WriteHeader(sample.status)
			fmt.Fprint(w, sample.body)
		}))
		rx, tx, err := readNodeTotals(context.Background(), s.Client(), s.URL, "fixture-secret")
		s.Close()
		if (err == nil) != sample.valid {
			t.Fatalf("sample %q: %v", sample.body, err)
		}
		if sample.valid && sample.body[17] == '8' && (rx != 8192 || tx != 123) {
			t.Fatal("directions reversed", rx, tx)
		}
	}
}

func TestNodeTotalsMonitorCancellationJoinsWorker(t *testing.T) {
	entered := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	defer s.Close()
	_, p, _ := net.SplitHostPort(s.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	var published atomic.Int32
	stop := monitorNodeTotals(context.Background(), uint16(port), "fixture-secret", func(any) { published.Add(1) })
	<-entered
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("statistics worker did not cancel")
	}
	if published.Load() != 0 {
		t.Fatal("late statistics emitted after cancellation")
	}
}

func TestPublicIPValidationAndFallback(t *testing.T) {
	for _, raw := range []string{"", "<html>oops</html>", "127.0.0.1", "10.0.0.1", "100.64.1.1", "192.0.2.1", "2001:db8::1", "fe80::1%en0", "::ffff:192.168.1.1"} {
		if _, err := publicIPValue(raw); err == nil {
			t.Fatalf("accepted non-public response %q", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "2606:4700:4700::1111", " 8.8.8.8\n"} {
		if _, err := publicIPValue(raw); err != nil {
			t.Fatalf("rejected public response %q", raw)
		}
	}
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			fmt.Fprint(w, "10.0.0.1")
		} else {
			fmt.Fprint(w, "1.1.1.1\n")
		}
	}))
	defer s.Close()
	ip, err := queryPublicIP(context.Background(), s.Client(), []string{s.URL + "/bad", s.URL + "/ok"})
	if err != nil || ip != "1.1.1.1" {
		t.Fatal(ip, err)
	}
}
