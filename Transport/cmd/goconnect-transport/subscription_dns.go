package main

// HY2 carries ordinary UDP in unreliable QUIC datagrams. Resolve public DNS
// locally in the node core, then send DoH through Selected's reliable stream.
// This does not change macOS resolvers, the outer routing policy or other UDP.
func configureSubscriptionDNS(config map[string]any, node map[string]any) {
	if node["type"] != "hysteria2" {
		return
	}
	const resolver = "GoConnectDNS"
	config["dns"] = map[string]any{
		"enable": true, "listen": "", "ipv6": true, "enhanced-mode": "normal",
		"prefer-h3": false, "use-hosts": true, "use-system-hosts": true,
		"nameserver": []string{
			"https://1.1.1.1/dns-query#Selected",
			"https://8.8.8.8/dns-query#Selected",
		},
		"default-nameserver":      []string{"system"},
		"proxy-server-nameserver": []string{"system"},
		"nameserver-policy": map[string]any{
			"+.local": "system", "+.lan": "system", "+.home.arpa": "system",
			"+.internal": "system", "+.in-addr.arpa": "system", "+.ip6.arpa": "system",
		},
	}
	config["proxies"] = []map[string]any{node, {"name": resolver, "type": "dns"}}
	// Explicit address exclusions need no GeoIP download and preserve private,
	// loopback, link-local and Tailscale/CGNAT resolver destinations.
	rules := []string{}
	for _, cidr := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
	} {
		rules = append(rules, "IP-CIDR,"+cidr+",Selected,no-resolve")
	}
	for _, cidr := range []string{"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"} {
		rules = append(rules, "IP-CIDR6,"+cidr+",Selected,no-resolve")
	}
	rules = append(rules, "AND,((NETWORK,udp),(DST-PORT,53)),"+resolver, "MATCH,Selected")
	config["rules"] = rules
}
