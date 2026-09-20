package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// Subscription data never enters the privileged service. Only a selected, reduced
// proxy definition is consumed by a normal-user core with no TUN or controller.
type subscriptionNode struct {
	Name  string         `json:"name"`
	Type  string         `json:"type"`
	Proxy map[string]any `json:"proxy"`
}
type subscriptionData struct {
	Name             string             `json:"name"`
	URL              string             `json:"url"`
	Nodes            []subscriptionNode `json:"nodes"`
	Updated          int64              `json:"updated"`
	UserInfo         string             `json:"userInfo"`
	UnsupportedCount int                `json:"unsupportedCount"`
}
type subscriptionOperation struct {
	URL string `json:"url"`
}

const subscriptionLimit = 8 << 20

func parseSubscription(data []byte) ([]subscriptionNode, error) {
	if len(data) > subscriptionLimit {
		return nil, errors.New("subscription too large")
	}
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Proxies) == 0 || len(doc.Proxies) > 5000 {
		return nil, errors.New("invalid Clash subscription")
	}
	nodes := []subscriptionNode{}
	names := map[string]bool{}
	for _, raw := range doc.Proxies {
		name, _ := raw["name"].(string)
		kind, _ := raw["type"].(string)
		if name == "" || names[name] {
			return nil, errors.New("duplicate or missing node name")
		}
		names[name] = true
		if !strings.Contains("|anytls|hysteria2|vmess|vless|trojan|ss|socks5|http|", "|"+kind+"|") {
			continue
		}
		if plugin, ok := raw["plugin"].(string); ok && plugin != "" {
			continue
		}
		if _, ok := raw["reality-opts"]; ok {
			continue
		}
		if network, ok := raw["network"].(string); ok && network != "" && network != "tcp" && network != "ws" && network != "grpc" {
			continue
		}
		safe := map[string]any{}
		for _, key := range strings.Fields("name type server port password uuid alterId cipher network udp tls servername sni skip-cert-verify client-fingerprint fingerprint alpn up down obfs obfs-password flow packet-encoding") {
			if v, ok := raw[key]; ok {
				safe[key] = v
			}
		}
		if ws, ok := raw["ws-opts"].(map[string]any); ok {
			out := map[string]any{}
			for _, key := range []string{"path", "headers", "max-early-data", "early-data-header-name"} {
				if v, ok := ws[key]; ok {
					out[key] = v
				}
			}
			safe["ws-opts"] = out
		}
		if _, ok := safe["ws-opts"]; !ok {
			if path, ok := raw["ws-path"].(string); ok {
				safe["ws-opts"] = map[string]any{"path": path, "headers": raw["ws-headers"]}
			}
		}
		if g, ok := raw["grpc-opts"].(map[string]any); ok {
			safe["grpc-opts"] = map[string]any{"grpc-service-name": g["grpc-service-name"]}
		}
		server, ok := safe["server"].(string)
		if !ok || server == "" || strings.ContainsAny(server, "/\x00\r\n") {
			return nil, errors.New("invalid node server")
		}
		nodes = append(nodes, subscriptionNode{name, kind, safe})
	}
	if len(nodes) == 0 {
		return nil, errors.New("no supported nodes")
	}
	return nodes, nil
}
func subscriptionUnsupported(data []byte, nodes []subscriptionNode) int {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	yaml.Unmarshal(data, &doc)
	return len(doc.Proxies) - len(nodes)
}
func subscriptionCommand(ctx context.Context, importing bool) error {
	var results []subscriptionData
	if importing {
		account, err := user.Current()
		if err != nil {
			return err
		}
		home := account.HomeDir
		base := filepath.Join(home, "Library/Application Support/io.github.clash-verge-rev.clash-verge-rev")
		data, err := os.ReadFile(filepath.Join(base, "profiles.yaml"))
		if err != nil {
			fail("未找到 Clash Verge 订阅。请先在本机配置订阅。")
			return err
		}
		var profiles struct {
			Items []struct {
				Type string `yaml:"type"`
				Name string `yaml:"name"`
				File string `yaml:"file"`
				URL  string `yaml:"url"`
			} `yaml:"items"`
		}
		if err = yaml.Unmarshal(data, &profiles); err != nil {
			return err
		}
		for _, p := range profiles.Items {
			if p.Type != "remote" {
				continue
			}
			if filepath.Base(p.File) != p.File {
				return errors.New("invalid profile file")
			}
			data, err = os.ReadFile(filepath.Join(base, "profiles", p.File))
			if err != nil {
				return err
			}
			nodes, e := parseSubscription(data)
			if e != nil {
				fail("订阅格式不受支持，原文件未修改。")
				return e
			}
			results = append(results, subscriptionData{Name: p.Name, URL: p.URL, Nodes: nodes, Updated: time.Now().Unix(), UnsupportedCount: subscriptionUnsupported(data, nodes)})
		}
	} else {
		var request subscriptionOperation
		if json.NewDecoder(io.LimitReader(os.Stdin, 65536)).Decode(&request) != nil {
			return errors.New("invalid request")
		}
		u, err := url.Parse(request.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			fail("请输入 HTTPS 订阅地址。")
			return errors.New("invalid URL")
		}
		client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 || r.URL.Scheme != "https" {
				return errors.New("unsafe redirect")
			}
			return nil
		}}
		req, _ := http.NewRequestWithContext(ctx, "GET", request.URL, nil)
		// Some providers select their modern AnyTLS/Hysteria2 template only for
		// the Clash Verge client family. Preserve that subscription compatibility.
		req.Header.Set("User-Agent", "clash-verge/v2.5.2")
		resp, err := client.Do(req)
		if err != nil {
			fail("订阅更新失败，请检查网络或订阅有效期。已保留上次内容。")
			return errors.New("fetch failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			fail("订阅服务器未返回可用内容。已保留上次内容。")
			return errors.New("HTTP status")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, subscriptionLimit+1))
		if err != nil {
			return err
		}
		nodes, err := parseSubscription(data)
		if err != nil {
			fail("需要包含 proxies 的 Clash YAML 订阅。已保留上次内容。")
			return err
		}
		results = []subscriptionData{{Name: "新订阅", URL: request.URL, Nodes: nodes, Updated: time.Now().Unix(), UserInfo: resp.Header.Get("subscription-userinfo"), UnsupportedCount: subscriptionUnsupported(data, nodes)}}
	}
	if results == nil {
		results = []subscriptionData{}
	}
	emit(results)
	return nil
}

