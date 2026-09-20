package main

// Deliberately record codes and counters only. Never redirect raw core output:
// it can contain subscription credentials, destinations and authorization data.
import (
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync/atomic"
	"time"
)

const diagnosticDirectory = serviceBase + "/diagnostics"
const diagnosticLimit = 1 << 20

var diagnosticCode = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)
var diagnostics *diagnosticLog

type diagnosticRecord struct {
	Time  string `json:"time"`
	PID   int    `json:"pid"`
	Event string `json:"event"`
	Value int64  `json:"value"`
}
type diagnosticLog struct {
	queue   chan diagnosticRecord
	stop    chan struct{}
	done    chan struct{}
	dropped atomic.Int64
}

func trace(code string, value int64) {
	if diagnostics != nil {
		diagnostics.record(code, value)
	}
}
func (l *diagnosticLog) record(code string, value int64) {
	if !diagnosticCode.MatchString(code) {
		return
	}
	record := diagnosticRecord{time.Now().UTC().Format(time.RFC3339Nano), os.Getpid(), code, value}
	select {
	case l.queue <- record:
	default:
		l.dropped.Add(1)
	}
}
func secureDiagnosticFile(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe diagnostic file")
	}
	return f, nil
}

// Fixed two-file rotation; callers never supply a user-controlled path or filename.
func appendDiagnostic(path string, data []byte, limit int64) error {
	if int64(len(data)) > limit {
		return errors.New("diagnostic record too large")
	}
	f, err := secureDiagnosticFile(path, unix.O_CREAT|unix.O_WRONLY|unix.O_APPEND)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if st.Size()+int64(len(data)) > limit {
		f.Close()
		if err = os.Rename(path, path+".1"); err != nil {
			return err
		}
		f, err = secureDiagnosticFile(path, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY)
		if err != nil {
			return err
		}
	}
	defer f.Close()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return err
}
func startDiagnosticLog(directory, name string) (*diagnosticLog, error) {
	lock, err := secureDiagnosticFile(filepath.Join(directory, name+".lock"), unix.O_CREAT|unix.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, err
	}
	l := &diagnosticLog{queue: make(chan diagnosticRecord, 128), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(l.done)
		defer lock.Close()
		path := filepath.Join(directory, name+".jsonl")
		write := func(r diagnosticRecord) {
			data, _ := json.Marshal(r)
			if appendDiagnostic(path, append(data, '\n'), diagnosticLimit) != nil {
				l.dropped.Add(1)
			}
		}
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case r := <-l.queue:
				write(r)
			case <-tick.C:
				write(diagnosticRecord{time.Now().UTC().Format(time.RFC3339Nano), os.Getpid(), "process.heartbeat", l.dropped.Swap(0)})
				write(diagnosticRecord{time.Now().UTC().Format(time.RFC3339Nano), os.Getpid(), "process.goroutines", int64(runtime.NumGoroutine())})
			case <-l.stop:
				for {
					select {
					case r := <-l.queue:
						write(r)
					default:
						return
					}
				}
			}
		}
	}()
	return l, nil
}
func (l *diagnosticLog) close() {
	close(l.stop)
	select {
	case <-l.done:
	case <-time.After(time.Second):
	}
}
func startRootDiagnostics(name string) func() {
	if os.Geteuid() != 0 || ensureRootDirectory(diagnosticDirectory) != nil {
		return func() {}
	}
	l, err := startDiagnosticLog(diagnosticDirectory, name)
	if err != nil {
		return func() {}
	}
	diagnostics = l
	trace("process.started", 900)
	return l.close
}

// Tail only fixed root-owned files. Read via the already-authenticated socket;
// the approved user gets diagnostic codes, never arbitrary root files.
func diagnosticTail(path string) string {
	f, err := secureDiagnosticFile(path, unix.O_RDONLY)
	if err != nil {
		return "unavailable"
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "unavailable"
	}
	offset := st.Size() - 128*1024
	if offset > 0 {
		_, _ = f.Seek(offset, io.SeekStart)
	}
	data, err := io.ReadAll(io.LimitReader(f, 128*1024))
	if err != nil {
		return "unavailable"
	}
	if offset > 0 {
		for i, c := range data {
			if c == '\n' {
				data = data[i+1:]
				break
			}
		}
	}
	return string(data)
}
func readDiagnosticLogs() map[string]string {
	result := make(map[string]string)
	if secureRootPath(diagnosticDirectory, true) != nil {
		return result
	}
	for _, name := range []string{"service.jsonl.1", "service.jsonl", "worker.jsonl.1", "worker.jsonl"} {
		result[name] = diagnosticTail(filepath.Join(diagnosticDirectory, name))
	}
	return result
}
func serviceDiagnostics() error {
	conn, err := serviceConnect()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if err = json.NewEncoder(conn).Encode(serviceRequest{Action: "diagnostics"}); err != nil {
		return err
	}
	var reply struct {
		State string            `json:"state"`
		Logs  map[string]string `json:"logs"`
	}
	if err = json.NewDecoder(io.LimitReader(conn, 2<<20)).Decode(&reply); err != nil {
		return err
	}
	if reply.State != "diagnostics" {
		return errors.New("diagnostics unavailable; service upgrade required")
	}
	emit(reply)
	return nil
}
