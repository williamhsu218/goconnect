package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var errCompanySocketCleanup = errors.New("company socket cleanup incomplete")

// OpenConnect may exit before its hook has unwound. stopChild then kills the
// remaining owned group, so the hook's deferred listener.Close cannot unlink
// its socket. The session owns this final cleanup, after that group is gone.
func stopSessionChild(cmd *exec.Cmd, done <-chan error, companySocket string) error {
	return finishCompanySocketSession(companySocket, func() error { return stopChild(cmd, done) })
}

func finishCompanySocketSession(path string, stop func() error) error {
	if path == "" {
		return stop()
	}
	cleanup, captureErr := captureCompanySocketCleanup(path)
	if cleanup != nil && cleanup.directory != nil {
		defer cleanup.directory.Close()
	}
	// A capture failure must never skip stopping our processes. Conversely, an
	// unconfirmed stop must never unlink a potentially live backend's socket.
	if err := stop(); err != nil {
		return err
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil // The hook or the primary controller already cleaned up.
	}
	if captureErr != nil {
		return fmt.Errorf("%w: %v", errCompanySocketCleanup, captureErr)
	}
	if err := cleanup.removeStale(); err != nil {
		if _, gone := os.Lstat(path); os.IsNotExist(gone) {
			return nil // Primary teardown can concurrently remove its directory.
		}
		return fmt.Errorf("%w: %v", errCompanySocketCleanup, err)
	}
	return nil
}

type companySocketCleanup struct {
	path      string
	directory *os.File
	parent    os.FileInfo
	socket    os.FileInfo
}

func captureCompanySocketCleanup(path string) (*companySocketCleanup, error) {
	c := &companySocketCleanup{path: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := validateCompanySocketParent(path); err != nil {
		return c, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return c, errors.New("company cleanup target is not an owned private socket")
	}
	fd, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return c, err
	}
	c.directory = os.NewFile(uintptr(fd), filepath.Dir(path))
	c.parent, err = c.directory.Stat()
	if err != nil {
		return c, err
	}
	c.socket = info
	return c, c.sameIdentity()
}

func (c *companySocketCleanup) sameIdentity() error {
	if c.directory == nil || c.parent == nil || c.socket == nil {
		return errors.New("company socket was not present before stop")
	}
	if err := validateCompanySocketParent(c.path); err != nil {
		return err
	}
	parent, err := os.Lstat(filepath.Dir(c.path))
	if err != nil || !os.SameFile(parent, c.parent) {
		return errors.New("company socket directory changed")
	}
	current, err := os.Lstat(c.path)
	if err != nil || !os.SameFile(current, c.socket) || current.Mode() != c.socket.Mode() {
		return errors.New("company socket identity changed")
	}
	return nil
}

func (c *companySocketCleanup) removeStale() error {
	if err := c.sameIdentity(); err != nil {
		return err
	}
	probe, err := net.DialTimeout("unix", c.path, 250*time.Millisecond)
	if err == nil {
		probe.Close()
		return errors.New("company socket still has a listener")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return errors.New("company socket was not confirmed stale")
	}
	if err := c.sameIdentity(); err != nil {
		return err
	}
	// Anchor unlink to the original directory; never remove the reservation,
	// lease, routes, DNS, credentials or anything else in the primary session.
	return unix.Unlinkat(int(c.directory.Fd()), filepath.Base(c.path), 0)
}
