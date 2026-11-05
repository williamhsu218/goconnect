package bridge

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

type pressureWriter struct {
	until  time.Time
	err    error
	writes int
}

func (w *pressureWriter) Write(b []byte) (int, error) {
	if time.Now().Before(w.until) {
		return 0, w.err
	}
	w.writes++
	return len(b), nil
}

func TestPacketBackpressureRecoversBeyondOldDeadline(t *testing.T) {
	w := &pressureWriter{until: time.Now().Add(1200 * time.Millisecond), err: syscall.ENOBUFS}
	n, err := writePacket(context.Background(), w, []byte("one whole datagram"))
	if err != nil || n != 18 || w.writes != 1 {
		t.Fatalf("datagram lost or duplicated: %d %v %d", n, err, w.writes)
	}
}

func TestPacketBackpressureProtection(t *testing.T) {
	for _, e := range []error{syscall.ENOBUFS, syscall.EAGAIN, syscall.EPIPE} {
		w := &pressureWriter{until: time.Now().Add(time.Hour), err: e}
		start := time.Now()
		_, err := writePacketWithin(context.Background(), w, []byte("packet"), 40*time.Millisecond)
		if !errors.Is(err, e) || time.Since(start) > time.Second {
			t.Fatal("unbounded error", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := &pressureWriter{}
	if _, err := writePacket(ctx, w, []byte("packet")); !errors.Is(err, context.Canceled) || w.writes != 0 {
		t.Fatal("canceled packet written")
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_, err := writePacketWithin(context.Background(), a, []byte("packet"), 40*time.Millisecond)
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatal("blocking write had no deadline", err)
	}
	if _, err = writePacket(context.Background(), io.Discard, []byte("packet")); err != nil {
		t.Fatal(err)
	}
}
