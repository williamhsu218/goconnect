package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Set only when linking an explicitly isolated operational test build:
// -ldflags '-X main.serviceBuildFlavor=test'. No environment or CLI override.
var serviceBuildFlavor = "production"

const sharedCaptureLockPath = "/var/run/com.willhsu.GoConnect.capture.lock"
const productionAppDirectory = "/Applications/GoConnect.app"
const productionStatusExecutable = productionAppDirectory + "/Contents/Resources/Runtime/bin/GoConnectTransport"
const productionStatusLimit = 65_536
const productionStatusTimeout = 5 * time.Second

type serviceNamespacePaths struct {
	Flavor       string `json:"flavor"`
	Label        string `json:"label"`
	Base         string `json:"base"`
	RunDirectory string `json:"runDirectory"`
	Socket       string `json:"socket"`
	Plist        string `json:"plist"`
	PolicyPath   string `json:"policyPath"`
	CaptureLock  string `json:"captureLock"`
}

func serviceNamespaceForFlavor(flavor string) (serviceNamespacePaths, error) {
	var id string
	switch flavor {
	case "production":
		id = "com.willhsu.GoConnect"
	case "test":
		id = "com.willhsu.GoConnect.DualVPNTest"
	default:
		return serviceNamespacePaths{}, errors.New("unsupported service build flavor")
	}
	base := "/Library/PrivilegedHelperTools/" + id
	label := id + ".network"
	return serviceNamespacePaths{Flavor: flavor, Label: label, Base: base,
		RunDirectory: base + "/run", Socket: base + "/run/network.sock",
		Plist: "/Library/LaunchDaemons/" + label + ".plist", PolicyPath: base + "/service.json", CaptureLock: sharedCaptureLockPath}, nil
}

var selectedServiceNamespace = func() serviceNamespacePaths {
	paths, err := serviceNamespaceForFlavor(serviceBuildFlavor)
	if err != nil {
		panic("GoConnectTransport: unsupported service build flavor")
	}
	return paths
}()

type testHostReply struct {
	State           string `json:"state"`
	Available       bool   `json:"available"`
	ProductionState string `json:"productionState,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

func testHostUnknown() testHostReply {
	return testHostReply{State: "unknown", Reason: "无法确认正式 GoConnect 已空闲，已拒绝测试网络接管。"}
}

func testHostStatus(ctx context.Context) testHostReply {
	return testHostStatusWith(ctx, serviceBuildFlavor, os.Getuid(), os.Geteuid(), os.Lstat, runProductionStatus)
}

type productionStatusRunner func(context.Context) ([]byte, error)

func testHostStatusWith(ctx context.Context, flavor string, uid, euid int, stat func(string) (os.FileInfo, error), run productionStatusRunner) testHostReply {
	if flavor != "test" {
		return testHostReply{State: "unknown", Reason: "此检查仅用于隔离网络测试版。"}
	}
	if uid == 0 || euid == 0 {
		return testHostReply{State: "blocked", Reason: "必须以普通用户检查正式 GoConnect；禁止 root 执行正式 App 内的组件。"}
	}
	app, err := stat(productionAppDirectory)
	if os.IsNotExist(err) {
		return testHostReply{State: "available", Available: true, ProductionState: "appMissing"}
	}
	if err != nil || app == nil || !app.IsDir() {
		return testHostUnknown()
	}
	st, err := stat(productionStatusExecutable)
	// A present but incomplete/unreadable app is unknown, never "not installed".
	if err != nil || st == nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return testHostUnknown()
	}
	bounded, cancel := context.WithTimeout(ctx, productionStatusTimeout)
	defer cancel()
	data, err := run(bounded)
	if err != nil || bounded.Err() != nil || len(data) == 0 || len(data) > productionStatusLimit {
		return testHostUnknown()
	}
	// Decode only the two gate fields. Missing busy must not become false.
	var reply struct {
		State *string `json:"state"`
		Busy  *bool   `json:"busy"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&reply) != nil || decoder.Decode(new(any)) != io.EOF || reply.State == nil || reply.Busy == nil {
		return testHostUnknown()
	}
	switch *reply.State {
	case "ready", "notInstalled", "needsUpdate", "requiresAuthorization", "unavailable":
	default:
		return testHostUnknown()
	}
	if *reply.Busy {
		return testHostReply{State: "blocked", ProductionState: *reply.State, Reason: "正式 GoConnect 仍在连接或使用网络服务；请先主动断开，再启动测试接管。"}
	}
	switch *reply.State {
	case "ready", "notInstalled":
		return testHostReply{State: "available", Available: true, ProductionState: *reply.State}
	default:
		return testHostUnknown()
	}
}

// Ordinary-user execution only. Never call from a root service/install path.
// The fixed path, argv and minimal environment cannot be supplied by the GUI.
func productionStatusCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, productionStatusExecutable, "service-status")
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=en_US.UTF-8"}
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 200 * time.Millisecond
	return cmd
}
func runProductionStatus(ctx context.Context) ([]byte, error) {
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		return nil, errors.New("root cannot execute production app transport")
	}
	bounded, cancel := context.WithTimeout(ctx, productionStatusTimeout)
	defer cancel()
	output := &boundedStatusOutput{limit: productionStatusLimit, cancel: cancel}
	cmd := productionStatusCommand(bounded)
	cmd.Stdout = output
	err := cmd.Run()
	data, overflow := output.result()
	if overflow {
		return nil, errors.New("production status exceeds limit")
	}
	if err != nil || bounded.Err() != nil {
		return nil, errors.New("production status unavailable")
	}
	return data, nil
}

type boundedStatusOutput struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *boundedStatusOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	size := len(p)
	remaining := b.limit - len(b.data)
	if len(p) > remaining {
		if remaining > 0 {
			b.data = append(b.data, p[:remaining]...)
		}
		b.overflow = true
		if b.cancel != nil {
			b.cancel()
		}
		return size, nil
	}
	b.data = append(b.data, p...)
	return size, nil
}
func (b *boundedStatusOutput) result() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.overflow
}

// Acquires only the existing global capture inode. A missing lock fails closed:
// creating it here would mutate the production namespace and allow an initial
// create-vs-flock race. This test distribution targets machines with GoConnect.
func acquireExistingServiceCaptureLock() (*os.File, error) {
	return openExistingCaptureLock(sharedCaptureLockPath, 0)
}
func openExistingCaptureLock(path string, uid uint32) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil, errors.New("缺少正式 GoConnect 的共享捕获锁；测试服务仅支持已安装并初始化正式服务的环境。")
	}
	if err != nil {
		return nil, errors.New("无法安全检查共享捕获锁，拒绝维护测试服务。")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	raw, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || st.Mode().Perm() != 0o600 || raw.Uid != uid || raw.Nlink != 1 {
		file.Close()
		return nil, errors.New("共享捕获锁的所有权或权限无效。")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("已有 GoConnect 捕获会话，请先主动断开后再维护测试服务。")
	}
	return file, nil
}
