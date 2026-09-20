package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceProtocolRejectsExtraAuthorityAndAmbiguousInput(t *testing.T) {
	accepted := []string{`{"action":"status"}`, `{"action":"route","session":"/private/tmp/example/session.json"}`}
	for _, value := range accepted {
		if _, err := decodeServiceRequest([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	rejected := []string{
		`{"action":"shell","command":"id"}`,
		`{"action":"route","session":"/tmp/a/session.json","uid":0}`,
		`{"action":"status","session":"/tmp/a/session.json"}`,
		`{"action":"route","session":"../session.json"}`,
		`{"action":"route","session":"/tmp/a/../session.json"}`,
		`{"action":"route","session":"/tmp/a/config.json"}`,
		`{"action":"route","session":"/tmp/a\n/session.json"}`,
		`{"action":"status"} {"action":"route"}`,
	}
	for _, value := range rejected {
		if _, err := decodeServiceRequest([]byte(value)); err == nil {
			t.Fatalf("accepted unsafe request: %s", value)
		}
	}
}

func TestServiceRevisionDetectsReplacementAndRejectsLinks(t *testing.T) {
	runtime := t.TempDir()
	if err := os.Mkdir(filepath.Join(runtime, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range serviceBinaries {
		if err := os.WriteFile(filepath.Join(runtime, "bin", name), []byte(name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"lib", "share"} {
		if err := os.Mkdir(filepath.Join(runtime, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(runtime, "share/cacert.pem"), []byte("fixture CA"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := runtimeRevision(runtime)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runtime, "bin", "openconnect")
	if err = os.WriteFile(path, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := runtimeRevision(runtime)
	if err != nil || after == before {
		t.Fatal("replacement was not detected", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(runtime, "bin", "GoConnectTransport"), path); err != nil {
		t.Fatal(err)
	}
	if _, err = runtimeRevision(runtime); err == nil {
		t.Fatal("accepted linked runtime")
	}
}

func TestServiceRejectsUserWritableRootPaths(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user")
	}
	if err := secureRootPath(t.TempDir(), true); err == nil {
		t.Fatal("accepted a user-owned service directory")
	}
}

func TestServiceAuditTokenBindsTheActualConnectingBinary(t *testing.T) {
	if os.Getenv("GOCONNECT_PEER_TEST") == "1" {
		conn, err := net.Dial("unix", os.Getenv("GOCONNECT_PEER_SOCKET"))
		if err != nil {
			os.Exit(2)
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("ready\n"))
		buffer := make([]byte, 1)
		_, _ = conn.Read(buffer)
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Darwin UNIX socket paths have a 104-byte limit.
	directory, err := os.MkdirTemp("/private/tmp", "goc-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "s")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestServiceAuditTokenBindsTheActualConnectingBinary$")
	cmd.Env = append(os.Environ(), "GOCONNECT_PEER_TEST=1", "GOCONNECT_PEER_SOCKET="+path)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	_ = listener.SetDeadline(time.Now().Add(5 * time.Second))
	conn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	id, err := verifyServicePeer(conn, self)
	if err != nil {
		t.Fatal("approved client rejected:", err)
	}
	if id.UID != uint32(os.Geteuid()) || id.PID != cmd.Process.Pid {
		t.Fatal("wrong kernel identity", fmt.Sprint(id))
	}
	if _, err = verifyServicePeer(conn, "/usr/bin/true"); err == nil {
		t.Fatal("mismatching code identity accepted")
	}
}
