package bridge

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestIPv4OnlyStackRejectsLiteralIPv6Promptly(t *testing.T) {
	stack, err := New([]netip.Addr{netip.MustParseAddr("192.0.2.2")}, nil, 1400)
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	c, err := stack.DialContext(ctx, "tcp", "[2001:db8::1]:443")
	if c != nil {
		c.Close()
	}
	if err == nil {
		t.Fatal("IPv4-only stack accepted IPv6")
	}
	if time.Since(started) > time.Second {
		t.Fatal("unsupported family did not fail promptly", err)
	}
	t.Log("literal IPv6 rejected without host fallback", time.Since(started))
}
