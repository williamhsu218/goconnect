package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

type nativeResolver struct {
	domain, iface string
	servers       []netip.Addr
	port, order   int
	scoped        bool
}

type managedDNSPlan struct {
	port      uint16
	resolvers []nativeResolver
}

// Preserve resolver domains, ports and IPv6 zones. Flattening scoped/private
// resolvers into the default list would send public names to corporate DNS.
func parseNativeResolvers(data string) ([]nativeResolver, error) {
	var result []nativeResolver
	var current *nativeResolver
	scoped := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DNS configuration") {
			scoped = strings.Contains(line, "scoped") || strings.Contains(line, "service-specific")
			current = nil
		} else if strings.HasPrefix(line, "resolver #") {
			result = append(result, nativeResolver{port: 53, order: 200000, scoped: scoped})
			current = &result[len(result)-1]
		} else if current != nil {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			switch {
			case strings.HasPrefix(key, "nameserver["):
				a, err := netip.ParseAddr(value)
				if err != nil || a.IsUnspecified() || a.IsMulticast() {
					return nil, errors.New("invalid DNS server")
				}
				current.servers = append(current.servers, a.Unmap())
			case key == "domain":
				current.domain = strings.TrimSuffix(strings.ToLower(value), ".")
			case key == "port":
				port, err := strconv.Atoi(value)
				if err != nil || port < 1 || port > 65535 {
					return nil, errors.New("invalid DNS port")
				}
				current.port = port
			case key == "order":
				order, err := strconv.Atoi(value)
				if err != nil || order < 0 {
					return nil, errors.New("invalid DNS order")
				}
				current.order = order
			case key == "if_index":
				start, end := strings.Index(value, "("), strings.Index(value, ")")
				if start >= 0 && end > start {
					current.iface = value[start+1 : end]
				}
			}
		}
	}
	if len(result) > 128 {
		return nil, errors.New("too many DNS resolvers")
	}
	return result, nil
}

func (r nativeResolver) upstreams(fallbackInterface string) []string {
	var result []string
	for _, a := range r.servers {
		iface := r.iface
		if iface == "" {
			iface = fallbackInterface
		}
		if a.IsLoopback() {
			iface = ""
		}
		if a.Is6() && a.IsLinkLocalUnicast() && a.Zone() == "" {
			a = a.WithZone(iface)
		}
		u := url.URL{Scheme: "udp", Host: net.JoinHostPort(a.String(), strconv.Itoa(r.port))}
		endpoint := u.String()
		if iface != "" {
			endpoint += "#" + iface
		}
		result = append(result, endpoint)
	}
	return result
}

func (p managedDNSPlan) defaultResolver() (nativeResolver, error) {
	var selected *nativeResolver
	for i := range p.resolvers {
		r := &p.resolvers[i]
		if r.domain == "" && !r.scoped && len(r.servers) > 0 && (selected == nil || r.order < selected.order) {
			selected = r
		}
	}
	if selected == nil {
		return nativeResolver{}, errors.New("未找到原网络 DNS，未接管系统解析")
	}
	return *selected, nil
}

func discoverManagedDNS() (*managedDNSPlan, error) {
	data, err := systemCommand("/usr/sbin/scutil", "--dns")
	if err != nil {
		return nil, err
	}
	resolvers, err := parseNativeResolvers(string(data))
	if err != nil {
		return nil, err
	}
	p := &managedDNSPlan{resolvers: resolvers}
	if _, err = p.defaultResolver(); err != nil {
		return nil, err
	}
	// Reserve/check both protocols. The core must bind and answer before the
	// system resolver is installed; a race for this port fails closed at startup.
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer tcp.Close()
	p.port = uint16(tcp.Addr().(*net.TCPAddr).Port)
	udp, err := net.ListenPacket("udp4", tcp.Addr().String())
	if err != nil {
		return nil, err
	}
	udp.Close()
	return p, nil
}

