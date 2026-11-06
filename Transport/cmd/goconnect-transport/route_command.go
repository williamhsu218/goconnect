package main

import (
	"errors"
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
