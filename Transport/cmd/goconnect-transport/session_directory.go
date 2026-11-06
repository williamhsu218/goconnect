package main

import (
	"errors"
	"os"
	"path/filepath"
)

// No child or route exists yet. A failed first journal write must not leave an
// unreadable session directory that blocks every later connection and repair.
func initializeSessionDirectory(base, prefix string, save func(string) error) (string, error) {
	dir, err := os.MkdirTemp(base, prefix)
	if err != nil {
		return "", err
	}
	if err = save(filepath.Join(dir, "state.json")); err != nil {
		return "", errors.Join(err, os.RemoveAll(dir))
	}
	return dir, nil
}
