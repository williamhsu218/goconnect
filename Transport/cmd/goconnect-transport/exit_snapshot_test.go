package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExitSnapshotAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.exit.json")
	for _, content := range []string{`{"event":"session.exit_first"}`, `{"event":"session.exit_final"}`} {
		if err := writeExitSnapshot(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		data, readErr := os.ReadFile(path)
		if err != nil || readErr != nil || st.Mode().Perm() != 0600 || string(data) != content+"\n" {
			t.Fatal("snapshot incomplete or public", st, err, readErr)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary snapshot residue", entries)
	}
	if err := writeExitSnapshot(path, make([]byte, 32769)); err == nil {
		t.Fatal("unbounded snapshot")
	}
}

func TestExitSnapshotRejectsUnsafeTargets(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "public"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target, path := filepath.Join(dir, "target"), filepath.Join(dir, "worker.exit.json")
			if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "hardlink":
				err = os.Link(target, path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "public":
				err = os.WriteFile(path, nil, 0600)
				if err == nil {
					err = os.Chmod(path, 0644)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = writeExitSnapshot(path, []byte("replacement")); err == nil {
				t.Fatal("unsafe snapshot accepted")
			}
			if data, _ := os.ReadFile(target); string(data) != "untouched" {
				t.Fatal("target changed")
			}
		})
	}
}
