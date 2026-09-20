package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const appGroupLabel = "GoConnect application routing label"
const appGroupPathLabel = appGroupLabel + " v1:"

// Store the path in the directory record, not a reversible assumption about its hash.
func appGroupDescription(path string) string {
	return appGroupPathLabel + base64.RawURLEncoding.EncodeToString([]byte(path))
}

type directoryRecord map[string][]string

func (r *directoryRecord) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*r = directoryRecord{}
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		case xml.StartElement:
			if t.Name.Local != "key" {
				return errors.New("unexpected directory attribute")
			}
			var key string
			if err = d.DecodeElement(&key, &t); err != nil {
				return err
			}
			var value struct {
				Strings []string `xml:"string"`
			}
			if err = d.Decode(&value); err != nil {
				return err
			}
			if _, duplicate := (*r)[key]; duplicate {
				return errors.New("duplicate directory attribute")
			}
			(*r)[key] = value.Strings
		}
	}
}
func (r directoryRecord) one(key string) string {
	values := r["dsAttrTypeStandard:"+key]
	if len(values) != 1 {
		return ""
	}
	return values[0]
}
func readAppGroupRecords() ([]directoryRecord, error) {
	data, err := systemCommand("/usr/bin/dscl", "-plist", ".", "-readall", "/Groups", "RecordName", "PrimaryGroupID", "RealName", "GroupMembership", "GroupMembers", "NestedGroups")
	if err != nil {
		return nil, err
	}
	var p struct {
		Records []directoryRecord `xml:"array>dict"`
	}
	err = xml.Unmarshal(data, &p)
	if err == nil && len(p.Records) == 0 {
		err = errors.New("empty directory snapshot")
	}
	return p.Records, err
}

func managedGroup(r directoryRecord, uid uint32) (name string, gid uint32, path string, ok bool) {
	name = r.one("RecordName")
	prefix := "goc_" + strconv.FormatUint(uint64(uid), 10) + "_"
	if !strings.HasPrefix(name, prefix) || len(strings.TrimPrefix(name, prefix)) != 16 {
		return
	}
	for _, ch := range strings.TrimPrefix(name, prefix) {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return
		}
	}
	n, err := strconv.ParseUint(r.one("PrimaryGroupID"), 10, 32)
	if err != nil || n < 61000 || n >= 65000 {
		return
	}
	gid = uint32(n)
	for _, key := range []string{"GroupMembership", "GroupMembers", "NestedGroups"} {
		if len(r["dsAttrTypeStandard:"+key]) != 0 {
			return
		}
	}
	label := r.one("RealName")
	if label == appGroupLabel {
		ok = true
		return
	} // Legacy: path unknown, only uninstall may collect it.
	if !strings.HasPrefix(label, appGroupPathLabel) {
		return
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(label, appGroupPathLabel))
	path = string(decoded)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || appGroupName(uid, path) != name {
		return
	}
	ok = true
	return
}

// Include supplementary, real and saved groups: descendants can still carry a label
// after the original app has exited. Callers hold captureLock through GC and launch.
func processGroupIDs() (map[uint32]bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	if len(processes) == 0 {
		return nil, errors.New("empty process snapshot")
	}
	used := map[uint32]bool{}
	for _, p := range processes {
		used[p.Eproc.Pcred.P_rgid] = true
		used[p.Eproc.Pcred.P_svgid] = true
		c := p.Eproc.Ucred
		if c.Ngroups < 1 || int(c.Ngroups) > len(c.Groups) {
			return nil, errors.New("incomplete process credentials")
		}
		for _, gid := range c.Groups[:int(c.Ngroups)] {
			used[gid] = true
		}
	}
	return used, nil
}

