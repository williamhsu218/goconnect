package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServiceNamespacesSeparatePersistentAuthorityButShareCapture(t *testing.T) {
	production, err := serviceNamespaceForFlavor("production")
	if err != nil {
		t.Fatal(err)
	}
	test, err := serviceNamespaceForFlavor("test")
	if err != nil {
		t.Fatal(err)
	}
	if production.Label != "com.willhsu.GoConnect.network" || production.Base != "/Library/PrivilegedHelperTools/com.willhsu.GoConnect" || production.RunDirectory != production.Base+"/run" || production.Socket != production.RunDirectory+"/network.sock" || production.Plist != "/Library/LaunchDaemons/com.willhsu.GoConnect.network.plist" || production.PolicyPath != production.Base+"/service.json" {
		t.Fatalf("production namespace drift: %+v", production)
	}
	if test.Label != "com.willhsu.GoConnect.DualVPNTest.network" || test.Base != "/Library/PrivilegedHelperTools/com.willhsu.GoConnect.DualVPNTest" {
		t.Fatalf("test namespace: %+v", test)
	}
	for name, pair := range map[string][2]string{"label": {production.Label, test.Label}, "base": {production.Base, test.Base}, "run": {production.RunDirectory, test.RunDirectory}, "socket": {production.Socket, test.Socket}, "plist": {production.Plist, test.Plist}, "policy": {production.PolicyPath, test.PolicyPath}} {
		if pair[0] == pair[1] {
			t.Errorf("shared %s", name)
		}
	}
	if production.CaptureLock != "/var/run/com.willhsu.GoConnect.capture.lock" || test.CaptureLock != production.CaptureLock {
		t.Fatal("capture lock must remain global")
	}
	if serviceLabel != selectedServiceNamespace.Label || serviceBase != selectedServiceNamespace.Base || serviceRunDirectory != selectedServiceNamespace.RunDirectory || serviceSocket != selectedServiceNamespace.Socket || servicePlist != selectedServiceNamespace.Plist || servicePolicyPath != selectedServiceNamespace.PolicyPath {
		t.Fatal("service globals do not follow selected namespace")
	}
	for _, bad := range []string{"", "prod", "TEST", "test/../production", "production\n"} {
		if _, e := serviceNamespaceForFlavor(bad); e == nil {
			t.Fatalf("accepted flavor %q", bad)
		}
	}
}
func TestServiceProductionStatusCommandHasFixedOrdinaryUserBoundary(t *testing.T) {
	cmd := productionStatusCommand(context.Background())
	if cmd.Path != productionStatusExecutable || !reflect.DeepEqual(cmd.Args, []string{productionStatusExecutable, "service-status"}) {
		t.Fatalf("unexpected executable/arguments: %s %q", cmd.Path, cmd.Args)
	}
	if !reflect.DeepEqual(cmd.Env, []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=en_US.UTF-8"}) || cmd.Stdin != nil || cmd.Stderr == nil {
		t.Fatal("status command inherits authority or sensitive output")
	}
	if cmd.WaitDelay <= 0 || cmd.WaitDelay > time.Second {
		t.Fatal("unbounded pipe drain")
	}
}
func serviceNamespaceFixtureStat(t *testing.T) func(string) (os.FileInfo, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture")
	if e := os.WriteFile(path, []byte("fixture"), 0700); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	directory, e := os.Stat(filepath.Dir(path))
	if e != nil {
		t.Fatal(e)
	}
	return func(s string) (os.FileInfo, error) {
		if s == productionAppDirectory {
			return directory, nil
		}
		if s != productionStatusExecutable {
			t.Errorf("unexpected lookup %s", s)
		}
		return info, nil
	}
}
func TestServiceTestHostStatusFailsClosedWithoutSensitivePassThrough(t *testing.T) {
	stat := serviceNamespaceFixtureStat(t)
	tests := []struct {
		label, json string
		available   bool
		state       string
	}{
		{"ready idle", `{"state":"ready","busy":false}`, true, "available"},
		{"not installed", `{"state":"notInstalled","busy":false}`, true, "available"},
		{"busy ready", `{"state":"ready","busy":true}`, false, "blocked"},
		{"busy notinstalled", `{"state":"notInstalled","busy":true}`, false, "blocked"},
		{"missing busy", `{"state":"ready"}`, false, "unknown"},
		{"null busy", `{"state":"ready","busy":null}`, false, "unknown"},
		{"missing state", `{"busy":false}`, false, "unknown"},
		{"unknown", `{"state":"secret-token","busy":true}`, false, "unknown"},
		{"needs update", `{"state":"needsUpdate","busy":false}`, false, "unknown"},
		{"unavailable", `{"state":"unavailable","busy":false}`, false, "unknown"},
		{"requires auth", `{"state":"requiresAuthorization","busy":false}`, false, "unknown"},
		{"trailing", `{"state":"ready","busy":false} {"state":"ready","busy":false}`, false, "unknown"},
		{"malformed", `{"state":"ready","busy":"false"}`, false, "unknown"},
		{"oversize", strings.Repeat("x", productionStatusLimit+1), false, "unknown"},
		{"message not copied", `{"state":"ready","busy":true,"message":"secret-token"}`, false, "blocked"},
	}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			calls := 0
			reply := testHostStatusWith(context.Background(), "test", 501, 501, stat, func(ctx context.Context) ([]byte, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > productionStatusTimeout || time.Until(deadline) <= 0 {
					t.Fatal("missing bounded timeout")
				}
				return []byte(test.json), nil
			})
			if reply.Available != test.available || reply.State != test.state || calls != 1 {
				t.Fatalf("status=%+v calls=%d", reply, calls)
			}
			if strings.Contains(reply.Reason, "secret-token") || strings.Contains(reply.ProductionState, "secret-token") {
				t.Fatal("copied untrusted sensitive output")
			}
		})
	}
	for _, failure := range []error{errors.New("provider secret-token"), context.DeadlineExceeded} {
		reply := testHostStatusWith(context.Background(), "test", 501, 501, stat, func(context.Context) ([]byte, error) { return []byte(`{"state":"ready","busy":false}`), failure })
		if reply.Available || reply.State != "unknown" || strings.Contains(reply.Reason, "secret-token") {
			t.Fatalf("failed process accepted: %+v", reply)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	reply := testHostStatusWith(canceled, "test", 501, 501, stat, func(context.Context) ([]byte, error) { return []byte(`{"state":"ready","busy":false}`), nil })
	if reply.Available {
		t.Fatal("canceled check accepted")
	}
}
func TestServiceTestHostStatusNeverExecutesAsRootOrProductionFlavor(t *testing.T) {
	for _, test := range []struct {
		flavor    string
		uid, euid int
	}{{"production", 501, 501}, {"test", 0, 0}, {"test", 501, 0}, {"test", 0, 501}, {"other", 501, 501}} {
		reply := testHostStatusWith(context.Background(), test.flavor, test.uid, test.euid, func(string) (os.FileInfo, error) { t.Fatal("forbidden caller checked app"); return nil, nil }, func(context.Context) ([]byte, error) { t.Fatal("forbidden caller executed app"); return nil, nil })
		if reply.Available {
			t.Fatalf("forbidden caller accepted %+v", test)
		}
	}
}
func TestServiceTestHostMissingAppDistinguishedFromPermissionFailure(t *testing.T) {
	for _, test := range []struct {
		err       error
		available bool
	}{{os.ErrNotExist, true}, {os.ErrPermission, false}, {errors.New("filesystem error"), false}} {
		reply := testHostStatusWith(context.Background(), "test", 501, 501, func(string) (os.FileInfo, error) { return nil, test.err }, func(context.Context) ([]byte, error) { t.Fatal("executed absent/unreadable app"); return nil, nil })
		if reply.Available != test.available {
			t.Fatalf("error=%v reply=%+v", test.err, reply)
		}
	}
}
func TestServiceTestHostIncompleteAppIsUnknown(t *testing.T) {
	fixture := serviceNamespaceFixtureStat(t)
	reply := testHostStatusWith(context.Background(), "test", 501, 501, func(path string) (os.FileInfo, error) {
		if path == productionAppDirectory {
			return fixture(path)
		}
		return nil, os.ErrNotExist
	}, func(context.Context) ([]byte, error) { t.Fatal("executed incomplete app"); return nil, nil })
	if reply.Available || reply.State != "unknown" {
		t.Fatalf("incomplete app accepted: %+v", reply)
	}
}
func TestServiceStatusOutputBoundCancelsAndDoesNotRetainOverflow(t *testing.T) {
	calls := 0
	b := &boundedStatusOutput{limit: 8, cancel: func() { calls++ }}
	b.Write([]byte("1234"))
	b.Write([]byte("567890secret"))
	data, overflow := b.result()
	if string(data) != "12345678" || !overflow || calls != 1 {
		t.Fatalf("data=%q overflow=%v cancel=%d", data, overflow, calls)
	}
	data[0] = 'x'
	again, _ := b.result()
	if string(again) != "12345678" {
		t.Fatal("result aliases retained buffer")
	}
}
func TestServiceExistingCaptureGuardIsNonCreatingExclusiveAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.lock")
	if f, e := openExistingCaptureLock(path, uint32(os.Geteuid())); e == nil {
		f.Close()
		t.Fatal("missing coordination lock accepted")
	}
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		t.Fatal("guard created missing lock", e)
	}
	if e := os.WriteFile(path, []byte("untouched"), 0600); e != nil {
		t.Fatal(e)
	}
	before, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := openExistingCaptureLock(path, uint32(os.Geteuid()))
	if e != nil {
		t.Fatal(e)
	}
	if duplicate, e := openExistingCaptureLock(path, uint32(os.Geteuid())); e == nil {
		duplicate.Close()
		lock.Close()
		t.Fatal("second capture lock accepted")
	}
	lock.Close()
	again, e := openExistingCaptureLock(path, uint32(os.Geteuid()))
	if e != nil {
		t.Fatal("lock not released", e)
	}
	again.Close()
	after, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if !os.SameFile(before, after) || string(data) != "untouched" || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("capture inode/data mutated")
	}
}
func TestServiceExistingCaptureGuardRejectsUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.lock")
	if e := os.WriteFile(path, []byte("fixture"), 0644); e != nil {
		t.Fatal(e)
	}
	if f, e := openExistingCaptureLock(path, uint32(os.Geteuid())); e == nil {
		f.Close()
		t.Fatal("public lock accepted")
	}
	os.Chmod(path, 0600)
	if f, e := openExistingCaptureLock(path, uint32(os.Geteuid())+1); e == nil {
		f.Close()
		t.Fatal("wrong owner accepted")
	}
	alias := filepath.Join(dir, "alias")
	if e := os.Link(path, alias); e != nil {
		t.Fatal(e)
	}
	if f, e := openExistingCaptureLock(path, uint32(os.Geteuid())); e == nil {
		f.Close()
		t.Fatal("hardlinked lock accepted")
	}
	os.Remove(alias)
	link := filepath.Join(dir, "linked")
	os.Symlink(path, link)
	if f, e := openExistingCaptureLock(link, uint32(os.Geteuid())); e == nil {
		f.Close()
		t.Fatal("symlink lock accepted")
	}
	fifo := filepath.Join(dir, "fifo")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	if f, e := openExistingCaptureLock(fifo, uint32(os.Geteuid())); e == nil {
		f.Close()
		t.Fatal("nonregular lock accepted")
	}
}
