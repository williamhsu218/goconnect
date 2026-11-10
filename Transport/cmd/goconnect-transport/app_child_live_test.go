package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// Signed stand-ins perform real socket operations. No production App, TUN,
// system DNS, route or service installation is touched by this acceptance.
func TestLiveNativeAppChildProcess(t *testing.T) {
	role := os.Getenv("GOCONNECT_APP_CHILD_ROLE")
	if role == "" {
		t.Skip("isolated subprocess entry")
	}
	if role == "parent" {
		child := os.Getenv("GOCONNECT_APP_CHILD_EXECUTABLE")
		args := []string{"-test.run=^TestLiveNativeAppChildProcess$"}
		command := exec.Command(child, args...)
		if os.Getenv("GOCONNECT_APP_CHILD_SHELL") == "1" {
			command = exec.Command("/bin/sh", "-c", "\"$1\" -test.run=^TestLiveNativeAppChildProcess$; wait", "probe", child)
		}
		command.Env = append(os.Environ(), "GOCONNECT_APP_CHILD_ROLE=child")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if os.Getenv("GOCONNECT_APP_CHILD_DETACH") == "1" {
			command.Env = append(command.Env, "GOCONNECT_APP_CHILD_DELAY=1")
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if role != "child" {
		t.Fatal("invalid isolated role")
	}
	if os.Getenv("GOCONNECT_APP_CHILD_DELAY") == "1" {
		time.Sleep(1500 * time.Millisecond)
	}
	if os.Getenv("GOCONNECT_APP_CHILD_UDP") == "1" {
		control, relay, err := liveUDPRelay(os.Getenv("GOCONNECT_APP_CHILD_SOCKS"))
		if err != nil {
			t.Fatal(err)
		}
		defer control.Close()
		defer relay.Close()
		address := net.ParseIP(os.Getenv("GOCONNECT_APP_CHILD_ADDRESS")).To4()
		if address == nil {
			t.Fatal("IPv4 fixture address required")
		}
		packet := append([]byte{0, 0, 0, 1}, address...)
		packet = binary.BigEndian.AppendUint16(packet, 8080)
		packet = append(packet, []byte("child")...)
		relay.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err = relay.Write(packet); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 2048)
		n, err := relay.Read(buffer)
		if err != nil || n < 10 {
			t.Fatalf("UDP fixture response: %d %v", n, err)
		}
		fmt.Println("APP_CHILD_BODY=" + string(buffer[10:n]))
		return
	}
	dialer, err := proxy.SOCKS5("tcp", os.Getenv("GOCONNECT_APP_CHILD_SOCKS"), nil, &net.Dialer{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.(proxy.ContextDialer).DialContext, DisableKeepAlives: true}}
	r, err := client.Get("http://" + net.JoinHostPort(os.Getenv("GOCONNECT_APP_CHILD_ADDRESS"), "8080"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("APP_CHILD_BODY=" + string(body))
}

func nativeChildFixture(t *testing.T) (root, other, child, unsigned, newChild string) {
	t.Helper()
	identity := os.Getenv("GOCONNECT_APP_CHILD_SIGN_IDENTITY")
	if identity == "" {
		t.Skip("explicit local signing identity required for native signed child acceptance")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(base, "Selected.app")
	other = filepath.Join(base, "Other.app")
	child = filepath.Join(base, "external", "v1", "child")
	newChild = filepath.Join(base, "external", "v2", "child")
	unsigned = filepath.Join(base, "external", "unsigned", "child")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "Contents/MacOS/Parent"), filepath.Join(other, "Contents/MacOS/Parent"), child, newChild, unsigned} {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, content, 0700); err != nil {
			t.Fatal(err)
		}
		signer := identity
		if path == unsigned {
			signer = "-"
		}
		command := exec.Command("/usr/bin/codesign", "--force", "--sign", signer, "--options", "runtime", "--timestamp=none", "--identifier", "com.willhsu.GoConnect.NativeChildFixture", path)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("local fixture signature failed: %v %s", err, out)
		}
	}
	return
}

