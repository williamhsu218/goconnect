package bridge

import (
	"context"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
)

const udpDestinationLimit = 128
const udpDestinationQueue = 8
const udpQueueByteLimit = 1 << 20
const udpPacketLifetime = 75 * time.Second

type queuedDatagram struct {
	body     []byte
	deadline time.Time
}

type udpDestination struct {
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan queuedDatagram
	flow   uint64
}

type udpAssociationState struct {
	server      *SOCKSServer
	ctx         context.Context
	local       *net.UDPConn
	mu          sync.Mutex
	remotes     map[string]*udpDestination
	queuedBytes int
	active      int
	workers     sync.WaitGroup
}

// Only this association's receive loop enqueues. Neither dial nor write runs
// here: one slow destination must not stop dispatch to healthy destinations.
// Bound queued payloads before copying; no goroutine is created per datagram.
func (a *udpAssociationState) enqueue(address string, client *net.UDPAddr, body []byte) {
	a.mu.Lock()
	if a.ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	d := a.remotes[address]
	if a.queuedBytes+len(body) > udpQueueByteLimit || (d == nil && a.active >= udpDestinationLimit) || (d != nil && (len(d.queue) == cap(d.queue) || d.ctx.Err() != nil)) {
		flow := uint64(0)
		if d != nil {
			flow = d.flow
		}
		a.mu.Unlock()
		observe(a.server.Observe, "udp_queue", markedSTUN(body), flow, len(body), syscall.ENOBUFS)
		return
	}
	if d == nil {
		ctx, cancel := context.WithCancel(a.ctx)
		d = &udpDestination{ctx: ctx, cancel: cancel, queue: make(chan queuedDatagram, udpDestinationQueue), flow: a.server.flows.Add(1)}
		a.remotes[address] = d
		a.active++
		a.workers.Add(1)
		go a.run(address, client, d)
	}
	packet := queuedDatagram{body: append([]byte(nil), body...), deadline: time.Now().Add(udpPacketLifetime)}
	a.queuedBytes += len(body)
	d.queue <- packet // capacity checked above; this loop is the only producer
	a.mu.Unlock()
}

func (a *udpAssociationState) dequeue(d *udpDestination) (queuedDatagram, bool) {
	select {
	case <-d.ctx.Done():
		return queuedDatagram{}, false
	case packet := <-d.queue:
		a.mu.Lock()
		a.queuedBytes -= len(packet.body)
		a.mu.Unlock()
		return packet, true
	}
}

func (a *udpAssociationState) run(address string, client *net.UDPAddr, d *udpDestination) {
	defer a.workers.Done()
	var remote net.Conn
	var readerDone chan struct{}
	var stopClose func() bool
	defer func() {
		// Remove the cache entry before Close can wake the caller or old reader.
		// Identity comparison prevents an old reader retiring a replacement.
		a.mu.Lock()
		if a.remotes[address] == d {
			delete(a.remotes, address)
		}
		a.mu.Unlock()
		d.cancel()
		if remote != nil {
			_ = remote.Close()
		}
		if stopClose != nil {
			stopClose()
		}
		if readerDone != nil {
			<-readerDone
		}
		for {
			select {
			case p := <-d.queue:
				a.mu.Lock()
				a.queuedBytes -= len(p.body)
				a.mu.Unlock()
				observe(a.server.Observe, "udp_queue", markedSTUN(p.body), d.flow, len(p.body), context.Canceled)
			default:
				a.mu.Lock()
				a.active--
				a.mu.Unlock()
				return
			}
		}
	}()
	packet, ok := a.dequeue(d)
	if !ok || d.ctx.Err() != nil {
		return
	}
	dialCtx, stop := context.WithTimeout(d.ctx, 10*time.Second)
	var err error
	remote, err = a.server.Dialer.DialContext(dialCtx, "udp", address)
	stop()
	if err != nil {
		observe(a.server.Observe, "udp_dial", markedSTUN(packet.body), d.flow, 0, err)
		return
	}
	stopClose = context.AfterFunc(d.ctx, func() { _ = remote.Close() })
	readerDone = make(chan struct{})
	go func() {
		defer close(readerDone)
		defer d.cancel()
		a.readReplies(d, remote, client)
	}()
	for {
		id := markedSTUN(packet.body)
		observe(a.server.Observe, "udp_write_start", id, d.flow, len(packet.body), nil)
		written := 0
		if remaining := time.Until(packet.deadline); remaining <= 0 {
			err = context.DeadlineExceeded
		} else {
			// Queue and dial time count toward the existing total packet budget.
			written, err = writePacketWithin(d.ctx, remote, packet.body, remaining)
		}
		if err == nil && written != len(packet.body) {
			err = io.ErrShortWrite
		}
		observe(a.server.Observe, "udp_write", id, d.flow, written, err)
		if err != nil {
			return
		} // discard pending packets; never replay ambiguous writes
		packet, ok = a.dequeue(d)
		if !ok {
			return
		}
	}
}

func (a *udpAssociationState) readReplies(d *udpDestination, remote net.Conn, client *net.UDPAddr) {
	encoded, err := encodedAddress(remote.RemoteAddr().String())
	if err != nil {
		return
	}
	body := make([]byte, 65000)
	for {
		_ = remote.SetReadDeadline(time.Now().Add(90 * time.Second))
		n, err := remote.Read(body)
		if err != nil {
			if d.ctx.Err() != nil {
				err = d.ctx.Err()
			}
			observe(a.server.Observe, "udp_read", probeID{}, d.flow, n, err)
			return
		}
		id := markedSTUN(body[:n])
		observe(a.server.Observe, "udp_read", id, d.flow, n, nil)
		packet := append([]byte{0, 0, 0}, encoded...)
		packet = append(packet, body[:n]...)
		if _, err = a.local.WriteToUDP(packet, client); err != nil {
			observe(a.server.Observe, "socks_send", id, d.flow, n, err)
			return
		}
		observe(a.server.Observe, "socks_send", id, d.flow, n, nil)
	}
}
