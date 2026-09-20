package main

import (
	"errors"
	"fmt"
	"net/netip"
)

type routingNetworkState struct {
	Interface string
	Tailscale tailscaleBypass
	LAN       []localNetworkRoute
	DNS       []netip.Addr
}

func readRoutingNetwork(live bool) (routingNetworkState, error) {
	var s routingNetworkState
	var err error
	s.Interface, err = routeInterface(probeTarget)
	if err != nil {
		return s, err
	}
	s.Tailscale, err = discoverTailscale()
	if err != nil {
		return s, err
	}
	if live {
		s.LAN, err = discoverLocalNetworks()
		if err != nil {
			return s, err
		}
		s.DNS, err = discoverSystemDNS()
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

type networkPoller struct {
	baseline string
	failures int
	read     func() (routingNetworkState, error)
}

// A failed sample never publishes half of a new LAN/DNS/Tailscale snapshot.
// Three consecutive failed rounds end the session. A confirmed public route
// change is still fatal immediately, rather than tolerated as read jitter.
func (p *networkPoller) poll() (*routingNetworkState, error) {
	state, err := p.read()
	if state.Interface != "" && state.Interface != p.baseline {
		trace("network.interface_changed", 0)
		return nil, errors.New("original network changed")
	}
	if err != nil {
		p.failures++
		trace("network.poll_failed", int64(p.failures))
		if p.failures >= 3 {
			return nil, fmt.Errorf("network inspection failed three consecutive times: %w", err)
		}
		return nil, nil
	}
	if state.Interface != p.baseline {
		trace("network.interface_changed", 0)
		return nil, errors.New("original network changed")
	}
	if p.failures > 0 {
		trace("network.poll_recovered", 0)
	}
	p.failures = 0
	return &state, nil
}
