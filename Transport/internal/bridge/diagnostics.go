package bridge

// Diagnostics observe explicitly marked STUN test transactions, never ordinary
// payloads, destinations, tokens or raw errors. They do not change packet data.
import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"
)

type Observation struct {
	Stage       string `json:"stage"`
	Outcome     string `json:"outcome"`
	Transaction uint64 `json:"transaction"`
	Flow        uint64 `json:"flow"`
	PacketKind  uint16 `json:"packetKind"`
	Bytes       int    `json:"bytes"`
	Errno       int    `json:"errno"`
	TimeMicros  int64  `json:"timeMicros"`
	Sequence    uint64 `json:"sequence"`
	Dropped     uint64 `json:"dropped"`
	PID         int    `json:"pid"`
}

type Observer func(Observation)

// One bounded queue, one writer and a finite per-session record budget. A slow
// consumer can lose diagnostics, but can never block packet forwarding.
type Diagnostics struct {
	queue    chan Observation
	sequence atomic.Uint64
	dropped  atomic.Uint64
	limit    uint64
}

func NewDiagnostics(ctx context.Context, consume Observer) *Diagnostics {
	d := &Diagnostics{queue: make(chan Observation, 128), limit: 8192}
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-d.queue:
				consume(e)
			case <-tick.C:
				if d.sequence.Load() > 0 {
					consume(Observation{Stage: "status", Outcome: "ok", TimeMicros: time.Now().UnixMicro(), Sequence: d.sequence.Load(), Dropped: d.dropped.Load(), PID: os.Getpid()})
				}
			}
		}
	}()
	return d
}

func (d *Diagnostics) Observe(e Observation) {
	e.TimeMicros = time.Now().UnixMicro()
	e.PID = os.Getpid()
	e.Sequence = d.sequence.Add(1)
	if e.Sequence > d.limit {
		d.dropped.Add(1)
		return
	}
	select {
	case d.queue <- e:
	default:
		d.dropped.Add(1)
	}
}

type probeID struct {
	transaction uint64
	kind        uint16
}

func markedSTUN(b []byte) probeID {
	if len(b) < 20 || int(binary.BigEndian.Uint16(b[2:4]))+20 != len(b) || len(b)%4 != 0 ||
		binary.BigEndian.Uint32(b[4:8]) != 0x2112a442 || string(b[8:12]) != "GCD1" {
		return probeID{}
	}
	kind := binary.BigEndian.Uint16(b[:2])
	id := binary.BigEndian.Uint64(b[12:20])
	if (kind != 1 && kind != 0x101 && kind != 0x111) || id == 0 || id > 1<<53-1 {
		return probeID{}
	}
	return probeID{id, kind}
}

func packetProbe(b []byte) probeID {
	if len(b) < 1 {
		return probeID{}
	}
	var h, end int
	switch b[0] >> 4 {
	case 4:
		if len(b) < 20 || b[9] != 17 || binary.BigEndian.Uint16(b[6:8])&0x3fff != 0 {
			return probeID{}
		}
		h = int(b[0]&15) * 4
		end = int(binary.BigEndian.Uint16(b[2:4]))
		if h < 20 {
			return probeID{}
		}
	case 6:
		// Extension headers and fragments are intentionally not traced. The
		// forwarding stack still handles them normally.
		if len(b) < 40 || b[6] != 17 {
			return probeID{}
		}
		h = 40
		end = 40 + int(binary.BigEndian.Uint16(b[4:6]))
	default:
		return probeID{}
	}
	if end != len(b) || end < h+8 || int(binary.BigEndian.Uint16(b[h+4:h+6])) != end-h {
		return probeID{}
	}
	return markedSTUN(b[h+8 : end])
}

func observe(o Observer, stage string, id probeID, flow uint64, n int, err error) {
	if o == nil || (id.transaction == 0 && err == nil) {
		return
	}
	e := Observation{Stage: stage, Outcome: "ok", Transaction: id.transaction, PacketKind: id.kind, Flow: flow, Bytes: n}
	if err != nil {
		e.Outcome = "io_error"
		var number syscall.Errno
		if errors.As(err, &number) {
			e.Errno = int(number)
		}
		var timeout net.Error
		switch {
		case errors.Is(err, context.Canceled):
			e.Outcome = "canceled"
		case errors.Is(err, io.ErrShortWrite):
			e.Outcome = "short_write"
		case errors.Is(err, net.ErrClosed):
			e.Outcome = "closed"
		case errors.As(err, &timeout) && timeout.Timeout():
			e.Outcome = "timeout"
		}
	}
	o(e)
}
