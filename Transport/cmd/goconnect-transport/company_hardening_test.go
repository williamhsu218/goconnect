package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
)

func TestCompanyCanonicalGenerationRejectsLengthOnlyTokens(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", 36), strings.Repeat("a", 36), "00000000_0000-4000-8000-000000000001", "00000000-0000-4000-8000-00000000000g"} {
		if canonicalCompanyGeneration(value) {
			t.Fatalf("accepted %q", value)
		}
		g, claim, _ := fixtureCompanyGate(t)
		before := g.snapshot()
		if err := g.command(context.Background(), companyCommand{ExpectedRevision: before.Revision, Generation: value, Action: "prepare", Backend: &claim}); err == nil {
			t.Fatal("malformed generation reached prepare")
		}
		if g.snapshot().Revision != before.Revision {
			t.Fatal("invalid generation mutated revision")
		}
	}
	for _, value := range []string{companyTestGeneration, "ABCDEF01-1234-4567-89AB-CDEF01234567"} {
		if !canonicalCompanyGeneration(value) {
			t.Fatal("canonical UUID rejected")
		}
	}
}

func TestCompanyBlockedDNSRebuildsQuestionWithoutEDNSOrResources(t *testing.T) {
	question := dnsmessage.Question{Name: dnsmessage.MustNewName("app.corp.example.invalid."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	request := dnsmessage.Message{Header: dnsmessage.Header{ID: 123, RecursionDesired: true, CheckingDisabled: true}, Questions: []dnsmessage.Question{question}, Additionals: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("."), Type: dnsmessage.TypeOPT, Class: 4096}, Body: &dnsmessage.OPTResource{Options: []dnsmessage.Option{{Code: 65001, Data: []byte{1, 2, 3}}}}}}}
	wire, err := request.Pack()
	if err != nil {
		t.Fatal(err)
	}
	reply := blockedCompanyDNSAnswer(wire)
	var decoded dnsmessage.Message
	if err := decoded.Unpack(reply); err != nil {
		t.Fatal(err)
	}
	if decoded.Header.ID != 123 || !decoded.Header.Response || decoded.Header.RCode != dnsmessage.RCodeServerFailure || !decoded.Header.RecursionDesired || decoded.Header.AuthenticData || decoded.Header.CheckingDisabled {
		t.Fatalf("invalid blocked header: %+v", decoded.Header)
	}
	if len(decoded.Questions) != 1 || decoded.Questions[0] != question || len(decoded.Answers) != 0 || len(decoded.Authorities) != 0 || len(decoded.Additionals) != 0 {
		t.Fatal("request resources survived refusal")
	}
	rebuilt, _ := decoded.Pack()
	if len(rebuilt) != len(reply) {
		t.Fatal("unparsed trailing bytes in refusal")
	}
	request.Questions = append(request.Questions, question)
	wire, _ = request.Pack()
	if blockedCompanyDNSAnswer(wire) != nil {
		t.Fatal("multi-question query accepted")
	}
	request.Questions = []dnsmessage.Question{question}
	request.Header.Response = true
	wire, _ = request.Pack()
	if blockedCompanyDNSAnswer(wire) != nil {
		t.Fatal("response answered as query")
	}
	if blockedCompanyDNSAnswer([]byte{1, 2, 3}) != nil || blockedCompanyDNSAnswer(make([]byte, 4097)) != nil {
		t.Fatal("malformed/oversize DNS answered")
	}
}

func TestCompanyBackendLivenessUsesUIDPIDAndBirth(t *testing.T) {
	child := exec.Command("/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	}()
	birth, err := companyProcessStartTime(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	claim := companyBackendClaim{PID: child.Process.Pid, StartTime: birth}
	uid := uint32(os.Getuid())
	if !companyBackendAlive(claim, uid) {
		t.Fatal("owned child not recognized")
	}
	if companyBackendAlive(claim, uid+1) {
		t.Fatal("UID mismatch accepted")
	}
	claim.StartTime++
	if companyBackendAlive(claim, uid) {
		t.Fatal("reused PID accepted")
	}
	claim.StartTime = birth
	child.Process.Kill()
	child.Wait()
	if companyBackendAlive(claim, uid) {
		t.Fatal("exited and reaped backend alive")
	}
}

func TestCompanyBackendLossBlocksOwnedFlowsWithoutClosingGate(t *testing.T) {
	for _, active := range []bool{false, true} {
		g, claim, fixture := fixtureCompanyGate(t)
		generation := companyTestGeneration
		if err := g.command(context.Background(), companyCommand{Generation: generation, Action: "prepare", Backend: &claim}); err != nil {
			t.Fatal(err)
		}
		if active {
			if err := g.command(context.Background(), companyCommand{ExpectedRevision: g.snapshot().Revision, Generation: generation, Action: "commit"}); err != nil {
				t.Fatal(err)
			}
		}
		var held net.Conn
		if active {
			front, _ := proxy.SOCKS5("tcp", g.listener.Addr().String(), &proxy.Auth{User: "goconnect", Password: g.token}, nil)
			var err error
			held, err = front.Dial("tcp", "10.201.0.9:443")
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			echoCompany(t, held)
		}
		before := g.snapshot()
		// Simulate PID reuse after an authenticated attach. The kernel-check test
		// above separately proves a real owned child is rejected after exit.
		g.mu.Lock()
		b := g.prepared
		if active {
			b = g.active
		}
		b.claim.StartTime++
		g.mu.Unlock()
		g.checkBackendLiveness()
		state := g.snapshot()
		if state.State != "blocked" || state.Revision != before.Revision+1 || state.Generation != generation || state.Message == "" {
			t.Fatalf("bad liveness refusal: %+v", state)
		}
		if g.ctx.Err() != nil {
			t.Fatal("backend failure cancelled stable gate")
		}
		g.checkBackendLiveness()
		if g.snapshot().Revision != state.Revision {
			t.Fatal("duplicate death changed revision")
		}
		if err := g.command(context.Background(), companyCommand{ExpectedRevision: before.Revision, Generation: generation, Action: "commit"}); err == nil {
			t.Fatal("stale commit reopened dead backend")
		}
		if held != nil {
			held.SetReadDeadline(time.Now().Add(time.Second))
			var buf [1]byte
			if _, err := held.Read(buf[:]); err == nil {
				t.Fatal("owned flow survived revocation")
			}
		}
		beforeDNS := fixture.dns.Load()
		dns, err := net.Dial("tcp", g.dnsTCP.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		q := dnsmessage.Message{Header: dnsmessage.Header{ID: 123}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("app.corp.example.invalid."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
		wire, _ := q.Pack()
		frame := make([]byte, 2+len(wire))
		binary.BigEndian.PutUint16(frame, uint16(len(wire)))
		copy(frame[2:], wire)
		dns.SetDeadline(time.Now().Add(time.Second))
		dns.Write(frame)
		var h [2]byte
		if _, err := io.ReadFull(dns, h[:]); err != nil {
			t.Fatal("DNS listener stopped", err)
		}
		dns.Close()
		if fixture.dns.Load() != beforeDNS {
			t.Fatal("blocked DNS reached company resolver")
		}
	}
}
