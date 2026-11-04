package budget

import (
	"sync"
)

type Budget struct {
	mu                    sync.Mutex
	total, live           int
	global, providerLimit int
	accounts              map[int64]int
	providers             map[string]int
}
type Snapshot struct {
	Total int `json:"upstream_connections"`
	Live  int `json:"live_connections"`
	Limit int `json:"global_limit"`
}

func New(global, provider int) *Budget {
	return &Budget{global: global, providerLimit: provider, accounts: map[int64]int{}, providers: map[string]int{}}
}
func (b *Budget) SetLimits(global, provider int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.global = global
	b.providerLimit = provider
}
func (b *Budget) Reserve(account int64, provider string, limit int, live bool) (func(), bool) {
	return b.reserve(account, provider, limit, live, false)
}

// Replenishment is background traffic, but may coexist with live playback.
// All account, provider, and global ceilings still apply.
func (b *Budget) ReserveReplenishment(account int64, provider string, limit int) (func(), bool) {
	return b.reserve(account, provider, limit, false, true)
}
func (b *Budget) reserve(account int64, provider string, limit int, live, replenishment bool) (func(), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit < 1 || b.total >= b.global || b.accounts[account] >= limit || b.providers[provider] >= b.providerLimit || (!live && !replenishment && b.live > 0) {
		return nil, false
	}
	b.total++
	b.accounts[account]++
	b.providers[provider]++
	if live {
		b.live++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.total--
			b.accounts[account]--
			if b.accounts[account] == 0 {
				delete(b.accounts, account)
			}
			b.providers[provider]--
			if b.providers[provider] == 0 {
				delete(b.providers, provider)
			}
			if live {
				b.live--
			}
		})
	}, true
}
func (b *Budget) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Snapshot{b.total, b.live, b.global}
}

// WithProtectedAccounts excludes new reservations while a short queue-deletion
// transaction protects every account currently supplying playback.
func (b *Budget) WithProtectedAccounts(fn func([]int64) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := []int64{}
	for id, n := range b.accounts {
		if n > 0 {
			ids = append(ids, id)
		}
	}
	return fn(ids)
}

// AccountUsage is a snapshot, not a reservation. The serial checker subtracts
// its own metadata reservation before comparing provider-reported occupancy.
func (b *Budget) AccountUsage(id int64) int { b.mu.Lock(); defer b.mu.Unlock(); return b.accounts[id] }
