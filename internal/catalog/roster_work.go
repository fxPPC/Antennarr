package catalog

import (
	"time"
)

type ChannelRoster struct {
	ChannelID string       `json:"channel_id"`
	Name      string       `json:"name"`
	Policy    RosterPolicy `json:"policy"`
	Override  RosterPolicy `json:"override"`
	Count     int          `json:"count"`
	Providers int          `json:"providers"`
}
type RosterSummary struct {
	RetiredDetails24  int             `json:"retired_details_24h"`
	RetiredMissing24  int             `json:"retired_details_missing_24h"`
	Retired24         int             `json:"accounts_retired_24h"`
	Channels          []ChannelRoster `json:"channels"`
	QueuedAccounts    int             `json:"queued_accounts"`
	QueuedStreams     int             `json:"queued_streams"`
	AwaitingDiscovery int             `json:"awaiting_discovery"`
	Ready             int             `json:"ready_streams"`
	Deficit           int             `json:"deficit"`
}

func (s *Store) RosterState() (RosterSummary, error) {
	out := RosterSummary{Channels: []ChannelRoster{}}
	chs, e := s.Channels()
	if e != nil {
		return out, e
	}
	for _, ch := range chs {
		if !ch.Enabled {
			continue
		}
		p, e := s.EffectivePolicy(ch.ID)
		if e != nil {
			return out, e
		}
		o, e := s.ChannelPolicy(ch.ID)
		if e != nil {
			return out, e
		}
		r := ChannelRoster{ChannelID: ch.ID, Name: ch.Name, Policy: p, Override: o}
		e = s.DB.QueryRow(`SELECT count(*),count(DISTINCT a.rate_key) FROM roster r JOIN accounts a ON a.id=r.account_id WHERE r.channel_id=?`, ch.ID).Scan(&r.Count, &r.Providers)
		if e != nil {
			return out, e
		}
		out.Ready += r.Count
		out.Deficit += max(0, p.Target-r.Count)
		out.Channels = append(out.Channels, r)
	}
	return out, nil
}

// Queue totals are for the Input page, not the serial worker's hot path.
func (s *Store) RosterSummary() (RosterSummary, error) {
	out, e := s.RosterState()
	if e != nil {
		return out, e
	}
	e = s.DB.QueryRow(`SELECT count(*) FROM accounts a WHERE state NOT IN ('inactive','retired','manually_blocked') AND NOT EXISTS(SELECT 1 FROM roster r WHERE r.account_id=a.id)`).Scan(&out.QueuedAccounts)
	if e != nil {
		return out, e
	}
	e = s.DB.QueryRow(`SELECT count(*) FROM streams s JOIN accounts a ON a.id=s.account_id WHERE a.state NOT IN ('inactive','retired','manually_blocked') AND s.state NOT IN ('retired','manually_blocked') AND s.enabled=1 AND NOT EXISTS(SELECT 1 FROM roster r WHERE r.stream_id=s.id) AND EXISTS(SELECT 1 FROM mappings m JOIN channels ch ON ch.id=m.channel_id WHERE m.stream_id=s.id AND m.enabled=1 AND ch.enabled=1)`).Scan(&out.QueuedStreams)
	if e != nil {
		return out, e
	}
	e = s.DB.QueryRow(`SELECT count(*) FROM accounts WHERE kind='xtream' AND catalogue_checked=0 AND state NOT IN ('inactive','retired','manually_blocked')`).Scan(&out.AwaitingDiscovery)
	if e != nil {
		return out, e
	}
	cutoff := time.Now().Unix()/60 - 1440
	// One SQL snapshot keeps totals and retained details consistent while the
	// checker deletes accounts concurrently. Both use the same minute cutoff.
	e = s.DB.QueryRow(`SELECT
 (SELECT coalesce(sum(CAST(value AS INTEGER)),0) FROM settings WHERE name GLOB 'accounts_retired_minute_*' AND CAST(substr(name,length('accounts_retired_minute_')+1) AS INTEGER)>?),
 (SELECT count(*) FROM events WHERE kind='account_removed' AND happened>=?)`, cutoff, (cutoff+1)*60).Scan(&out.Retired24, &out.RetiredDetails24)
	out.RetiredMissing24 = max(0, out.Retired24-out.RetiredDetails24)
	return out, e
}

