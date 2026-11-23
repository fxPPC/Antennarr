// Package access bounds administration traffic without throttling private tuners.
package access

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type allowance struct {
	credit float64
	at     time.Time
}

type entry struct {
	failures                                    int
	blocked, last, heavyAt, backupAt, restoreAt time.Time
	api, assets                                 allowance
}
type Guard struct {
	mu      sync.Mutex
	peers   []netip.Prefix
	entries map[string]*entry
	busy    bool
	now     func() time.Time
}

func New(proxies string) (*Guard, error) {
	g := &Guard{entries: map[string]*entry{}, now: time.Now}
	for _, raw := range strings.Split(proxies, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, e := netip.ParsePrefix(raw)
		if e != nil {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy")
			}
			p = netip.PrefixFrom(ip, ip.BitLen())
		}
		g.peers = append(g.peers, p)
	}
	return g, nil
}
func (g *Guard) trusted(ip netip.Addr) bool {
	for _, p := range g.peers {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
func (g *Guard) Key(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return "unknown"
	}
	ip = ip.Unmap()
	// Walk from the known peer towards the client. A client-supplied leftmost
	// X-Forwarded-For value cannot replace the actual address appended by NPM.
	if g.trusted(ip) {
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0 && g.trusted(ip); i-- {
			candidate, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if err != nil {
				break
			}
			ip = candidate.Unmap()
		}
	}
	return ip.String()
}
func (g *Guard) get(key string, now time.Time) *entry {
	v := g.entries[key]
	if v == nil {
		if len(g.entries) >= 4096 {
			for k := range g.entries {
				delete(g.entries, k)
				break
			}
		}
		initial := allowance{credit: 60, at: now}
		v = &entry{api: initial, assets: initial}
		g.entries[key] = v
	}
	if now.Sub(v.last) > 30*time.Minute {
		v.failures = 0
		v.blocked = time.Time{}
	}
	v.last = now
	return v
}

// Failure returns a cooldown for repeated invalid credentials. Correct
// credentials are always checked and can clear a cooldown, avoiding lockout.
func (g *Guard) Failure(key string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	v := g.get(key, now)
	if now.Before(v.blocked) {
		return v.blocked.Sub(now)
	}
	v.failures++
	if v.failures >= 5 {
		delay := min(5*time.Minute, 2*time.Second*time.Duration(1<<min(v.failures-5, 8)))
		v.blocked = now.Add(delay)
		return delay
	}
	return 0
}
func (g *Guard) Success(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	v := g.get(key, g.now())
	v.failures = 0
	v.blocked = time.Time{}
}
func (g *Guard) Allow(key string, dashboardAsset bool) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	v := g.get(key, now)
	budget := &v.api
	if dashboardAsset {
		budget = &v.assets
	}
	budget.credit = math.Min(60, budget.credit+now.Sub(budget.at).Seconds()*10)
	budget.at = now
	if budget.credit < 1 {
		return 100 * time.Millisecond
	}
	budget.credit--
	return 0
}

// Cost admits one expensive operation globally, plus per-client spacing. It
// returns immediately instead of queueing work that could compete with TV.
func (g *Guard) Cost(key, path, method string) (func(), time.Duration) {
	noop := func() {}
	if method != "POST" {
		return noop, 0
	}
	backup := path == "/api/backup" || path == "/api/restore" || path == "/api/input/clear"
	heavy := path == "/api/import" || (strings.HasPrefix(path, "/api/accounts/") && strings.HasSuffix(path, "/refresh"))
	if !backup && !heavy {
		return noop, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	v := g.get(key, now)
	next := v.heavyAt
	spacing := 30 * time.Second
	if backup {
		next = v.backupAt
		spacing = time.Minute
		if path == "/api/restore" {
			next = v.restoreAt
		}
	}
	if now.Before(next) {
		return noop, next.Sub(now)
	}
	if g.busy {
		return noop, 5 * time.Second
	}
	if backup {
		if path == "/api/restore" {
			v.restoreAt = now.Add(spacing)
		} else {
			v.backupAt = now.Add(spacing)
		}
	} else {
		v.heavyAt = now.Add(spacing)
	}
	g.busy = true
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); g.busy = false; g.mu.Unlock() }) }, 0
}
func Limited(w http.ResponseWriter, wait time.Duration) {
	seconds := max(1, int(math.Ceil(wait.Seconds())))
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	http.Error(w, fmt.Sprintf("Too many requests; try again in %d seconds", seconds), http.StatusTooManyRequests)
}
