package company

import (
	"net/netip"
	"slices"
	"sync"
	"time"
)

// Each authenticated company session has its own resolver preference.
type dnsResolverPreference struct {
	mu             sync.Mutex
	backup         netip.Addr
	until          time.Time
	failedLookup   time.Time
	answeredLookup time.Time
}

func (p *dnsResolverPreference) order(servers []netip.Addr, now time.Time) []netip.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i := slices.Index(servers, p.backup); i > 0 && now.Before(p.until) {
		ordered := append([]netip.Addr{p.backup}, servers[:i]...)
		return append(ordered, servers[i+1:]...)
	}
	return servers
}

func (p *dnsResolverPreference) primaryAnswered(started time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if started.After(p.answeredLookup) {
		p.answeredLookup = started
	}
	if !started.Before(p.failedLookup) {
		p.until = time.Time{}
	}
}

func (p *dnsResolverPreference) useBackup(server netip.Addr, started, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Parallel lookups cannot extend the same cooldown indefinitely or
	// reapply it after a newer lookup has heard back from the primary.
	if now.Before(p.until) || !started.After(p.answeredLookup) || !started.After(p.failedLookup) {
		return
	}
	p.backup = server
	p.failedLookup = started
	p.until = now.Add(dnsPrimaryCooldown)
}
