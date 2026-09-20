package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTransparentConnectionSnapshot(t *testing.T) {
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if unavailable.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"connections":[
		 {"metadata":{"processPath":"/Applications/Example.app/Contents/MacOS/Example","host":"private.example"},"chains":["GoConnect"]},
		 {"metadata":{"processPath":"/Applications/Example.app/Contents/MacOS/Example"},"chains":["OriginalNetwork"]},
		 {"metadata":{"processPath":""},"chains":["Local0"]},
		 {"metadata":{"processPath":""},"chains":["REJECT"]}]}`))
	}))
	defer server.Close()
	file, err := os.Create(filepath.Join(t.TempDir(), "status"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	control := controlFiles{status: file, request: routeRequest{Token: "fixture-token", AppPaths: []string{"/Applications/Example.app"}},
		snapshot: routeSnapshot{CaptureMode: "transparent-routes", RoutingMode: "whitelist"}}
	port := uint16(server.Listener.Addr().(*net.TCPAddr).Port)
	if err := control.refreshConnections(server.Client(), port); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var snapshot routeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "ready" || snapshot.UpdatedAt == 0 || snapshot.VPN != 1 || snapshot.Direct != 2 || snapshot.Blocked != 1 || snapshot.Unknown != 2 || !snapshot.VPNObserved || snapshot.Apps[0].VPN != 1 || snapshot.Apps[0].Direct != 1 {
		t.Fatalf("incorrect transparent statistics: %+v", snapshot)
	}
	if strings.Contains(string(data), "private.example") || strings.Contains(string(data), "fixture-token") {
		t.Fatal("private controller data was written to status")
	}
	unavailable.Store(true)
	if err := control.refreshConnections(server.Client(), port); err == nil {
		t.Fatal("controller failure must not masquerade as a zero-connection sample")
	}
	after, _ := os.ReadFile(file.Name())
	if string(after) != string(data) {
		t.Fatal("failed sample must preserve the previous sample and its timestamp")
	}
}

func TestTransparentDirectChainNames(t *testing.T) {
	for _, name := range []string{"DIRECT", "OriginalNetwork", "Local0", "Local12"} {
		if !isDirectChain(name) {
			t.Errorf("direct chain not recognized: %s", name)
		}
	}
	for _, name := range []string{"GoConnect", "REJECT", "Local", "Local-1", "Local+1", "Local01", "LocalProxy"} {
		if isDirectChain(name) {
			t.Errorf("non-direct chain misclassified: %s", name)
		}
	}
}
