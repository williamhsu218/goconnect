package main

import (
	"encoding/json"
	"errors"
	"reflect"
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

const fixtureRealityPublicKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"

func TestSubscriptionRetainsSafeVlessRealityOpts(t *testing.T) {
	const encryption = "mlkem768x25519plus.native.1rtt." + fixtureRealityPublicKey
	data := []byte(`defaults: &reality
  public-key: ` + fixtureRealityPublicKey + `
  short-id: 01234567
  support-x25519mlkem768: true
  private-key: must-strip
  certificate: /private/file
proxies:
  - {name: TCP, type: vless, uuid: 11111111-1111-4111-8111-111111111111, server: 192.0.2.1, port: 443, tls: true, encryption: ` + encryption + `, reality-opts: *reality}
  - {name: RPC, type: vless, uuid: 11111111-1111-4111-8111-111111111111, server: 192.0.2.1, port: 443, tls: true, network: grpc, grpc-opts: {grpc-service-name: qa}, reality-opts: {<<: *reality, short-id: "0123ABCD", support-x25519mlkem768: false}}
  - {name: Trojan, type: trojan, server: 192.0.2.1, port: 443, password: test, reality-opts: {public-key: ` + fixtureRealityPublicKey + `}}
`)
	nodes, err := parseSubscription(data)
	if err != nil || len(nodes) != 3 {
		t.Fatalf("expected three supported REALITY nodes, got %d: %v", len(nodes), err)
	}
	for i, want := range []map[string]any{
		{"public-key": fixtureRealityPublicKey, "short-id": "01234567", "support-x25519mlkem768": true},
		{"public-key": fixtureRealityPublicKey, "short-id": "0123ABCD", "support-x25519mlkem768": false},
		{"public-key": fixtureRealityPublicKey},
	} {
		if got := nodes[i].Proxy["reality-opts"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s REALITY options = %#v, want %#v", nodes[i].Name, got, want)
		}
	}
	if nodes[0].Proxy["encryption"] != encryption {
		t.Error("VLESS encryption lost")
	}
}

func TestSubscriptionSkipsInvalidRealityWithoutDowngrade(t *testing.T) {
	const valid = "{name: Reality, type: vless, server: 192.0.2.1, port: 443, network: tcp, tls: true, reality-opts: {public-key: " + fixtureRealityPublicKey + ", short-id: 'ab'}}"
	const plain = "{name: Plain, type: socks5, server: 192.0.2.1, port: 1080}"
	for _, tc := range []struct{ name, from, to string }{
		{"short key", fixtureRealityPublicKey, "abc"},
		{"invalid key alphabet", fixtureRealityPublicKey, strings.Repeat("+", 43)},
		{"padded key", fixtureRealityPublicKey, fixtureRealityPublicKey + "="},
		{"missing key", "public-key:", "unknown-key:"},
		{"odd short id", "'ab'", "'abc'"},
		{"nonhex short id", "'ab'", "'ag'"},
		{"oversize short id", "'ab'", "'001122334455667788'"},
		{"malformed hybrid option", "short-id: 'ab'", "support-x25519mlkem768: invalid"},
		{"null reality", "{public-key: " + fixtureRealityPublicKey + ", short-id: 'ab'}", "null"},
		{"nonmapping reality", "{public-key: " + fixtureRealityPublicKey + ", short-id: 'ab'}", "bad"},
		{"missing TLS", "tls: true, ", ""},
		{"disabled TLS", "tls: true", "tls: false"},
		{"WebSocket", "network: tcp", "network: ws"},
		{"malformed network", "network: tcp", "network: []"},
		{"wrong protocol", "type: vless", "type: ss"},
		{"malformed encryption", "type: vless", "type: vless, encryption: []"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := strings.Replace(valid, tc.from, tc.to, 1)
			data := []byte("proxies: [" + bad + ", " + plain + "]")
			nodes, err := parseSubscription(data)
			if err != nil || len(nodes) != 1 || nodes[0].Name != "Plain" || subscriptionUnsupported(data, nodes) != 1 {
				t.Fatalf("expected only the plain node to survive: %#v, %v", nodes, err)
			}
			if _, err := parseSubscription([]byte("proxies: [" + bad + "]")); !errors.Is(err, errNoSupportedNodes) {
				t.Fatalf("expected unsupported-node error, got %v", err)
			}
		})
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
