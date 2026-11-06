package bridge

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type pendingUDPDialer struct{ started chan string }

func (d pendingUDPDialer) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	d.started <- address
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestUDPAssociationQueueAndWorkerBounds(t *testing.T) {
	for _, kind := range []string{"destination", "bytes", "workers"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan string, 128)
			var drops atomic.Int32
			s := &SOCKSServer{Dialer: pendingUDPDialer{started}, Observe: func(o Observation) {
				if o.Stage == "udp_queue" && o.Outcome == "io_error" {
					drops.Add(1)
				}
			}}
			a := &udpAssociationState{server: s, ctx: ctx, remotes: make(map[string]*udpDestination)}
			body := []byte("queued")
			if kind == "bytes" {
				body = make([]byte, 60000)
			}
			a.enqueue("192.0.2.1:1", nil, body)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("dial not started")
			}
			switch kind {
			case "destination":
				for i := 0; i < udpDestinationQueue+10; i++ {
					a.enqueue("192.0.2.1:1", nil, body)
				}
			case "bytes":
				for i := 0; i < 128; i++ {
					a.enqueue(fmt.Sprintf("192.0.2.1:%d", i/8+1), nil, body)
				}
			case "workers":
				for i := 2; i <= udpDestinationLimit; i++ {
					a.enqueue(fmt.Sprintf("192.0.2.1:%d", i), nil, body)
				}
				a.enqueue("192.0.2.1:129", nil, body)
			}
			a.mu.Lock()
			if a.active > udpDestinationLimit || a.queuedBytes > udpQueueByteLimit {
				t.Error("unbounded association", a.active, a.queuedBytes)
			}
			for _, d := range a.remotes {
				if len(d.queue) > udpDestinationQueue {
					t.Error("unbounded destination")
				}
			}
			a.mu.Unlock()
			if drops.Load() == 0 {
				t.Error("overflow silently accepted")
			}
			cancel()
			done := make(chan struct{})
			go func() { a.workers.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("workers did not stop")
			}
			if a.active != 0 || a.queuedBytes != 0 || len(a.remotes) != 0 {
				t.Fatal("queue or endpoint leaked after cancellation")
			}
		})
	}
}

type orderedUDP struct {
	*failedUDP
	gate    chan struct{}
	written chan string
}

func (c *orderedUDP) Write(b []byte) (int, error) {
	select {
	case <-c.gate:
	case <-c.closed:
		return 0, net.ErrClosed
	}
	c.written <- string(b)
	return len(b), nil
}

func TestUDPQueueOwnsPayloadAndPreservesOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &orderedUDP{failedUDP: &failedUDP{closed: make(chan struct{})}, gate: make(chan struct{}), written: make(chan string, 10)}
	a := &udpAssociationState{server: &SOCKSServer{Dialer: fixedUDPDialer{c}}, ctx: ctx, remotes: make(map[string]*udpDestination)}
	body := []byte("first")
	a.enqueue("192.0.2.1:1", nil, body)
	copy(body, "other") // the receive buffer will be reused by the next datagram
	a.enqueue("192.0.2.1:1", nil, []byte("second"))
	close(c.gate)
	for _, want := range []string{"first", "second"} {
		select {
		case got := <-c.written:
			if got != want {
				t.Fatalf("order or ownership: got %q want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("write missing")
		}
	}
	cancel()
	a.workers.Wait()
	select {
	case extra := <-c.written:
		t.Fatal("duplicated datagram", extra)
	default:
	}
}

func TestUDPExpiredQueuedPacketIsNotWritten(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &failedUDP{closed: make(chan struct{})}
	s := &SOCKSServer{Dialer: fixedUDPDialer{c}}
	dctx, dcancel := context.WithCancel(ctx)
	d := &udpDestination{ctx: dctx, cancel: dcancel, queue: make(chan queuedDatagram, 1)}
	d.queue <- queuedDatagram{body: []byte("late"), deadline: time.Now().Add(-time.Second)}
	a := &udpAssociationState{server: s, ctx: ctx, remotes: map[string]*udpDestination{"192.0.2.1:1": d}, active: 1, queuedBytes: 4}
	a.workers.Add(1)
	a.run("192.0.2.1:1", nil, d)
	if c.writes.Load() != 0 || a.active != 0 || a.queuedBytes != 0 {
		t.Fatal("expired packet sent or leaked")
	}
}

type isolatedUDPDialer struct {
	blocked     *blockingUDP
	echo        string
	blockDial   bool
	dialStarted chan struct{}
	dialStopped chan struct{}
}

func (d *isolatedUDPDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if address == "192.0.2.2:5353" {
		if d.blockDial {
			close(d.dialStarted)
			<-ctx.Done()
			close(d.dialStopped)
			return nil, ctx.Err()
		}
		return d.blocked, nil
	}
	return (&net.Dialer{}).DialContext(ctx, "udp", d.echo)
}

func sendUDP(t *testing.T, u net.Conn, address, body string) {
	t.Helper()
	a, err := encodedAddress(address)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = u.Write(append(append([]byte{0, 0, 0}, a...), []byte(body)...)); err != nil {
		t.Fatal(err)
	}
}

func TestUDPAssociationIsolatesBlockedDestination(t *testing.T) {
	for _, dial := range []bool{false, true} {
		name := "write"
		if dial {
			name = "dial"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			echo, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer echo.Close()
			go func() {
				b := make([]byte, 100)
				for {
					n, p, err := echo.ReadFrom(b)
					if err != nil {
						return
					}
					echo.WriteTo(b[:n], p)
				}
			}()
			blocked := &blockingUDP{failedUDP: &failedUDP{closed: make(chan struct{})}, started: make(chan struct{})}
			d := &isolatedUDPDialer{blocked: blocked, echo: echo.LocalAddr().String(), blockDial: dial, dialStarted: make(chan struct{}), dialStopped: make(chan struct{})}
			control, u := udpAssociation(t, ctx, &SOCKSServer{Dialer: d, Token: "fixture-token"})
			sendUDP(t, u, "192.0.2.2:5353", "blocked")
			started := blocked.started
			if dial {
				started = d.dialStarted
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("blocked destination never started")
			}
			u.SetDeadline(time.Now().Add(time.Second))
			sendUDP(t, u, "192.0.2.3:5353", "healthy")
			b := make([]byte, 100)
			n, err := u.Read(b)
			if err != nil {
				t.Fatal("one blocked destination stalled healthy destination", err)
			}
			r := &sliceReader{data: b[3:n]}
			if _, err = readAddress(r); err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(r)
			if string(body) != "healthy" {
				t.Fatalf("wrong packet: %q", body)
			}
			control.Close()
			stopped := blocked.closed
			if dial {
				stopped = d.dialStopped
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("control close did not cancel blocked destination")
			}
		})
	}
}
