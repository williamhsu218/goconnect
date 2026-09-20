package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const serviceLabel = "com.willhsu.GoConnect.network"
const serviceBase = "/Library/PrivilegedHelperTools/com.willhsu.GoConnect"
const serviceRunDirectory = serviceBase + "/run"
const serviceSocket = serviceRunDirectory + "/network.sock"
const servicePlist = "/Library/LaunchDaemons/" + serviceLabel + ".plist"
const servicePolicyPath = serviceBase + "/service.json"
const serviceVersion = "1"

type serviceRequest struct {
	Action  string `json:"action"`
	Session string `json:"session,omitempty"`
}
type serviceReply struct {
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Busy    bool   `json:"busy"`
	Message string `json:"message,omitempty"`
}

func decodeServiceRequest(data []byte) (serviceRequest, error) {
	var r serviceRequest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, err
	}
	if d.Decode(new(any)) != io.EOF {
		return r, errors.New("trailing input")
	}
	switch r.Action {
	case "status", "diagnostics":
		if r.Session != "" {
			return r, errors.New("unexpected session")
		}
	case "route":
		if len(r.Session) > 2048 || !filepath.IsAbs(r.Session) || filepath.Clean(r.Session) != r.Session || filepath.Base(r.Session) != "session.json" || strings.ContainsAny(r.Session, "\x00\r\n") {
			return r, errors.New("invalid session path")
		}
	default:
		return r, errors.New("unsupported service operation")
	}
	return r, nil
}

func serviceConnect() (*net.UnixConn, error) {
	if err := secureRootPath(serviceRunDirectory, true); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", serviceSocket, 2*time.Second)
	if err != nil {
		return nil, err
	}
	u := conn.(*net.UnixConn)
	identity, err := verifyServicePeer(u, "")
	if err != nil || identity.UID != 0 {
		u.Close()
		return nil, errors.New("untrusted network service")
	}
	return u, nil
}

func serviceStatus() serviceReply {
	policy, err := readServicePolicy()
	if os.IsNotExist(err) {
		return serviceReply{State: "notInstalled"}
	}
	if err != nil {
		return serviceReply{State: "unavailable", Message: "服务安装信息无效，请修复服务。"}
	}
	runtime, err := runtimePath()
	if err != nil {
		return serviceReply{State: "unavailable"}
	}
	revision, err := runtimeRevision(runtime)
	if err != nil {
		return serviceReply{State: "unavailable"}
	}
	if policy.UID != uint32(os.Getuid()) {
		return serviceReply{State: "requiresAuthorization", Message: "当前用户尚未授权使用本机服务。"}
	}
	if revision != policy.Revision {
		return serviceReply{State: "needsUpdate", Version: policy.Version}
	}
	conn, err := serviceConnect()
	if err != nil {
		return serviceReply{State: "unavailable", Version: policy.Version}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if json.NewEncoder(conn).Encode(serviceRequest{Action: "status"}) != nil {
		return serviceReply{State: "unavailable"}
	}
	var reply serviceReply
	if json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&reply) != nil {
		return serviceReply{State: "unavailable"}
	}
	return reply
}

// This ordinary-user client keeps the authenticated socket open for one session.
// Closing it or losing the GUI lease revokes the worker, without stopping launchd.
func serviceRoute(ctx context.Context, path string) error {
	conn, err := serviceConnect()
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err = json.NewEncoder(conn).Encode(serviceRequest{Action: "route", Session: path}); err != nil {
		return err
	}
	decoder := json.NewDecoder(conn)
	var reply serviceReply
	if err = decoder.Decode(&reply); err != nil {
		return err
	}
	if reply.State != "accepted" {
		return errors.New(reply.Message)
	}
	_ = conn.SetDeadline(time.Time{})
	if err = decoder.Decode(&reply); err != nil {
		return err
	}
	if reply.State != "stopped" {
		return errors.New(reply.Message)
	}
	return nil
}

func runNetworkService(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("root service required")
	}
	policy, err := readServicePolicy()
	if err != nil {
		return err
	}
	runtime, err := runtimePath()
	if err != nil || runtime != filepath.Join(serviceBase, "versions", policy.Revision) {
		return errors.New("service location mismatch")
	}
	if err = secureRootPath(filepath.Join(runtime, "bin/GoConnectTransport"), false); err != nil {
		return err
	}
	if err = ensureRootDirectory(serviceRunDirectory); err != nil {
		return err
	}
	lock, err := rootLock(serviceRunDirectory + "/service.lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	if st, e := os.Lstat(serviceSocket); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return errors.New("unexpected socket file")
		}
		if err = os.Remove(serviceSocket); err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: serviceSocket, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = restrictServiceSocket(serviceSocket, policy.UID); err != nil {
		return err
	}
	go func() { <-ctx.Done(); listener.Close() }()
	var active atomic.Bool
	var workers sync.WaitGroup
	defer func() {
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(16 * time.Second):
			trace("service.stop_timeout", 0)
		}
	}()
	slots := make(chan struct{}, 8)
	for {
		conn, e := listener.AcceptUnix()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		// Reject other UIDs with a cheap kernel check before code validation or slots.
		peer, peerErr := verifyServicePeer(conn, "")
		if peerErr != nil || (peer.UID != policy.UID && peer.UID != 0) {
			conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-slots }()
			defer conn.Close()
			handleServiceConnection(ctx, conn, runtime, policy.UID, &active)
		}()
	}
}

