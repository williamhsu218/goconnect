package integration

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"goconnect.local/transport/internal/bridge"
)

// This uses the packaged OpenConnect, a real UNIX datagram pipe and the
// production bridge, with an isolated TLS/CSTP gateway. No system TUN or route.
func TestBundledBridgeDiagnostics(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("requires packaged runtime")
	}
	seconds := 2
	if text := os.Getenv("GOCONNECT_TEST_BRIDGE_SECONDS"); text != "" {
		value, err := strconv.Atoi(text)
		if err != nil || value < 1 || value > 1800 {
			t.Fatal("invalid diagnostic test duration")
		}
		seconds = value
	}
	f := newRecoveryFixture(t)
	h := startHelper(t, f.server.URL, f.ca)
	ready := waitRecoveryEvent(t, h, "ready", 10*time.Second)
	port := int(ready["port"].(float64))
	var observations []bridge.Observation
	var failures []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range h.events {
			if e["event"] == "bridge_diagnostic" {
				data, _ := json.Marshal(e["bridge"])
				var o bridge.Observation
				if json.Unmarshal(data, &o) == nil {
					observations = append(observations, o)
				}
			}
			if e["event"] == "error" {
				failures = append(failures, "transport_error")
			}
		}
	}()
	c, err := recoveryDialer(t, port).Dial("tcp", "192.0.2.2:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	u := recoveryUDP(t, port)
	body := bytes.Repeat([]byte("isolated-TCP-integrity\n"), 24000)
	got := make([]byte, len(body))
	start := time.Now()
	var count uint64
	maxUDP := time.Duration(0)
	for time.Since(start) < time.Duration(seconds)*time.Second {
		tick := time.Now()
		count++
		c.SetDeadline(time.Now().Add(20 * time.Second))
		if _, err = c.Write(body); err != nil {
			t.Fatal(err)
		}
		if _, err = io.ReadFull(c, got); err != nil || !bytes.Equal(got, body) {
			t.Fatal("TCP mismatch", err)
		}
		packet := []byte{0, 0, 0, 1, 192, 0, 2, 2, 20, 233, 0, 1, 0, 0, 0x21, 0x12, 0xa4, 0x42, 'G', 'C', 'D', '1', 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(packet[22:], count)
		u.SetDeadline(time.Now().Add(15 * time.Second))
		sent := time.Now()
		if _, err = u.Write(packet); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, 100)
		n, err := u.Read(reply)
		if err != nil || !bytes.Equal(packet, reply[:n]) {
			t.Fatal("UDP mismatch", err)
		}
		if elapsed := time.Since(sent); elapsed > maxUDP {
			maxUDP = elapsed
		}
		if wait := time.Second - time.Since(tick); wait > 0 {
			time.Sleep(wait)
		}
	}
	// Let the asynchronous diagnostic queue drain before terminating the helper.
	time.Sleep(200 * time.Millisecond)
	h.input.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("helper output did not finish")
	}
	seen := map[uint64]map[string]bool{}
	for _, o := range observations {
		if o.Dropped != 0 {
			t.Fatalf("lost diagnostics: %d", o.Dropped)
		}
		if o.Transaction == 0 {
			continue
		}
		if o.Outcome != "ok" {
			t.Fatalf("bridge failure: %s %s errno=%d", o.Stage, o.Outcome, o.Errno)
		}
		if seen[o.Transaction] == nil {
			seen[o.Transaction] = map[string]bool{}
		}
		seen[o.Transaction][o.Stage] = true
	}
	for id := uint64(1); id <= count; id++ {
		if len(seen[id]) != 9 {
			t.Fatalf("trace %d incomplete: %v", id, seen[id])
		}
	}
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	if output := os.Getenv("GOCONNECT_TEST_BRIDGE_EVIDENCE"); output != "" {
		data, err := json.MarshalIndent(map[string]any{"seconds": time.Since(start).Seconds(), "transactions": count, "tcpBytes": uint64(len(body)) * count, "maxUDPSeconds": maxUDP.Seconds(), "observations": observations}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.Write(data)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Log(fmt.Sprintf("%.3fs: %d TCP and UDP round trips; all nine bridge stages; max UDP %.6fs", time.Since(start).Seconds(), count, maxUDP.Seconds()))
}
