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
	directory := t.TempDir()
	status := filepath.Join(directory, "status")
	dir, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	control := controlFiles{directory: dir, uid: uint32(os.Getuid()), request: routeRequest{Token: "fixture-token", AppPaths: []string{"/Applications/Example.app"}},
		snapshot: routeSnapshot{CaptureMode: "transparent-routes", RoutingMode: "whitelist"}}
	port := uint16(server.Listener.Addr().(*net.TCPAddr).Port)
	if err := control.refreshConnections(server.Client(), port); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(status)
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
	after, _ := os.ReadFile(status)
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

func TestRouteStatusPublishesCompleteSnapshots(t *testing.T) {
	directory := controlFixture(t)
	control, err := openControl(filepath.Join(directory, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.close()

	path := filepath.Join(directory, "status")
	control.snapshot.Apps = []appRouteStatus{{Path: "/Applications/Example.app", VPN: 3}}
	control.write("ready")
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	control.snapshot.Apps = nil
	control.snapshot.FailureCode = "physical_network_changed"
	control.write("failed")
	var previous routeSnapshot
	if err := json.NewDecoder(reader).Decode(&previous); err != nil || previous.State != "ready" || len(previous.Apps) != 1 {
		t.Fatalf("reader lost its original complete snapshot: %q, %v", previous.State, err)
	}
	latest, err := readRouteSnapshot(path)
	if err != nil || latest.State != "failed" || latest.FailureCode != "physical_network_changed" {
		t.Fatalf("new reader missed the terminal status: %+v, %v", latest, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("status is not private: %v", info.Mode().Perm())
	}
}
