package integration

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// A loopback-only fixture. Production's physical binding and target restrictions
// are independently exercised in the transport package's outer proxy tests.
func companyFixtureProxy(t *testing.T, server, token string) uint16 {
	t.Helper()
	target, _ := url.Parse(server)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != target.Host {
			http.Error(w, "restricted", 403)
			return
		}
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("goconnect:"+token))
		if r.Header.Get("Proxy-Authorization") != expected {
			w.Header().Set("Proxy-Authenticate", `Basic realm="Fixture"`)
			w.WriteHeader(407)
			return
		}
		remote, err := net.Dial("tcp", target.Host)
		if err != nil {
			http.Error(w, "unavailable", 502)
			return
		}
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			remote.Close()
			return
		}
		defer conn.Close()
		defer remote.Close()
		fmt.Fprint(buffer, "HTTP/1.1 200 Connection Established\r\n\r\n")
		buffer.Flush()
		done := make(chan struct{})
		go func() {
			io.Copy(remote, buffer)
			if c, ok := remote.(interface{ CloseWrite() error }); ok {
				c.CloseWrite()
			}
			close(done)
		}()
		io.Copy(conn, remote)
		conn.Close()
		<-done
	}))
	t.Cleanup(proxy.Close)
	endpoint, _ := url.Parse(proxy.URL)
	port, _ := strconv.ParseUint(endpoint.Port(), 10, 16)
	return uint16(port)
}
