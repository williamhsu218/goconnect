package bridge

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"
)

type tcpProgressWriter struct {
	io.Writer
	progress func()
}

func (w tcpProgressWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n > 0 {
		w.progress()
	}
	return n, err
}

// Company-only opt-in. EOF still half-closes the corresponding write direction
// so delayed responses remain readable. A terminal copy error closes both ends;
// otherwise, after the first EOF, only a period without successful forwarding
// expires the drain. Active responses have no fixed total lifetime.
func relayTCPWithIdleDrain(ctx context.Context, left, right net.Conn, idle time.Duration) {
	defer left.Close()
	defer right.Close()
	started := time.Now()
	var lastProgress atomic.Int64
	activity := make(chan struct{}, 1)
	progress := func() {
		// Duration from a retained time.Time uses its monotonic clock. CAS max
		// prevents concurrently completed writes from moving activity backward.
		now := time.Since(started).Nanoseconds()
		for previous := lastProgress.Load(); now > previous; previous = lastProgress.Load() {
			if lastProgress.CompareAndSwap(previous, now) {
				break
			}
		}
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	finished := make(chan error, 2)
	copyDirection := func(dst, src net.Conn) {
		_, err := io.Copy(tcpProgressWriter{Writer: dst, progress: progress}, src)
		if err == nil || errors.Is(err, io.EOF) {
			if half, ok := dst.(interface{ CloseWrite() error }); ok {
				err = half.CloseWrite()
			} else {
				err = errors.ErrUnsupported
			}
		}
		finished <- err
	}
	go copyDirection(right, left)
	go copyDirection(left, right)
	var timer *time.Timer
	var timeout <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	remaining := func() time.Duration {
		return idle - (time.Since(started) - time.Duration(lastProgress.Load()))
	}
	reset := func(wait time.Duration) {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
	}
	completed := 0
	for completed < 2 {
		select {
		case err := <-finished:
			completed++
			if err != nil {
				return
			}
			if completed == 1 {
				progress()
				timer = time.NewTimer(idle)
				timeout = timer.C
			}
		case <-activity:
			if timer != nil {
				if wait := remaining(); wait > 0 {
					reset(wait)
				}
			}
		case <-timeout:
			if wait := remaining(); wait > 0 {
				reset(wait)
			} else {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