func nativeChildRequest(t *testing.T, executable, child, role string, f *addressPolicyCore, address string, extra ...string) string {
	t.Helper()
	command := exec.Command(executable, "-test.run=^TestLiveNativeAppChildProcess$")
	command.Env = append(os.Environ(), "GOCONNECT_APP_CHILD_ROLE="+role, "GOCONNECT_APP_CHILD_EXECUTABLE="+child,
		"GOCONNECT_APP_CHILD_SOCKS=127.0.0.1:"+strconv.Itoa(int(f.listed)), "GOCONNECT_APP_CHILD_ADDRESS="+address)
	command.Env = append(command.Env, extra...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated process failed: %v %s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "APP_CHILD_BODY=") {
			return strings.TrimPrefix(line, "APP_CHILD_BODY=")
		}
	}
	t.Fatalf("missing real response: %s", out)
	return ""
}

func TestLiveNativeAppChildRouting(t *testing.T) {
	if os.Getenv("GOCONNECT_TEST_RUNTIME") == "" {
		t.Skip("packaged candidate runtime required")
	}
	root, other, child, unsigned, newChild := nativeChildFixture(t)
	parent := filepath.Join(root, "Contents/MacOS/Parent")
	otherParent := filepath.Join(other, "Contents/MacOS/Parent")
	for _, mode := range []string{modeWhitelist, modeGlobal} {
		t.Run(mode, func(t *testing.T) {
			selected, fallback := "GoConnect", "OriginalNetwork"
			if mode == modeGlobal {
				selected, fallback = fallback, selected
			}
			r := routeRequest{AppPaths: []string{root}, processOwnerUID: uint32(os.Getuid())}
			f := newAddressPolicyCore(t, mode, r, nil, "203.0.113.42", "203.0.113.42", addressPolicyOptions{nativeProcesses: true})
			address := f.query(t, "native-child.example")
			cases := []struct {
				name, executable, child, role, want string
				extra                               []string
			}{
				{"signed-external-child", parent, child, "parent", selected, nil},
				{"same-file-independent", child, child, "child", fallback, nil},
				{"signed-external-child-udp", parent, child, "parent", selected + ":child", []string{"GOCONNECT_APP_CHILD_UDP=1"}},
				{"same-file-independent-udp", child, child, "child", fallback + ":child", []string{"GOCONNECT_APP_CHILD_UDP=1"}},
				{"other-app-same-team", otherParent, child, "parent", fallback, nil},
				{"new-version-path", parent, newChild, "parent", selected, nil},
				{"unsigned-child", parent, unsigned, "parent", fallback, nil},
				{"shared-shell", parent, child, "parent", fallback, []string{"GOCONNECT_APP_CHILD_SHELL=1"}},
				{"detached-parent-exit", parent, child, "parent", fallback, []string{"GOCONNECT_APP_CHILD_DETACH=1"}},
			}
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					if got := nativeChildRequest(t, c.executable, c.child, c.role, f, address, c.extra...); got != c.want {
						t.Fatalf("actual exit %q, want %q", got, c.want)
					}
				})
			}
			t.Run("explicit-cli-still-works", func(t *testing.T) {
				r.AppPaths = append(r.AppPaths, child)
				g := newAddressPolicyCore(t, mode, r, nil, "203.0.113.42", "203.0.113.42", addressPolicyOptions{nativeProcesses: true})
				if got := nativeChildRequest(t, child, child, "child", g, g.query(t, "explicit-cli.example")); got != selected {
					t.Fatalf("explicit CLI exit %q, want %q", got, selected)
				}
			})
			if mode == modeWhitelist {
				t.Run("explicit-direct-wins", func(t *testing.T) {
					r.AppPaths = []string{root}
					r.DirectAppPaths = []string{child}
					g := newAddressPolicyCore(t, mode, r, nil, "203.0.113.42", "203.0.113.42", addressPolicyOptions{nativeProcesses: true})
					if got := nativeChildRequest(t, parent, child, "parent", g, g.query(t, "direct-cli.example")); got != "OriginalNetwork" {
						t.Fatalf("direct exclusion overridden: %q", got)
					}
				})
			}
		})
	}
}
