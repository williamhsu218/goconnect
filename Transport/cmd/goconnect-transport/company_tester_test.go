package main

import (
	"context"
	"golang.org/x/net/dns/dnsmessage"
	"net/netip"
	"testing"
)

func TestCompanyTesterScopeValidation(t *testing.T) {
	good := companyTestRequest{Server: "https://vpn.example.invalid:443", Interface: "en0", GatewayIPs: []string{"198.51.100.4"}, Network: "10.201.0.9/32", DNSName: "app.corp.example.invalid"}
	if e := good.validate(true); e != nil {
		t.Fatal(e)
	}
	for _, network := range []string{"0.0.0.0/0", "10.0.0.0/8", "8.8.8.8/32", "fc00::1/128", "100.64.0.1/32"} {
		bad := good
		bad.Network = network
		if bad.validate(true) == nil {
			t.Fatalf("accepted scope %s", network)
		}
	}
	for _, iface := range []string{"lo0", "utun100", "en0;echo", ""} {
		bad := good
		bad.Interface = iface
		if bad.validate(true) == nil {
			t.Fatalf("accepted interface %s", iface)
		}
	}
	bad := good
	bad.GatewayIPs = []string{"127.0.0.1"}
	if bad.validate(true) == nil {
		t.Fatal("loopback gateway accepted")
	}
	bad = good
	bad.Server = "https://user:secret@vpn.example.invalid"
	if bad.validate(true) == nil {
		t.Fatal("userinfo accepted")
	}
}
func TestCompanyTesterDNSOwnerTypeAndChainValidation(t *testing.T) {
	name := dnsmessage.MustNewName("app.corp.example.invalid.")
	alias := dnsmessage.MustNewName("canonical.corp.example.invalid.")
	other := dnsmessage.MustNewName("unrelated.example.invalid.")
	q := dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	address := func(owner dnsmessage.Name) dnsmessage.Resource {
		return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: owner, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.AResource{A: [4]byte{10, 201, 0, 9}}}
	}
	cname := func(owner, target dnsmessage.Name) dnsmessage.Resource {
		return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: owner, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.CNAMEResource{CNAME: target}}
	}
	cases := []struct {
		name   string
		modify func(*dnsmessage.Message)
		valid  bool
	}{
		{"direct", func(m *dnsmessage.Message) { m.Answers = []dnsmessage.Resource{address(name)} }, true},
		{"CNAME", func(m *dnsmessage.Message) { m.Answers = []dnsmessage.Resource{cname(name, alias), address(alias)} }, true},
		{"wrong owner", func(m *dnsmessage.Message) { m.Answers = []dnsmessage.Resource{address(other)} }, false},
		{"unrelated extra", func(m *dnsmessage.Message) { m.Answers = []dnsmessage.Resource{address(name), address(other)} }, false},
		{"wrong id", func(m *dnsmessage.Message) { m.ID++ }, false},
		{"wrong question", func(m *dnsmessage.Message) { m.Questions[0].Name = other }, false},
		{"wrong type", func(m *dnsmessage.Message) {
			m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AAAAResource{AAAA: netip.MustParseAddr("fc00::1").As16()}}}
		}, false},
		{"wrong class", func(m *dnsmessage.Message) {
			r := address(name)
			r.Header.Class = dnsmessage.ClassCHAOS
			m.Answers = []dnsmessage.Resource{r}
		}, false},
		{"CNAME loop", func(m *dnsmessage.Message) { m.Answers = []dnsmessage.Resource{cname(name, alias), cname(alias, name)} }, false},
		{"unchained CNAME", func(m *dnsmessage.Message) { m.Answers = []dnsmessage.Resource{cname(other, alias), address(name)} }, false},
		{"ambiguous owner", func(m *dnsmessage.Message) {
			m.Answers = []dnsmessage.Resource{cname(name, alias), address(name), address(alias)}
		}, false},
		{"NXDOMAIN", func(m *dnsmessage.Message) { m.RCode = dnsmessage.RCodeNameError }, false},
		{"SERVFAIL", func(m *dnsmessage.Message) { m.RCode = dnsmessage.RCodeServerFailure }, false},
		{"truncated", func(m *dnsmessage.Message) { m.Truncated = true }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := dnsmessage.Message{Header: dnsmessage.Header{ID: 42, Response: true}, Questions: []dnsmessage.Question{q}}
			c.modify(&m)
			wire, e := m.Pack()
			if e != nil {
				t.Fatal(e)
			}
			_, ips, _, e := companyTestParseDNS(wire, 42, q)
			if (e == nil) != c.valid {
				t.Fatalf("valid=%v err=%v", c.valid, e)
			}
			if c.valid && (len(ips) != 1 || ips[0] != "10.201.0.9") {
				t.Fatal("missing approved answer")
			}
		})
	}
	// AAAA and NODATA are not mislabelled A resolution.
	q.Type = dnsmessage.TypeAAAA
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: 42, Response: true}, Questions: []dnsmessage.Question{q}}
	wire, _ := m.Pack()
	rcode, ips, _, e := companyTestParseDNS(wire, 42, q)
	if e != nil || len(ips) != 0 || rcode == "" {
		t.Fatal("NODATA should remain a valid packet without resolution")
	}
	m.Answers = []dnsmessage.Resource{address(name)}
	wire, _ = m.Pack()
	if _, _, _, e = companyTestParseDNS(wire, 42, q); e == nil {
		t.Fatal("A response to AAAA query accepted")
	}
}
func TestCompanyTesterWebSchemeClosedBeforeDial(t *testing.T) {
	p := companyTestWeb(context.Background(), nil, "app.corp.example.invalid", netip.MustParseAddr("10.201.0.9"), "ftp")
	if p.Success || p.Error == "" {
		t.Fatal("unexpected scheme accepted")
	}
}
