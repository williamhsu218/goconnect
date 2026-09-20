package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const liveIPv6Address = "fdfe:dcba:9876::1"

const liveRulesFile = "app-rules.yaml"
const liveRulesProvider = "goconnect-apps"

var liveConnectionID = regexp.MustCompile(`^[A-Za-z0-9-]{1,80}$`)

func makeLiveConfig(r routeRequest, device string, port uint16) map[string]any {
	legacy := r
	legacy.LiveRouting = false
	config := makeConfig(legacy, "", device, port)
	target, fallback := "GoConnect", "DIRECT"
	if r.mode() == modeGlobal {
		target, fallback = "DIRECT", "GoConnect"
	}
	rules := directDomainRules(r.DirectDomains)
	rules = append(rules, "RULE-SET,"+liveRulesProvider+","+target)
	if r.mode() == modeGlobal {
		// A missed process lookup must not disable global VPN traffic. Keep
		// the 0.7.x fallback; only positively identified exclusions go DIRECT.
		rules = append(rules, "MATCH,GoConnect")
	} else {
		rules = append(rules, "PROCESS-PATH-REGEX,^.+$,"+fallback, "MATCH,REJECT")
	}
	config["rules"] = rules
	config["rule-providers"] = map[string]any{liveRulesProvider: map[string]any{
		"type": "file", "behavior": "classical", "format": "yaml", "path": "./" + liveRulesFile,
	}}
	// The local ingress is dual-stack independently of the remote VPN.
	// Forward a TLS/HTTP/QUIC hostname to the selected egress so an IPv4-only
	// VPN can resolve and carry requests originally addressed to IPv6.
	// This only reads public handshake metadata; TLS remains end-to-end.
	config["ipv6"] = true
	config["sniffer"] = map[string]any{
		"enable": true, "parse-pure-ip": true, "force-dns-mapping": true,
		"override-destination": true,
		"sniff": map[string]any{
			"HTTP": map[string]any{"ports": []string{"1-65535"}},
			"TLS":  map[string]any{"ports": []string{"1-65535"}},
			"QUIC": map[string]any{"ports": []int{443, 8443}},
		},
	}
	tun := config["tun"].(map[string]any)
	tun["inet4-address"] = []string{captureAddress + "/30"}
	tun["inet6-address"] = []string{liveIPv6Address + "/126"}
	addSystemListener(config, r)
	return config
}

func liveRulePayload(paths []string) []string {
	payload := make([]string, 0, len(paths))
	for _, path := range paths {
		payload = append(payload, "PROCESS-PATH-REGEX,"+processPathPattern(path))
	}
	// Classical providers require a nonempty file. This expression never matches,
	// including metadata with no process path: use the mode-specific fallback.
	if len(payload) == 0 {
		payload = append(payload, "PROCESS-PATH-REGEX,a^")
	}
	return payload
}

func writeLiveRules(dir string, paths []string) error {
	data, err := yaml.Marshal(map[string]any{"payload": liveRulePayload(paths)})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".app-rules-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, liveRulesFile))
}

func prepareLiveApps(paths []string, probe bool) ([]capturedApp, error) {
	apps := make([]capturedApp, 0, len(paths))
	for _, path := range paths {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return nil, errors.New("应用已移动，请重新选择。")
		}
		st, err := os.Stat(path)
		if err != nil {
			return nil, errors.New("分流目标已移除或移动，请重新选择。")
		}
		if appBundleRoutingPath(path) {
			if !st.IsDir() {
				return nil, errors.New("应用目录无效，请重新选择。")
			}
		} else if !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
			return nil, errors.New("服务文件无效、不可执行或已被替换，请重新选择。")
		}
		app := capturedApp{Path: path}
		if probe {
			app.Binary = filepath.Join(path, "Contents/MacOS/Probe")
		}
		apps = append(apps, app)
	}
	return apps, nil
}

func liveRoutingRules(uid uint32, device string, r routeRequest, ts tailscaleBypass) string {
	// Reuse the gateway and Tailscale policy, but both modes capture this UID;
	// DIRECT versus VPN is decided in the core using the originating process.
	global := r
	global.LiveRouting = false
	global.RoutingMode = modeGlobal
	rules := routingRules(uid, nil, device, global, ts)
	if !r.ProbeOnly {
		// Retain the terminal IPv6 reject as a guard if route-to does not apply.
		block := fmt.Sprintf("block return out quick on ! lo0 inet6 proto { tcp udp } from any to any user %d\n", uid)
		capture := fmt.Sprintf("pass out quick on ! lo0 route-to (%s %s) inet6 proto { tcp udp } from any to any user %d flags any no state\n", device, liveIPv6Address, uid)
		rules = strings.Replace(rules, block, capture+block, 1)
		// DNS sent by an app (e.g. Chromium's built-in resolver) uses the
		// same system resolvers as mDNSResponder, without entering the VPN.
		var dns strings.Builder
		dns.WriteString(localDiscoveryRules(uid, r.systemLAN))
		for _, rule := range r.DirectDomains {
			if prefix, ok := directIPPrefix(rule.Domain); ok {
				family := "inet6"
				if prefix.Addr().Is4() {
					family = "inet"
				}
				fmt.Fprintf(&dns, "pass out quick on ! lo0 %s proto { tcp udp } from any to %s user %d flags any no state\n", family, prefix, uid)
			}
		}
		for _, route := range r.systemLAN {
			family := "inet6"
			if route.Prefix.Addr().Is4() {
				family = "inet"
			}
			fmt.Fprintf(&dns, "pass out quick on %s %s proto { tcp udp } from any to %s user %d flags any no state\n", route.Interface, family, route.Prefix, uid)
		}
		for _, resolver := range r.systemDNS {
			family := "inet6"
			if resolver.Is4() {
				family = "inet"
			}
			fmt.Fprintf(&dns, "pass out quick on ! lo0 %s proto { tcp udp } from any to %s port 53 user %d flags any no state\n", family, resolver, uid)
		}
		// Keep Tailscale's interface/deny rules before these narrow DNS rules.
		at := strings.Index(rules, "pass out quick on ! lo0 route-to")
		if at >= 0 {
			rules = rules[:at] + dns.String() + rules[at:]
		}
	}
	return systemRoutingRules(r, ts) + rules
}

