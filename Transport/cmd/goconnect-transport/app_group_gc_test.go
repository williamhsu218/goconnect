package main

import (
	"encoding/xml"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func fixtureRecord(uid uint32, path string) directoryRecord {
	return directoryRecord{
		"dsAttrTypeStandard:RecordName":     {appGroupName(uid, path)},
		"dsAttrTypeStandard:PrimaryGroupID": {"61999"},
		"dsAttrTypeStandard:RealName":       {appGroupDescription(path)},
	}
}
func TestManagedGroupRejectsForeignMutatedAndMemberRecords(t *testing.T) {
	for _, mutate := range []func(directoryRecord){
		func(r directoryRecord) { r["dsAttrTypeStandard:RealName"] = []string{"unrelated group"} },
		func(r directoryRecord) {
			r["dsAttrTypeStandard:RealName"] = []string{appGroupDescription("/Applications/Other.app")}
		},
		func(r directoryRecord) { r["dsAttrTypeStandard:PrimaryGroupID"] = []string{"20"} },
		func(r directoryRecord) {
			r["dsAttrTypeStandard:RecordName"] = append(r["dsAttrTypeStandard:RecordName"], "alias")
		},
		func(r directoryRecord) { r["dsAttrTypeStandard:GroupMembers"] = []string{"member-uuid"} },
		func(r directoryRecord) { r["dsAttrTypeStandard:NestedGroups"] = []string{"nested-group"} },
		func(r directoryRecord) { r["dsAttrTypeStandard:GroupMembership"] = []string{"user"} },
	} {
		r := fixtureRecord(501, "/Applications/Test.app")
		mutate(r)
		if _, _, _, ok := managedGroup(r, 501); ok {
			t.Fatal("accepted foreign or modified record", r)
		}
	}
	r := fixtureRecord(501, "/Applications/A\n\r\"字.app")
	if _, _, path, ok := managedGroup(r, 501); !ok || path != "/Applications/A\n\r\"字.app" {
		t.Fatal("path round trip failed")
	}
	if _, _, _, ok := managedGroup(r, 502); ok {
		t.Fatal("accepted another user's label")
	}
	r["dsAttrTypeStandard:RealName"] = []string{appGroupLabel}
	if _, _, path, ok := managedGroup(r, 501); !ok || path != "" {
		t.Fatal("legacy label must remain pathless")
	}
}
func TestDirectorySnapshotAndAllProfileProtection(t *testing.T) {
	records, err := readAppGroupRecords()
	if err != nil || len(records) == 0 {
		t.Fatal(err)
	}
	var dup struct {
		R directoryRecord `xml:"dict"`
	}
	if xml.Unmarshal([]byte(`<plist><dict><key>a</key><array/><key>a</key><array/></dict></plist>`), &dup) == nil {
		t.Fatal("accepted duplicate attribute")
	}
	paths, err := decodeConfiguredGroupPaths([]byte(`{"schemaVersion":5,"connections":[{"applications":[{"path":"/A.app"}]},{"excludedApplications":[{"path":"/B.app"}]}]}`))
	if err != nil || !paths["/A.app"] || !paths["/B.app"] {
		t.Fatal("lost other profile or direct app", err)
	}
	for _, data := range []string{`{}`, `{"schemaVersion":99,"connections":[{}]}`, `invalid`} {
		if _, err := decodeConfiguredGroupPaths([]byte(data)); err == nil {
			t.Fatal("GC must skip unknown config")
		}
	}
	if _, err := processGroupIDs(); err != nil {
		t.Fatal("cannot inspect process group reservations", err)
	}
}

// Explicit opt-in only. Run as root after build, never during ordinary swift/go tests.
// Creates one isolated group and a sleeping child; no VPN or user app is launched.
func TestRootRoutingGroupLifecycleAndSocketIsolation(t *testing.T) {
	raw := os.Getenv("GOCONNECT_ROOT_AUDIT_UID")
	if raw == "" || os.Geteuid() != 0 {
		t.Skip("requires explicit root audit")
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || n < 501 {
		t.Fatal("invalid audit UID")
	}
	uid := uint32(n)
	lock, err := captureLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	dir, err := os.MkdirTemp("/private/tmp", "goc-audit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "Removed.app")
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	name, gid, err := ensureAppGroup(uid, path)
	if err != nil {
		t.Fatal(err)
	}
	defer systemCommand("/usr/sbin/dseditgroup", "-o", "delete", name)
	collectFixture := func(current []string, uninstall bool) error {
		records, e := readAppGroupRecords()
		if e != nil {
			return e
		}
		var fixture []directoryRecord
		for _, r := range records {
			if r.one("RecordName") == name {
				fixture = append(fixture, r)
			}
		}
		return collectAppGroupRecords(uid, current, uninstall, fixture)
	}
	exists := func() bool { _, e := systemCommand("/usr/bin/dscl", ".", "-read", "/Groups/"+name); return e == nil }
	if err = collectFixture(nil, false); err != nil || !exists() {
		t.Fatal("deleted existing app", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = collectFixture([]string{path}, false); err != nil || !exists() {
		t.Fatal("deleted protected path", err)
	}
	child := exec.Command("/bin/sleep", "20")
	// Hold the label only in supplementary groups, not the primary GID.
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: 20, Groups: []uint32{20, gid}}}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	if err = collectFixture(nil, false); err != nil || !exists() {
		t.Fatal("deleted live supplementary group", err)
	}
	child.Process.Kill()
	child.Wait()
	if err = collectFixture(nil, false); err != nil || exists() {
		t.Fatal("unused removed app not collected", err)
	}
	// Test legacy adoption, then uninstall cleanup without needing a real uninstall.
	if _, err = systemCommand("/usr/sbin/dseditgroup", "-o", "create", "-i", fmt.Sprint(gid), "-r", appGroupLabel, name); err != nil {
		t.Fatal(err)
	}
	if err = collectFixture(nil, false); err != nil || !exists() {
		t.Fatal("guessed legacy path", err)
	}
	if _, _, err = ensureAppGroup(uid, path); err != nil {
		t.Fatal("legacy adoption", err)
	}
	if err = collectFixture(nil, false); err != nil || exists() {
		t.Fatal("adopted stale label not collected", err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ensureAppGroup(uid, path); err != nil {
		t.Fatal(err)
	}
	// Restrict uninstall fixture to this test's record: normal cleanup also preserves live labels.
	if err = collectFixture(nil, true); err != nil || exists() {
		t.Fatal("uninstall label cleanup", err)
	}
	socket := filepath.Join(dir, "s")
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = restrictServiceSocket(socket, uid); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(socket)
	if err != nil || st.Mode().Perm() != 0600 || st.Sys().(*syscall.Stat_t).Uid != uid {
		t.Fatal("socket permissions", err)
	}
	// A different real UID must be rejected by the filesystem before any protocol read.
	script := `import socket,sys
s=socket.socket(socket.AF_UNIX)
try: s.connect(sys.argv[1])
except PermissionError: sys.exit(0)
sys.exit(7)`
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(py, "-c", script, socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("foreign UID wasn't denied: %s %v", out, e)
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal("root denied", err)
	}
	conn.Close()
	cmd = exec.Command(py, "-c", `import socket,sys;s=socket.socket(socket.AF_UNIX);s.connect(sys.argv[1])`, socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: 20}}
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("approved UID denied: %s %v", out, e)
	}
}
