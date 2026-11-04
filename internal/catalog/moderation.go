package catalog

import (
	"database/sql"
	"net/url"
	"strings"
	"time"
)

func ProviderKey(provider string) string {
	raw := provider
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, e := url.Parse(raw)
	if e == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	return strings.ToLower(provider)
}

func (s *Store) CatalogueChecked(account, now int64) error {
	_, e := s.DB.Exec("UPDATE accounts SET catalogue_checked=?,failures=0,state='healthy',next_check=0 WHERE id=? AND state NOT IN ('retired','manually_blocked')", now, account)
	return e
}

// ReserveCheck atomically persists pacing across restarts and manual checks.
// Timestamps are milliseconds. A future result means no slot was consumed.
func (s *Store) ReserveCheck(provider, kind string, now int64, globalGap, providerGap time.Duration) (int64, error) {
	provider = ProviderKey(provider)
	tx, e := s.DB.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	realClock := now == 0
	if realClock {
		now = time.Now().UnixMilli()
	}
	for _, key := range []string{"*", provider} {
		if _, e = tx.Exec("INSERT OR IGNORE INTO provider_rates(provider) VALUES(?)", key); e != nil {
			return 0, e
		}
	}
	column := "next_api"
	if kind == "probe" {
		column = "next_probe"
	}
	var ready int64
	e = tx.QueryRow("SELECT max("+column+",blocked_until,(SELECT "+column+" FROM provider_rates WHERE provider='*')) FROM provider_rates WHERE provider=?", provider).Scan(&ready)
	if e != nil {
		return 0, e
	}
	if ready > now {
		return ready, nil
	}
	if _, e = tx.Exec("UPDATE provider_rates SET "+column+"=? WHERE provider='*'", now+globalGap.Milliseconds()); e != nil {
		return 0, e
	}
	if _, e = tx.Exec("UPDATE provider_rates SET "+column+"=? WHERE provider=?", now+providerGap.Milliseconds(), provider); e != nil {
		return 0, e
	}
	if realClock {
		return 0, tx.Commit()
	}
	return now, tx.Commit()
}
func (s *Store) ProviderDeferred(provider string, now int64) bool {
	var until int64
	err := s.DB.QueryRow("SELECT blocked_until FROM provider_rates WHERE provider=?", ProviderKey(provider)).Scan(&until)
	if err != nil && err != sql.ErrNoRows {
		return true
	} // A database error must not bypass a provider cooldown.
	return until > now
}
func (s *Store) ProviderTrouble(provider string, now, retryUntil int64, rateLimited bool) error {
	key := ProviderKey(provider)
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT OR IGNORE INTO provider_rates(provider) VALUES(?)", key); e != nil {
		return e
	}
	var failures int
	if e = tx.QueryRow("SELECT failures FROM provider_rates WHERE provider=?", key).Scan(&failures); e != nil {
		return e
	}
	base := 5 * time.Minute
	if rateLimited {
		base = 30 * time.Minute
	}
	for i := 0; i < failures && base < 24*time.Hour; i++ {
		base *= 2
	}
	if base > 24*time.Hour {
		base = 24 * time.Hour
	}
	until := now + base.Milliseconds()
	if retryUntil > until {
		until = retryUntil
	}
	_, e = tx.Exec("UPDATE provider_rates SET blocked_until=max(blocked_until,?),failures=failures+1 WHERE provider=?", until, key)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ProviderResponsive(provider string) error {
	_, e := s.DB.Exec("UPDATE provider_rates SET failures=0 WHERE provider=?", ProviderKey(provider))
	return e
}