func changedAppPaths(before, after []string) []string {
	old, next := map[string]bool{}, map[string]bool{}
	for _, p := range before {
		old[p] = true
	}
	for _, p := range after {
		next[p] = true
	}
	result := []string{}
	for _, p := range before {
		if !next[p] {
			result = append(result, p)
		}
	}
	for _, p := range after {
		if !old[p] {
			result = append(result, p)
		}
	}
	return result
}

func coreMutation(ctx context.Context, client *http.Client, port uint16, token, method, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, endpoint), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return errors.New("分流核心未确认规则更新。")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("分流核心拒绝更新（HTTP %d）。", response.StatusCode)
	}
	return nil
}

func (c *controlFiles) handleLiveCommand(ctx context.Context, client *http.Client, port uint16, dir string) error {
	data, _, err := readOwned(c.directory, "command.json", c.uid)
	if err != nil {
		return nil
	}
	var command routeCommand
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF || command.Sequence <= c.lastCommand {
		return nil
	}
	c.lastCommand = command.Sequence
	result := &routeCommandResult{Sequence: command.Sequence}
	c.snapshot.CommandResult = result
	if command.Action != "updateApps" || command.Sequence == 0 || command.AppPaths == nil || command.AppIndex != 0 || command.Network != "" || command.Helper || command.LocalPort != 0 || command.Target != "" {
		result.Message = "无效的实时名单更新。"
		return nil
	}
	candidate := c.request
	candidate.AppPaths = command.AppPaths
	if candidate.validate() != nil {
		result.Message = "应用名单格式无效。"
		return nil
	}
	apps, err := prepareLiveApps(candidate.AppPaths, false)
	if err != nil {
		result.Message = err.Error()
		return nil
	}
	changed := changedAppPaths(c.request.AppPaths, candidate.AppPaths)
	if len(changed) == 0 {
		result.Success = true
		result.Message = "名单未变化。"
		return nil
	}
	previous := append([]string{}, c.request.AppPaths...)
	api := &http.Client{Timeout: 3 * time.Second, Transport: client.Transport}
	reload := func(paths []string) error {
		if err := coreMutation(ctx, api, port, c.request.Token, http.MethodPut, "/providers/rules/"+liveRulesProvider); err != nil {
			return err
		}
		if !liveRulesReady(api, port, c.request.Token, len(liveRulePayload(paths))) {
			return errors.New("核心未确认名单加载。")
		}
		return nil
	}
	if err = writeLiveRules(dir, candidate.AppPaths); err == nil {
		err = reload(candidate.AppPaths)
	}
	if err != nil {
		result.Message = "名单更新失败，已保留原规则。"
		if writeLiveRules(dir, previous) != nil || reload(previous) != nil {
			return errors.New("名单更新及恢复失败，分流会话已停止，请重新连接。")
		}
		return nil
	}
	c.request.AppPaths = candidate.AppPaths
	c.apps = apps
	result.Success = true
	// New connections now use the new policy. Close only affected core flows so
	// applications can reconnect without restarting themselves or the VPN.
	connections, err := coreSnapshot(api, port, c.request.Token)
	incomplete := err != nil
	if err == nil {
		flowCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		for _, connection := range connections.Connections {
			if flowCtx.Err() != nil {
				incomplete = true
				break
			}
			affected := false
			for _, path := range changed {
				if configuredPathMatches(path, connection.Metadata.ProcessPath) {
					affected = true
					break
				}
			}
			if !affected {
				continue
			}
			if !liveConnectionID.MatchString(connection.ID) {
				incomplete = true
				continue
			}
			if coreMutation(flowCtx, api, port, c.request.Token, http.MethodDelete, "/connections/"+connection.ID) != nil {
				incomplete = true
			}
		}
	}
	result.Message = "名单已实时生效；受影响 App 的旧网络连接已释放，请求将按新规则重新建立。"
	if incomplete {
		result.Message = "名单已生效，新请求使用新规则；部分旧连接需由 App 刷新后重新建立。"
	}
	return nil
}

func liveRulesReady(client *http.Client, port uint16, token string, expected int) bool {
	request, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/providers/rules", port), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var result struct {
		Providers map[string]struct {
			Count int `json:"ruleCount"`
		} `json:"providers"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil {
		return false
	}
	provider, ok := result.Providers[liveRulesProvider]
	return ok && provider.Count == expected
}
