package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func tcpDrainPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	caller, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	relay, err := listener.AcceptTCP()
	if err != nil {
		caller.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { caller.Close(); relay.Close() })
	caller.SetDeadline(time.Now().Add(4 * time.Second))
	relay.SetDeadline(time.Now().Add(4 * time.Second))
	return caller, relay
}

func tcpDrainFixture(t *testing.T, idle time.Duration) (*net.TCPConn, *net.TCPConn, <-chan struct{}) {
	t.Helper()
	client, left := tcpDrainPair(t)
	server, right := tcpDrainPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		relayTCPWithIdleDrain(ctx, left, right, idle)
		close(done)
	}()
	return client, server, done
}

func tcpDrainWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TCP idle drain did not terminate")
	}
}

func TestTCPDrainDelayedLargeResponse(t *testing.T) {
	client, server, done := tcpDrainFixture(t, 200*time.Millisecond)
	payload := bytes.Repeat([]byte("delayed response\n"), 25000)
	serverDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(server)
		if err == nil {
			time.Sleep(20 * time.Millisecond)
			_, err = server.Write(payload)
		}
		server.CloseWrite()
		serverDone <- err
	}()
	io.WriteString(client, "request")
	client.CloseWrite()
	got, err := io.ReadAll(client)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("response truncated: bytes=%d want=%d err=%v", len(got), len(payload), err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	tcpDrainWait(t, done)
}

func TestTCPDrainActiveResponseResetsIdle(t *testing.T) {
	const idle = 200 * time.Millisecond
	client, server, done := tcpDrainFixture(t, idle)
	serverDone := make(chan error, 1)
	go func() {
		if _, err := io.ReadAll(server); err != nil {
			serverDone <- err
			return
		}
		for i := 0; i < 12; i++ {
			if _, err := fmt.Fprintf(server, "chunk-%02d\n", i); err != nil {
				serverDone <- err
				return
			}
			time.Sleep(idle / 4)
		}
		serverDone <- server.CloseWrite()
	}()
	started := time.Now()
	io.WriteString(client, "request")
	client.CloseWrite()
	got, err := io.ReadAll(client)
	var want bytes.Buffer
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&want, "chunk-%02d\n", i)
	}
	if err != nil || !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("active response was cut off: bytes=%d want=%d err=%v", len(got), want.Len(), err)
	}
	if elapsed := time.Since(started); elapsed < 2*idle {
		t.Fatalf("fixture did not span two idle periods: %v", elapsed)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	tcpDrainWait(t, done)
}

func TestTCPDrainExpiresOnlyAfterEitherEOF(t *testing.T) {
	for _, clientFirst := range []bool{true, false} {
		t.Run(fmt.Sprint("clientFirst=", clientFirst), func(t *testing.T) {
			const idle = 80 * time.Millisecond
			client, server, done := tcpDrainFixture(t, idle)
			// Fully open keep-alive streams have no new idle deadline.
			select {
			case <-done:
				t.Fatal("open stream expired before either EOF")
			case <-time.After(2 * idle):
			}
			if clientFirst {
				client.CloseWrite()
				io.ReadAll(server)
			} else {
				server.CloseWrite()
				io.ReadAll(client)
			}
			tcpDrainWait(t, done)
		})
	}
}

func TestTCPDrainRSTClosesBothEnds(t *testing.T) {
	client, server, done := tcpDrainFixture(t, 10*time.Second)
	io.WriteString(client, "request")
	client.SetLinger(0)
	client.Close()
	tcpDrainWait(t, done)
	if _, err := io.ReadAll(server); err != nil {
		if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			t.Fatal("server remained connected after RST")
		}
	}
}
