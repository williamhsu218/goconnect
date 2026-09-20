package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type routeCommand struct {
	AppPaths  []string `json:"appPaths,omitempty"`
	Sequence  uint64   `json:"sequence"`
	AppIndex  int      `json:"appIndex"`
	Action    string   `json:"action"`
	Network   string   `json:"network,omitempty"`
	Helper    bool     `json:"helper,omitempty"`
	LocalPort uint16   `json:"localPort,omitempty"`
	Target    string   `json:"target,omitempty"`
}
type routeCommandResult struct {
	Sequence uint64      `json:"sequence"`
	Success  bool        `json:"success"`
	Message  string      `json:"message,omitempty"`
	Probe    *probeReply `json:"probe,omitempty"`
}

func (c routeCommand) validate(apps int, probe bool) error {
	if len(c.AppPaths) != 0 {
		return errors.New("unexpected live policy")
	}
	if c.Sequence == 0 || c.AppIndex < 0 || c.AppIndex >= apps {
		return errors.New("invalid app command")
	}
	if c.Action == "launch" && !probe && c.Network == "" && !c.Helper && c.LocalPort == 0 && c.Target == "" {
		return nil
	}
	if c.Action == "probe" && probe && (c.Network == "tcp" || c.Network == "udp") && (c.LocalPort == 0 || (c.Network == "udp" && c.LocalPort >= 1024)) && (c.Target == "" || (c.Target == "tailscale-dns" && c.Network == "udp" && c.LocalPort == 0)) {
		return nil
	}
	return errors.New("unsupported app command")
}

func (c *controlFiles) handleCommand(ctx context.Context, runtime string) {
	data, _, err := readOwned(c.directory, "command.json", c.uid)
	if err != nil {
		return
	}
	var command routeCommand
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || command.Sequence <= c.lastCommand {
		return
	}
	c.lastCommand = command.Sequence
	result := &routeCommandResult{Sequence: command.Sequence}
	c.snapshot.CommandResult = result
	if err = command.validate(len(c.apps), c.request.ProbeOnly); err != nil {
		result.Message = "无效的应用启动请求。"
		return
	}
	app := c.apps[command.AppIndex]
	if command.Action == "launch" {
		err = launchCapturedApp(ctx, c.uid, app, runtime)
	} else {
		binary := app.Binary
		if command.Helper {
			binary = filepath.Join(app.Path, "Contents/Helpers/ProbeHelper")
		}
		resolved, e := filepath.EvalSymlinks(binary)
		if e != nil || resolved != binary {
			result.Message = "自检程序路径发生变化。"
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
		defer cancel()
		args := []string{"routing-probe", command.Network}
		if command.LocalPort != 0 || command.Target != "" {
			port := ""
			if command.LocalPort != 0 {
				port = strconv.Itoa(int(command.LocalPort))
			}
			args = append(args, "", port, command.Target)
		}
		cmd := exec.CommandContext(probeCtx, binary, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: c.uid, Gid: app.GID, Groups: []uint32{app.GID}}}
		var out []byte
		out, err = cmd.Output()
		if err == nil {
			var reply probeReply
			err = json.Unmarshal(out, &reply)
			if err == nil {
				result.Probe = &reply
			}
		}
	}
	result.Success = err == nil
	if err != nil {
		result.Message = err.Error()
	}
}

func (c *controlFiles) updateAppProcesses() {
	processes, err := processIdentities()
	if err != nil {
		return
	}
	for i, app := range c.apps {
		if i >= len(c.snapshot.Apps) {
			break
		}
		managed, unmanaged := appProcessCounts(app, c.uid, processes)
		if c.request.LiveRouting {
			managed, unmanaged = 0, 0
			for _, process := range processes {
				if process.UID == c.uid && configuredPathMatches(app.Path, process.Path) {
					managed++
				}
			}
		}
		c.snapshot.Apps[i].Managed = managed
		c.snapshot.Apps[i].Unmanaged = unmanaged
	}
}
