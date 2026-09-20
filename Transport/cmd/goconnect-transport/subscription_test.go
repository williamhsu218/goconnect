package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionReducesUntrustedConfig(t *testing.T) {
	nodes, err := parseSubscription([]byte(`tun: {enable: true}
external-controller: 0.0.0.0:9090
script: {code: malicious}
proxies:
  - name: Test
    type: vmess
    server: example.com
    port: 443
    uuid: test
    network: ws
    tls: true
    dialer-proxy: other
    ws-opts: {path: /ws, headers: {Host: example.com}}
    certificate: /private/file
`))
	if err != nil || len(nodes) != 1 {
		t.Fatal("valid fixture rejected", err)
	}
	b, _ := json.Marshal(nodes[0].Proxy)
	s := string(b)
	for _, word := range []string{"dialer-proxy", "plugin", "certificate", "malicious", "external-controller"} {
		if strings.Contains(s, word) {
			t.Fatal("unsafe field retained", word)
		}
	}
	if !strings.Contains(s, "ws-opts") {
		t.Fatal("WS transport lost")
	}
}
func TestSubscriptionRejectsAmbiguousNamesAndUnsupported(t *testing.T) {
	for _, s := range []string{"proxies: []", "proxies: [{name: A, type: unknown, server: x}]", "proxies: [{name: A, type: vmess, server: x}, {name: A, type: vmess, server: y}]", "proxies: [{name: A, type: socks5, server: '/tmp/socket'}]"} {
		if _, err := parseSubscription([]byte(s)); err == nil {
			t.Fatal("accepted invalid subscription")
		}
	}
}
func TestDomainRuleValidationAndPrecedence(t *testing.T) {
	for _, domain := range []string{"a.com,DIRECT", "https://a.com", "-a.com", "a..com", "1.1.1.1", "a.com\nMATCH", "A.com"} {
		if validDirectDomain(domain) {
			t.Fatal("invalid domain accepted", domain)
		}
	}
	for _, domain := range []string{"example.com", "xn--fiqs8s.cn", "www.example.com"} {
		if !validDirectDomain(domain) {
			t.Fatal("valid domain rejected", domain)
		}
	}
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		r := routeRequest{RoutingMode: mode, AppPaths: []string{"/Applications/Test.app"}, DirectDomains: []domainRule{{"example.com", true}, {"only.test", false}}}
		c := makeConfig(r, "", "utun99", 12345)
		rules := c["rules"].([]string)
		if len(rules) != 4 || rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || rules[1] != "DOMAIN,only.test,DIRECT" {
			t.Fatal(mode, rules)
		}
		if mode == modeWhitelist && (!strings.HasSuffix(rules[2], ",GoConnect") || rules[3] != "MATCH,REJECT") {
			t.Fatal("whitelist scope lost", rules)
		}
		if mode == modeGlobal && (!strings.HasSuffix(rules[2], ",REJECT") || rules[3] != "MATCH,GoConnect") {
			t.Fatal("global exclusion lost", rules)
		}
		if c["sniffer"].(map[string]any)["enable"] != true {
			t.Fatal("direct domains require sniffer")
		}
		r.DirectDomains = nil
		if makeConfig(r, "", "utun99", 12345)["sniffer"].(map[string]any)["enable"] != false {
			t.Fatal("empty domain list should not enable sniffer")
		}
	}
}