// RosterObserved runs after a committed media result. A failure evicts immediately;
// successful probes/startup/playback qualify entries for admission while slots exist.
func (s *Store) RosterObserved(id int64, success bool) error {
	return s.rosterObserved(id, success, nil)
}
func (s *Store) rosterObserved(id int64, success bool, allowed map[string]bool) error {
	if !s.RosterEnabled() {
		return nil
	}
	if !success {
		tx, e := s.DB.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.Exec(`INSERT INTO events(happened,kind,channel_id,source_id,reason) SELECT ?,'roster_removed',channel_id,stream_id,'Media failed; replacement queued' FROM roster WHERE stream_id=?`, time.Now().Unix(), id); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE queue_positions SET position=random() & 9223372036854775807 WHERE kind='stream' AND source_id=?", id); e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO work_leases(kind,resource,until) VALUES('quality_invalid',?,?) ON CONFLICT(kind,resource) DO UPDATE SET until=max(until,excluded.until)", id, time.Now().Unix()+3600); e != nil {
			return e
		}
		if _, e = tx.Exec("DELETE FROM roster WHERE stream_id=?", id); e != nil {
			return e
		}
		return tx.Commit()
	}
	now := time.Now().Unix()
	rows, e := s.DB.Query(`SELECT m.channel_id FROM mappings m JOIN channels c ON c.id=m.channel_id WHERE m.stream_id=? AND m.enabled=1 AND c.enabled=1`, id)
	if e != nil {
		return e
	}
	var channels []string
	for rows.Next() {
		var ch string
		if e = rows.Scan(&ch); e != nil {
			rows.Close()
			return e
		}
		channels = append(channels, ch)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, ch := range channels {
		if allowed != nil && !allowed[ch] {
			continue
		}
		res, e := s.DB.Exec("UPDATE roster SET verified=?,due=? WHERE channel_id=? AND stream_id=?", now, now, ch, id)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			if e = s.admit(ch, id, now); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) QueueAccount(now int64, cache time.Duration) (Account, error) {
	var a Account
	tx, e := s.DB.Begin()
	if e != nil {
		return a, e
	}
	defer tx.Rollback()
	e = tx.QueryRow(`SELECT a.id,a.provider,a.kind,a.state,a.connection_limit,a.last_check FROM accounts a
 WHERE a.kind='xtream' AND a.state NOT IN('retired','manually_blocked') AND (a.state!='inactive' OR ?)
 AND (a.catalogue_checked<=? OR a.last_check=0 OR a.failures>0 OR a.external_connections>=a.connection_limit)
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.kind='account' AND l.resource=a.id AND l.until>?)
 AND NOT EXISTS(SELECT 1 FROM provider_rates p WHERE p.provider=a.rate_key AND max(p.next_api,p.blocked_until)>?)
 ORDER BY coalesce((SELECT position FROM queue_positions q WHERE q.kind='account' AND q.source_id=a.id),0),a.id LIMIT 1`, policyIn(tx).AccountFailures > 0, now-int64(cache.Seconds()), now, now*1000).Scan(&a.ID, &a.Provider, &a.Kind, &a.State, &a.Limit, &a.LastCheck)
	if e != nil {
		return a, e
	}
	if _, e = tx.Exec("UPDATE queue_positions SET position=random() & 9223372036854775807 WHERE kind='account' AND source_id=?", a.ID); e != nil {
		return a, e
	}
	return a, tx.Commit()
}
func (s *Store) RosterEntries(channel string) ([]map[string]any, error) {
	rows, e := s.DB.Query(`SELECT r.stream_id,r.account_id,a.provider,s.name,s.state,s.score,r.verified,r.due,s.codec,s.height FROM roster r JOIN streams s ON s.id=r.stream_id JOIN accounts a ON a.id=r.account_id WHERE r.channel_id=? ORDER BY r.verified DESC,r.stream_id`, channel)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, aid, last, due int64
		var provider, name, state, codec string
		var score float64
		var height int
		if e = rows.Scan(&id, &aid, &provider, &name, &state, &score, &last, &due, &codec, &height); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"stream_id": id, "account_id": aid, "provider": provider, "name": name, "state": state, "score": score, "verified": last, "due": due, "codec": codec, "height": height})
	}
	return out, rows.Err()
}