func handleServiceConnection(ctx context.Context, conn *net.UnixConn, runtime string, allowedUID uint32, active *atomic.Bool) {
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	write := func(r serviceReply) { _ = json.NewEncoder(conn).Encode(r) }
	identity, err := verifyServicePeer(conn, filepath.Join(runtime, "bin/GoConnectTransport"))
	if err != nil || (identity.UID != allowedUID && identity.UID != 0) || allowedUID < 501 {
		write(serviceReply{State: "denied", Message: "客户端未获本机服务授权，请更新或修复服务。"})
		return
	}
	reader := bufio.NewReaderSize(conn, 4096)
	data, err := reader.ReadSlice('\n')
	if err != nil {
		return
	}
	request, err := decodeServiceRequest(data)
	if err != nil {
		write(serviceReply{State: "denied", Message: "不支持的服务请求。"})
		return
	}
	if request.Action == "diagnostics" {
		_ = json.NewEncoder(conn).Encode(struct {
			State string            `json:"state"`
			Logs  map[string]string `json:"logs"`
		}{"diagnostics", readDiagnosticLogs()})
		return
	}
	if request.Action == "status" {
		write(serviceReply{State: "ready", Version: serviceVersion, Busy: active.Load()})
		return
	}
	if identity.UID != allowedUID {
		return
	}
	if !active.CompareAndSwap(false, true) {
		write(serviceReply{State: "busy", Message: "已有分流会话，请先断开。"})
		return
	}
	quarantined := false
	defer func() {
		if !quarantined {
			active.Store(false)
		}
	}()
	peer, err := unix.SysctlKinfoProc("kern.proc.pid", identity.PID)
	if err != nil || peer.Eproc.Ucred.Uid != identity.UID {
		return
	}
	// GUI and routing-check each spawn the narrow client. Only their own session
	// may be requested; the worker checks the UID and parent PID on its opened data.
	ownerPID := int(peer.Eproc.Ppid)
	worker := exec.Command(filepath.Join(runtime, "bin/GoConnectTransport"), "service-worker", request.Session, strconv.FormatUint(uint64(identity.UID), 10), strconv.Itoa(ownerPID))
	worker.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=en_US.UTF-8", "HOME=/var/root", "TMPDIR=/private/var/tmp"}
	lease, err := worker.StdinPipe()
	if err != nil {
		return
	}
	defer lease.Close()
	worker.Stdout = io.Discard
	worker.Stderr = io.Discard
	if err = worker.Start(); err != nil {
		write(serviceReply{State: "failed", Message: "无法启动本机分流服务。"})
		return
	}
	trace("worker.started", int64(worker.Process.Pid))
	write(serviceReply{State: "accepted"})
	_ = conn.SetDeadline(time.Time{})
	finished := make(chan error, 1)
	go func() { finished <- worker.Wait() }()
	disconnected := make(chan struct{})
	go func() { _, _ = reader.ReadByte(); close(disconnected) }()
	select {
	case err = <-finished:
	case <-ctx.Done():
		trace("worker.service_cancel", 0)
		lease.Close()
		select {
		case err = <-finished:
		case <-time.After(12 * time.Second):
			quarantined = true
			trace("worker.stop_timeout", int64(worker.Process.Pid))
			write(serviceReply{State: "failed", Message: "网络进程尚未退出，已暂停新连接；请保留诊断记录。"})
			go func() { <-finished; active.Store(false) }()
			return
		}
	case <-disconnected:
		trace("worker.client_disconnected", 0)
		lease.Close()
		select {
		case err = <-finished:
		case <-time.After(12 * time.Second):
			quarantined = true
			trace("worker.stop_timeout", int64(worker.Process.Pid))
			write(serviceReply{State: "failed", Message: "网络进程尚未退出，已暂停新连接；请保留诊断记录。"})
			go func() { <-finished; active.Store(false) }()
			return
		}
	}
	trace("worker.exited", int64(worker.ProcessState.ExitCode()))
	if err != nil {
		write(serviceReply{State: "failed", Message: "分流会话已结束，请检查诊断记录。"})
	} else {
		write(serviceReply{State: "stopped"})
	}
}

func serviceWorker(ctx context.Context, args []string) error {
	if len(args) != 3 || os.Geteuid() != 0 {
		return errors.New("invalid service worker")
	}
	uid, e1 := strconv.ParseUint(args[1], 10, 32)
	pid, e2 := strconv.Atoi(args[2])
	if e1 != nil || e2 != nil || uid < 501 || pid <= 1 {
		return errors.New("invalid session owner")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	c, err := inspectControl(args[0], false)
	if err != nil {
		return err
	}
	transparent := c.request.TransparentRouting
	c.close()
	if transparent {
		return transparentSession(ctx, args[0], uint32(uid), pid)
	}
	return companySessionForOwner(ctx, args[0], uint32(uid), pid)
}

// The approved local user and root only; do not grant access to the shared staff group.
func restrictServiceSocket(path string, uid uint32) error {
	if uid < 501 {
		return errors.New("invalid socket owner")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return os.Chown(path, int(uid), 0)
}