func subscriptionSession(parent context.Context) (err error) {
	defer func() {
		if err != nil {
			fail("订阅节点未能建立连接，请更新订阅或选择其它节点。")
		}
	}()
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 65537))
	line, e := reader.ReadBytes('\n')
	if e != nil || len(line) > 65536 {
		return errors.New("invalid request")
	}
	var r struct {
		Node  subscriptionNode `json:"node"`
		Token string           `json:"token"`
	}
	if json.Unmarshal(line, &r) != nil || len(r.Token) < 32 {
		return errors.New("invalid node")
	}
	// Re-apply the whitelist even when reading our own cache.
	data, _ := yaml.Marshal(map[string]any{"proxies": []map[string]any{r.Node.Proxy}})
	nodes, e := parseSubscription(data)
	if e != nil {
		return e
	}
	if len(nodes) != 1 {
		return errors.New("unsupported node")
	}
	proxy := nodes[0].Proxy
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() { io.Copy(io.Discard, reader); cancel() }()
	host := proxy["server"].(string)
	lookupCtx, lookupCancel := context.WithTimeout(ctx, 10*time.Second)
	defer lookupCancel()
	ips, e := net.DefaultResolver.LookupIP(lookupCtx, "ip4", host)
	if e != nil || len(ips) == 0 {
		return errors.New("resolve node")
	}
	gateway := ips[0].String()
	proxy["server"] = gateway
	proxy["name"] = "Selected"
	if ws, ok := proxy["ws-opts"].(map[string]any); ok {
		headers, ok := ws["headers"].(map[string]any)
		if !ok {
			headers = map[string]any{}
		}
		if headers["Host"] == nil && headers["host"] == nil {
			headers["Host"] = host
		}
		ws["headers"] = headers
	}
	if net.ParseIP(host) == nil {
		if proxy["sni"] == nil {
			proxy["sni"] = host
		}
		if proxy["servername"] == nil {
			proxy["servername"] = host
		}
	}
	port, e := freePort()
	if e != nil {
		return e
	}
	cfg := map[string]any{"mode": "rule", "log-level": "silent", "ipv6": false, "allow-lan": false, "bind-address": "127.0.0.1", "socks-port": port, "authentication": []string{"goconnect:" + r.Token}, "dns": map[string]any{"enable": false}, "tun": map[string]any{"enable": false}, "profile": map[string]any{"store-selected": false}, "proxies": []map[string]any{proxy}, "rules": []string{"MATCH,Selected"}}
	dir, e := os.MkdirTemp("", "GoConnect-node-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	data, e = json.Marshal(cfg)
	if e != nil {
		return e
	}
	path := filepath.Join(dir, "config.json")
	if e = os.WriteFile(path, data, 0600); e != nil {
		return e
	}
	self, _ := os.Executable()
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(self), "mihomo"), "-d", dir, "-f", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		if e := stopChild(cmd, done); e != nil {
			err = e
		}
	}()
	ready := false
	for i := 0; i < 100; i++ {
		select {
		case e := <-done:
			done <- e
			if e == nil {
				e = errors.New("core ended")
			}
			return e
		case <-ctx.Done():
			return nil
		default:
		}
		c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		cancel()
		return errors.New("core not ready")
	}
	emit(map[string]any{"event": "ready", "port": port, "gateway": gateway, "addresses": []string{}})
	select {
	case <-ctx.Done():
		return nil
	case e := <-done:
		done <- e
		if e == nil {
			return errors.New("core ended")
		}
		return e
	}
}
