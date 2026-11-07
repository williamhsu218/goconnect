package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveCompanyOpenConnectProxyAuthentication(t *testing.T) {
	runtime := os.Getenv("GOCONNECT_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("set packaged runtime for actual OpenConnect proxy authentication")
	}
	var authenticated atomic.Bool
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 65536))
		w.Header().Set("Content-Type", "text/xml")
		if bytes.Contains(body, []byte("<username>fixture-user</username>")) && bytes.Contains(body, []byte("<password>synthetic-secret-never-log</password>")) {
			authenticated.Store(true)
			fmt.Fprint(w, `<?xml version="1.0"?><config-auth client="vpn" type="complete"><auth id="success"/><session-token>fixture-cookie</session-token></config-auth>`)
		} else {
			fmt.Fprint(w, `<?xml version="1.0"?><config-auth client="vpn" type="auth-request"><auth id="main"><form method="post" action="/auth"><input type="text" name="username" label="Username:"/><input type="password" name="password" label="Password:"/></form></auth></config-auth>`)
		}
	}))
	defer fixture.Close()
	target, _ := url.Parse(fixture.URL)
	rawPort, _ := strconv.Atoi(target.Port())
	token := strings.Repeat("a", 64)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	outer, err := newCompanyOuterProxyWithDial(ctx, target.Hostname(), uint16(rawPort), []string{"198.51.100.4"}, token, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != net.JoinHostPort("198.51.100.4", target.Port()) {
			return nil, fmt.Errorf("unapproved outer address")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", target.Host)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	ca := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, filepath.Join(runtime, "bin/openconnect"), "--authenticate", "--protocol=anyconnect", "--non-inter", "--passwd-on-stdin", "--no-dtls", "--proxy-auth=basic", "--proxy", companyProxyURL(outer.Port, token), "--user", "fixture-user", "--cafile", ca, "--", fixture.URL)
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	command.Stdin = strings.NewReader("synthetic-secret-never-log\n")
	// Output contains a synthetic session cookie. Do not include it in test logs.
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		clean := string(output)
		for _, secret := range []string{token, "synthetic-secret-never-log", "fixture-cookie"} {
			clean = strings.ReplaceAll(clean, secret, "[redacted]")
		}
		if len(clean) > 1000 {
			clean = clean[len(clean)-1000:]
		}
		t.Fatalf("actual OpenConnect proxy authentication failed: %v: %s", runErr, clean)
	}
	if !authenticated.Load() {
		t.Fatal("actual OpenConnect did not reach authenticated fixture through restricted proxy")
	}
}
