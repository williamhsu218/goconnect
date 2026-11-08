package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func companyUnlinklessListener(t *testing.T, path string) *net.UnixListener {
	t.Helper()
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	t.Cleanup(func() { l.Close() })
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCompanySessionStopAllowsRepeatedBackendReconnect(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	for i := 0; i < 4; i++ {
		l := companyUnlinklessListener(t, path)
		// Model the hook's SIGKILL: descriptors close, but defer/unlink never runs.
		if err := finishCompanySocketSession(path, l.Close); err != nil {
			t.Fatal(i, err)
		}
		if err := validateCompanySocketPath(path); err != nil {
			t.Fatal("next attempt was blocked by its predecessor", err)
		}
	}
}

func TestCompanySessionStopPreservesSocketWhenStopUnconfirmed(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	l := companyUnlinklessListener(t, path)
	stopping := errors.New("owned process group still alive")
	if err := finishCompanySocketSession(path, func() error { return stopping }); !errors.Is(err, stopping) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("unconfirmed cleanup unlinked the socket", err)
	}
	l.Close()
}

func TestCompanySessionStopPreservesLiveListener(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	l := companyUnlinklessListener(t, path)
	if err := finishCompanySocketSession(path, func() error { return nil }); !errors.Is(err, errCompanySocketCleanup) {
		t.Fatal("live listener accepted as stale", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("live socket unlinked", err)
	}
	l.Close()
}

func TestCompanySessionStopPreservesReplacement(t *testing.T) {
	for _, kind := range []string{"file", "socket", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(companyTestSocketDir(t), "company.sock")
			l := companyUnlinklessListener(t, path)
			err := finishCompanySocketSession(path, func() error {
				l.Close()
				if err := os.Rename(path, path+".old"); err != nil {
					return err
				}
				switch kind {
				case "file":
					return os.WriteFile(path, []byte("keep"), 0600)
				case "socket":
					companyUnlinklessListener(t, path).Close()
					return nil
				default:
					return os.Symlink(path+".old", path)
				}
			})
			if !errors.Is(err, errCompanySocketCleanup) {
				t.Fatal("replacement not rejected", err)
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("replacement unlinked", err)
			}
		})
	}
}

func TestCompanySessionStopHandlesAlreadyRemovedSocket(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	l := companyUnlinklessListener(t, path)
	if err := finishCompanySocketSession(path, func() error { l.Close(); return os.Remove(path) }); err != nil {
		t.Fatal(err)
	}
}

func TestCompanySessionStopRejectsSocketCreatedDuringStop(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	err := finishCompanySocketSession(path, func() error { companyUnlinklessListener(t, path).Close(); return nil })
	if !errors.Is(err, errCompanySocketCleanup) {
		t.Fatal("unowned late socket not rejected", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("late socket removed", err)
	}
}

func TestCompanySessionStopStillStopsWhenCaptureFails(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	called := 0
	err := finishCompanySocketSession(path, func() error { called++; return nil })
	if called != 1 || !errors.Is(err, errCompanySocketCleanup) {
		t.Fatal(called, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep" {
		t.Fatal("non-socket changed", err)
	}
}

func TestCompanySessionStopLeavesLegacySessionUnchanged(t *testing.T) {
	called := 0
	if err := finishCompanySocketSession("", func() error { called++; return nil }); err != nil || called != 1 {
		t.Fatal(called, err)
	}
}

func TestCompanySessionStopPreservesChangedDirectory(t *testing.T) {
	dir := companyTestSocketDir(t)
	path := filepath.Join(dir, "company.sock")
	l := companyUnlinklessListener(t, path)
	old := dir + "-original"
	t.Cleanup(func() { os.RemoveAll(old) })
	err := finishCompanySocketSession(path, func() error {
		l.Close()
		if err := os.Rename(dir, old); err != nil {
			return err
		}
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		companyUnlinklessListener(t, path).Close()
		return nil
	})
	if !errors.Is(err, errCompanySocketCleanup) {
		t.Fatal("replacement directory not rejected", err)
	}
	for _, name := range []string{path, filepath.Join(old, "company.sock")} {
		if _, err := os.Lstat(name); err != nil {
			t.Fatal("socket removed across directory identity change", err)
		}
	}
}

func TestCompanySessionStopPreservesSocketAfterPermissionsChange(t *testing.T) {
	dir := companyTestSocketDir(t)
	path := filepath.Join(dir, "company.sock")
	l := companyUnlinklessListener(t, path)
	err := finishCompanySocketSession(path, func() error { l.Close(); return os.Chmod(dir, 0755) })
	if !errors.Is(err, errCompanySocketCleanup) {
		t.Fatal("non-private directory accepted", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("socket unlinked from a non-private directory", err)
	}
}

func TestCompanySessionOccupiedSocketHasLocalValidationError(t *testing.T) {
	path := filepath.Join(companyTestSocketDir(t), "company.sock")
	l := companyUnlinklessListener(t, path)
	defer l.Close()
	if err := validateCompanySocketPath(path); !errors.Is(err, errCompanySocketOccupied) {
		t.Fatal("occupied socket cannot be distinguished from credentials", err)
	}
}
