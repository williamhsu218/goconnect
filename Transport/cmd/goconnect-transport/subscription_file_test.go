package main

import (
	"context"
	"strings"
	"testing"
)

func TestSubscriptionFileRetainsSafeNodesAndRejectsUnencryptedMetadata(t *testing.T) {
	fixture := "proxies: [{name: local, type: hysteria2, server: 192.0.2.1, port: 45392, password: fixture-only}]\n"
	p, err := localSubscriptionPayload(context.Background(), fixture, "local-file")
	if err != nil || p.Name != "local-file" || !strings.HasPrefix(p.URL, "local://file/") || len(p.Nodes) != 1 {
		t.Fatal("local file import failed", err)
	}
	for _, u := range []string{"http://example.com/sub", "https://user:password@example.com/sub", "file:///etc/passwd"} {
		_, err = localSubscriptionPayload(context.Background(), fixture+"x-goconnect: {subscription-url: '"+u+"'}\n", "unsafe")
		if err == nil {
			t.Fatal("unsafe subscription URL accepted")
		}
	}
}