func configuredGroupPaths(uid uint32) (map[string]bool, error) {
	account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(account.HomeDir, "Library/Application Support/com.willhsu.GoConnect/configuration.json")
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Sys().(*syscall.Stat_t).Uid != uid || st.Size() > 4<<20 {
		return nil, errors.New("unsafe configuration snapshot")
	}
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, errors.New("configuration read failed")
	}
	return decodeConfiguredGroupPaths(data)
}
func decodeConfiguredGroupPaths(data []byte) (map[string]bool, error) {
	type app struct {
		Path string `json:"path"`
	}
	type connection struct {
		Apps     []app `json:"applications"`
		Excluded []app `json:"excludedApplications"`
	}
	var c struct {
		Version     int          `json:"schemaVersion"`
		Connections []connection `json:"connections"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.Version != 5 || len(c.Connections) == 0 {
		return nil, errors.New("unknown configuration schema")
	}
	result := map[string]bool{}
	for _, p := range c.Connections {
		for _, a := range append(p.Apps, p.Excluded...) {
			result[a.Path] = true
		}
	}
	return result, nil
}

// Best effort, fail closed on incomplete snapshots. Legacy pathless labels are
// adopted on next use; we never guess which missing app an opaque hash meant.
func collectAppGroups(uid uint32, current []string, uninstall bool) error {
	records, err := readAppGroupRecords()
	if err != nil {
		return err
	}
	return collectAppGroupRecords(uid, current, uninstall, records)
}

func collectAppGroupRecords(uid uint32, current []string, uninstall bool, records []directoryRecord) error {
	var err error
	protected := map[string]bool{}
	if !uninstall {
		protected, err = configuredGroupPaths(uid)
		if err != nil {
			return err
		}
	}
	for _, path := range current {
		protected[path] = true
	}
	counts := map[uint32]int{}
	for _, r := range records {
		n, e := strconv.ParseUint(r.one("PrimaryGroupID"), 10, 32)
		if e == nil {
			counts[uint32(n)]++
		}
	}
	// A directory user with this primary GID also keeps the label reserved.
	users, err := systemCommand("/usr/bin/dscl", ".", "-list", "/Users", "PrimaryGroupID")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(users), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			n, e := strconv.ParseUint(f[1], 10, 32)
			if e == nil {
				counts[uint32(n)]++
			}
		}
	}
	deleted := 0
	for _, r := range records {
		name, gid, path, ok := managedGroup(r, uid)
		if !ok || counts[gid] != 1 || protected[path] {
			continue
		}
		if !uninstall {
			if path == "" {
				continue
			}
			if _, e := os.Lstat(path); !os.IsNotExist(e) {
				continue
			}
		}
		// Refresh immediately before deletion, not once before a potentially long sweep.
		used, e := processGroupIDs()
		if e != nil {
			return e
		}
		if used[gid] {
			continue
		}
		// Detect directory changes since the initial snapshot before deleting anything.
		latest, e := systemCommand("/usr/bin/dscl", "-plist", ".", "-read", "/Groups/"+name)
		if e != nil {
			continue
		}
		var p struct {
			Record directoryRecord `xml:"dict"`
		}
		if xml.Unmarshal(latest, &p) != nil {
			continue
		}
		nn, gg, pp, valid := managedGroup(p.Record, uid)
		if !valid || nn != name || gg != gid || pp != path {
			continue
		}
		if _, e = systemCommand("/usr/sbin/dseditgroup", "-o", "delete", name); e != nil {
			return e
		}
		deleted++
		if !uninstall && deleted >= 32 {
			break
		} // Bound per-session maintenance work.
	}
	return nil
}

func adoptAppGroupPath(uid uint32, name, path string) error {
	data, err := systemCommand("/usr/bin/dscl", "-plist", ".", "-read", "/Groups/"+name)
	if err != nil {
		return err
	}
	var p struct {
		Record directoryRecord `xml:"dict"`
	}
	if err = xml.NewDecoder(bytes.NewReader(data)).Decode(&p); err != nil {
		return err
	}
	n, _, stored, ok := managedGroup(p.Record, uid)
	if !ok || n != name {
		return errors.New("unrecognized application routing group")
	}
	if stored == path {
		return nil
	}
	if stored != "" {
		return errors.New("application routing path conflict")
	}
	_, err = systemCommand("/usr/bin/dscl", ".", "-create", "/Groups/"+name, "RealName", appGroupDescription(path))
	return err
}
