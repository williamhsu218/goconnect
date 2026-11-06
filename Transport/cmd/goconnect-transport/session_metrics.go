package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// Read totals from the node core, which only carries this session's proxied
// traffic. The separate system-routing core also sees DIRECT traffic.
func readNodeTotals(ctx context.Context, client *http.Client, endpoint, token string) (uint64, uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/connections", nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, errors.New("statistics unavailable")
	}
	var totals struct {
		Download *uint64 `json:"downloadTotal"`
		Upload   *uint64 `json:"uploadTotal"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&totals); err != nil {
		return 0, 0, err
	}
	if totals.Download == nil || totals.Upload == nil {
		return 0, 0, errors.New("missing statistics")
	}
	return *totals.Download, *totals.Upload, nil
}

func monitorNodeTotals(parent context.Context, port uint16, token string, publish func(any)) func() {
	ctx, cancel := context.WithCancel(parent)
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	var worker sync.WaitGroup
	worker.Add(1)
	go func() {
		defer worker.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			rx, tx, err := readNodeTotals(ctx, client, fmt.Sprintf("http://127.0.0.1:%d", port), token)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				publish(map[string]any{"event": "stats_unavailable"})
			} else {
				publish(map[string]any{"event": "stats", "received": rx, "sent": tx})
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return func() { cancel(); worker.Wait(); transport.CloseIdleConnections() }
}

func publicIPValue(raw string) (string, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || ip.Zone() != "" {
		return "", errors.New("invalid public address")
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", errors.New("non-public address")
	}
	for _, reserved := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(reserved).Contains(ip) {
			return "", errors.New("reserved address")
		}
	}
	return ip.String(), nil
}

func queryPublicIP(ctx context.Context, client *http.Client, endpoints []string) (string, error) {
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 129))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil || len(data) > 128 {
			continue
		}
		if ip, err := publicIPValue(string(data)); err == nil {
			return ip, nil
		}
	}
	return "", errors.New("public address unavailable")
}

// Explicit SOCKS authentication ensures the result belongs to the selected
// line, even in whitelist mode or when the detector's domain is a DIRECT rule.
func publicIPCommand(parent context.Context) error {
	var r struct {
		Port  uint16 `json:"port"`
		Token string `json:"token"`
	}
	if json.NewDecoder(io.LimitReader(os.Stdin, 4096)).Decode(&r) != nil || r.Port == 0 || len(r.Token) < 32 || len(r.Token) > 200 {
		return errors.New("invalid exit address request")
	}
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(r.Port))), &proxy.Auth{User: "goconnect", Password: r.Token}, &net.Dialer{Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.(proxy.ContextDialer).DialContext, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(parent, 13*time.Second)
	defer cancel()
	ip, err := queryPublicIP(ctx, client, []string{"https://api64.ipify.org", "https://api.ipify.org"})
	if ctx.Err() != nil || err != nil {
		emit(map[string]any{"event": "public_ip"})
		return nil
	}
	emit(map[string]any{"event": "public_ip", "publicIP": ip})
	return nil
}
