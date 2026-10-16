package main

import (
	"os"
	"path/filepath"
)

// Fixed small snapshot survives JSONL rotation and temporary session cleanup.
// Existing unsafe files are rejected; rename never follows a destination link.
func writeExitSnapshot(path string, data []byte) error {
	if len(data) > 32768 {
		return os.ErrInvalid
	}
	if f, err := secureDiagnosticFile(path, os.O_RDONLY); err == nil {
		f.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".exit-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