func configureManagedDNS(config map[string]any, r routeRequest, original originalNetwork) {
	p := r.managedDNS
	if p == nil {
		return
	}
	base, _ := p.defaultResolver() // Discovery validates this before starting the core.
	direct := base.upstreams(original.Interface)
	policy := map[string]any{}
	// System mDNS and more-specific supplemental resolvers retain priority in
	// macOS. These policies also preserve private queries received by the TUN.
	for _, domain := range []string{"local", "lan", "home.arpa", "internal", "in-addr.arpa", "ip6.arpa"} {
		policy["+."+domain] = direct
	}
	// A single-label hostname is a local name, never a public lookup.
	policy["*"] = direct
	for _, rule := range r.DirectDomains {
		if _, ip := directIPPrefix(rule.Domain); ip {
			continue
		}
		key := rule.Domain
		if rule.IncludeSubdomains {
			key = "+." + key
		}
		policy[key] = direct
	}
	privateResolvers := map[string]nativeResolver{}
	for _, resolver := range p.resolvers {
		if resolver.domain != "" && len(resolver.servers) > 0 && validDirectDomain(resolver.domain) {
			previous, exists := privateResolvers[resolver.domain]
			if !exists || resolver.order < previous.order || (resolver.order == previous.order && previous.scoped && !resolver.scoped) {
				privateResolvers[resolver.domain] = resolver
			}
		}
	}
	for domain, resolver := range privateResolvers {
		policy["+."+domain] = resolver.upstreams(original.Interface)
	}
	proxied := []string{"https://1.1.1.1/dns-query#GoConnect", "https://8.8.8.8/dns-query#GoConnect"}
	config["dns"] = map[string]any{
		"enable": true, "listen": net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p.port))),
		"ipv6": true, "enhanced-mode": "normal", "prefer-h3": false,
		"use-hosts": true, "use-system-hosts": true,
		"nameserver": proxied, "nameserver-policy": policy,
		"default-nameserver": direct, "proxy-server-nameserver": direct,
		// Excluded apps still use the domain policy. Never re-resolve protected
		// public names with system DNS when their socket selects OriginalNetwork.
		"direct-nameserver": proxied, "direct-nameserver-follow-policy": true,
	}
	config["hosts"] = map[string]any{"goconnect-dns-ready.invalid": "127.0.0.1"}
	// System lookups reach the loopback listener directly. Do not hijack every
	// port-53 packet: explicitly addressed corporate/private DNS must retain its
	// existing route and destination. Application-owned encrypted DNS is outside
	// this native resolver policy and follows the application's traffic routing.
}

func managedDNSReady(port uint16) error {
	name := dnsmessage.MustNewName("goconnect-dns-ready.invalid.")
	q := dnsmessage.Message{Header: dnsmessage.Header{ID: 0x4743, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	wire, _ := q.Pack()
	for _, network := range []string{"udp4", "tcp4"} {
		conn, err := net.DialTimeout(network, net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), time.Second)
		if err != nil {
			return err
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		request := wire
		if network == "tcp4" {
			request = append(binary.BigEndian.AppendUint16(nil, uint16(len(wire))), wire...)
		}
		_, err = conn.Write(request)
		var response []byte
		if err == nil {
			if network == "tcp4" {
				header := make([]byte, 2)
				_, err = io.ReadFull(conn, header)
				if err == nil {
					response = make([]byte, binary.BigEndian.Uint16(header))
					_, err = io.ReadFull(conn, response)
				}
			} else {
				response = make([]byte, 4096)
				var n int
				n, err = conn.Read(response)
				response = response[:n]
			}
		}
		conn.Close()
		if err != nil {
			return err
		}
		var answer dnsmessage.Message
		if answer.Unpack(response) != nil || !answer.Response || answer.ID != q.ID || answer.RCode != dnsmessage.RCodeSuccess || len(answer.Questions) != 1 || answer.Questions[0] != q.Questions[0] || len(answer.Answers) != 1 {
			return errors.New("本机 DNS 未通过启动验证")
		}
		if a, ok := answer.Answers[0].Body.(*dnsmessage.AResource); !ok || a.A != [4]byte{127, 0, 0, 1} {
			return errors.New("本机 DNS 回应不匹配")
		}
	}
	return nil
}

func installManagedDNS(p *managedDNSPlan) (managedDNSLease, error) {
	if err := managedDNSReady(p.port); err != nil {
		return nil, fmt.Errorf("DNS 接管前验证失败: %w", err)
	}
	lease, err := acquireManagedDNS(p.port, "")
	if err != nil {
		return nil, err
	}
	if err = waitManagedDNS(p.port, "", true); err != nil {
		lease.close()
		return nil, err
	}
	return lease, nil
}

// Store acceptance alone is insufficient: wait until macOS publishes the
// resolver actually used by native lookup clients.
func waitManagedDNS(port uint16, scope string, present bool) error {
	for i := 0; i < 20; i++ {
		data, err := systemCommand("/usr/sbin/scutil", "--dns")
		if err != nil {
			return err
		}
		resolvers, err := parseNativeResolvers(string(data))
		if err != nil {
			return err
		}
		found := false
		for _, resolver := range resolvers {
			if !resolver.scoped && resolver.domain == scope && resolver.port == int(port) && len(resolver.servers) == 1 && resolver.servers[0] == netip.MustParseAddr("127.0.0.1") {
				found = true
			}
		}
		if present && scope == "" && found {
			selected, e := (managedDNSPlan{resolvers: resolvers}).defaultResolver()
			found = e == nil && selected.port == int(port) && len(selected.servers) == 1 && selected.servers[0] == netip.MustParseAddr("127.0.0.1")
		}
		if found == present {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("系统未确认 DNS 接管状态")
}
