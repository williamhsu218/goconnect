package bridge

import (
	"context"
	"encoding/binary"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func markedProbe(id uint64) []byte {
	b := []byte{0, 1, 0, 0, 0x21, 0x12, 0xa4, 0x42, 'G', 'C', 'D', '1', 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint64(b[12:], id)
	return b
}

func TestProbeParserRejectsOrdinaryMalformedAndFragmentedPackets(t *testing.T) {
	b := markedProbe(42)
	if markedSTUN(b).transaction != 42 {
		t.Fatal("marked probe lost")
	}
	for _, offset := range []int{0, 3, 4, 8, 12} {
		bad := append([]byte{}, b...)
		bad[offset] ^= 0xff
		if markedSTUN(bad).transaction != 0 {
			t.Fatalf("unsafe marker accepted at %d", offset)
		}
	}
	for size := 0; size < 20; size++ {
		if markedSTUN(b[:size]).transaction != 0 {
			t.Fatal("truncated marker")
		}
	}
	for _, id := range []uint64{0, 1 << 53} {
		if markedSTUN(markedProbe(id)).transaction != 0 {
			t.Fatal("invalid identifier")
		}
	}
	v4 := make([]byte, 28)
	v4[0] = 0x45
	v4[9] = 17
	binary.BigEndian.PutUint16(v4[2:4], 48)
	binary.BigEndian.PutUint16(v4[24:26], 28)
	v4 = append(v4, b...)
	if packetProbe(v4).transaction != 42 {
		t.Fatal("IPv4 marker lost")
	}
	v4[6] = 0x20
	if packetProbe(v4).transaction != 0 {
		t.Fatal("fragment traced")
	}
	v6 := make([]byte, 48)
	v6[0] = 0x60
	v6[6] = 17
	binary.BigEndian.PutUint16(v6[4:6], 28)
	binary.BigEndian.PutUint16(v6[44:46], 28)
	v6 = append(v6, b...)
	if packetProbe(v6).transaction != 42 {
		t.Fatal("IPv6 marker lost")
	}
	v6[6] = 44
	if packetProbe(v6).transaction != 0 {
		t.Fatal("IPv6 fragment traced")
	}
	for size := 0; size < len(v6); size++ {
		packetProbe(v6[:size])
	}
}

func TestDiagnosticsCannotBlockForwarderAndReportsLoss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d := NewDiagnostics(ctx, func(e Observation) { once.Do(func() { close(entered) }); <-release })
	d.Observe(Observation{Stage: "udp_write", Transaction: 1})
	<-entered
	start := time.Now()
	for i := 0; i < 10000; i++ {
		d.Observe(Observation{Stage: "udp_write", Transaction: 1})
	}
	if time.Since(start) > time.Second {
		t.Fatal("blocked on diagnostics")
	}
	if d.dropped.Load() == 0 || len(d.queue) > 128 {
		t.Fatal("missing bounded-drop evidence")
	}
	cancel()
	close(release)
}

func TestMarkedSTUNTraceTraversesSOCKSAndPacketBoundary(t *testing.T) {
	var mu sync.Mutex
	var got []Observation
	observer := func(e Observation) { mu.Lock(); got = append(got, e); mu.Unlock() }
	client, server, ctx := pairedStacksObserved(t, observer)
	echo, err := server.Network.DialUDPAddrPort(netip.MustParseAddrPort("192.0.2.2:5353"), netip.AddrPort{})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 100)
		n, p, e := echo.ReadFrom(b)
		if e == nil {
			b[0] = 1
			echo.WriteTo(b[:n], p)
		}
	}()
	_, u := udpAssociation(t, ctx, &SOCKSServer{Dialer: client, Token: "fixture-token", Observe: observer})
	a, _ := encodedAddress("192.0.2.2:5353")
	u.SetDeadline(time.Now().Add(time.Second))
	u.Write(append(append([]byte{0, 0, 0}, a...), markedProbe(42)...))
	b := make([]byte, 100)
	n, err := u.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if markedSTUN(b[10:n]).transaction != 42 {
		t.Fatal("probe was modified")
	}
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		seen := map[string]bool{}
		for _, e := range got {
			if e.Transaction == 42 && e.Outcome == "ok" {
				seen[e.Stage] = true
			}
		}
		mu.Unlock()
		if len(seen) == 9 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing stages: %v", seen)
		}
		time.Sleep(time.Millisecond)
	}
}
