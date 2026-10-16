package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestCoreReadinessDistinguishesFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		status             int
		delay              time.Duration
	}{
		{"ready", `{"tun":{"enable":true}}`, "success", 200, 0},
		{"disabled", `{"tun":{"enable":false}}`, "tun_disabled", 200, 0},
		{"decode", `SECRET_MALFORMED`, "controller_decode_failed", 200, 0},
		{"http", `SECRET_DENIED`, "controller_http_failed", 401, 0},
		{"timeout", `SECRET_LATE`, "controller_timeout", 200, 80 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer SECRET_TOKEN" {
					t.Error("fixture authentication missing")
				}
				time.Sleep(tc.delay)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, portString, _ := net.SplitHostPort(server.Listener.Addr().String())
			port, _ := strconv.Atoi(portString)
			ok, d := coreReadinessWith(&http.Client{Timeout: 30 * time.Millisecond}, uint16(port), "SECRET_TOKEN", "utun100", func(string) (*net.Interface, error) {
				return &net.Interface{Flags: net.FlagUp}, nil
			})
			if ok != (tc.reason == "success") || d.Reason != tc.reason || d.TimeoutMS != 30 || d.Interface != "utun100" {
				t.Fatalf("indistinguishable readiness failure: %+v", d)
			}
			if tc.reason != "controller_timeout" && d.HTTPStatus != tc.status {
				t.Fatal("HTTP status lost", d)
			}
			assertDiagnosticPrivate(t, d)
		})
	}
	for _, tc := range []struct {
		name, reason string
		iface        *net.Interface
		err          error
	}{
		{"missing", "tun_missing", nil, syscall.ENXIO},
		{"down", "tun_down", &net.Interface{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, d := coreReadinessWith(&http.Client{Timeout: time.Second}, 1, "SECRET", "utun100", func(string) (*net.Interface, error) { return tc.iface, tc.err })
			if ok || d.Reason != tc.reason || (tc.err != nil && d.Errno != int(syscall.ENXIO)) {
				t.Fatal(d)
			}
		})
	}
	server := httptest.NewServer(http.NotFoundHandler())
	_, portString, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	server.Close()
	ok, d := coreReadinessWith(&http.Client{Timeout: time.Second}, uint16(port), "SECRET", "utun100", func(string) (*net.Interface, error) { return &net.Interface{Flags: net.FlagUp}, nil })
	if ok || d.Reason != "controller_connect_failed" || d.Errno != int(syscall.ECONNREFUSED) {
		t.Fatal("controller connection failure lost", d)
	}
}

func TestLeaseDiagnosticReasons(t *testing.T) {
	for _, reason := range []string{"success", "lease_missing", "lease_unreadable", "lease_token_mismatch", "lease_expired", "lease_future", "owner_changed", "owner_missing"} {
		t.Run(reason, func(t *testing.T) {
			dir := controlFixture(t)
			c, err := openControl(filepath.Join(dir, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.close()
			lease := filepath.Join(dir, "lease")
			switch reason {
			case "lease_missing":
				err = os.Remove(lease)
			case "lease_unreadable":
				err = os.Chmod(lease, 0644)
			case "lease_token_mismatch":
				err = os.WriteFile(lease, []byte("SECRET_WRONG_TOKEN"), 0600)
			case "lease_expired", "lease_future":
				stamp := time.Now().Add(-10 * time.Second)
				if reason == "lease_future" {
					stamp = time.Now().Add(10 * time.Second)
				}
				err = os.Chtimes(lease, stamp, stamp)
			case "owner_changed":
				c.started.Sec--
			case "owner_missing":
				c.request.OwnerPID = 1 << 30
			}
			if err != nil {
				t.Fatal(err)
			}
			d, err := c.leaseDiagnostics()
			if reason == "success" {
				if d.Reason != "" || err != nil {
					t.Fatal(d, err)
				}
				return
			}
			if d.Reason != reason || d.TimeoutMS != 5000 || c.alive() {
				t.Fatalf("lease failure not distinguished: %+v %v", d, err)
			}
			if reason == "lease_missing" && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing lease errno lost", err)
			}
			assertDiagnosticPrivate(t, d)
		})
	}
}
