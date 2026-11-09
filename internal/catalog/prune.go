package catalog

import (
	"database/sql"
	"encoding/json"
	"time"
)

type CleanupResult struct {
	Accounts int64 `json:"accounts"`
	Streams  int64 `json:"streams"`
}

// PruneCatalogue permanently removes retired sources and examined accounts with
// no remaining channel mappings. An unexamined Xtream account is still queued,
// not proven useless. Matching ignores UHD mode and channel enablement so a
// temporary output preference does not destroy the user's reserves.
// The caller holds the connection-budget reservation lock and supplies in-use
// account IDs; persistent check leases provide additional protection.
func (s *Store) PruneCatalogue(protected []int64) (CleanupResult, error) {
	var out CleanupResult
	tx, err := s.DB.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("CREATE TEMP TABLE prune_protected(id INTEGER PRIMARY KEY)"); err != nil {
		return out, err
	}
	for _, id := range protected {
		if _, err = tx.Exec("INSERT OR IGNORE INTO prune_protected VALUES(?)", id); err != nil {
			return out, err
		}
	}
	now := time.Now().Unix()
	// Bound saved alternatives to three roster targets per channel/quality class (at least 60).
	// Rank providers first, then their strongest/recent streams, to retain diversity.
	// Accounts remain import-queue records and can rediscover discarded candidates.
	target := 30
	if policy := s.rosterPolicy.Load(); policy != nil {
		target = policy.Target
	}
	if _, err = tx.Exec(`CREATE TEMP TABLE reserve_keep AS
 WITH provider_rank AS (
 SELECT m.channel_id,s.id,a.rate_key,s.score,s.last_success,(instr(upper(s.name),'UHD')>0) quality,
 row_number() OVER(PARTITION BY m.channel_id,(instr(upper(s.name),'UHD')>0),a.rate_key ORDER BY s.score DESC,s.last_success DESC,s.id) provider_position
 FROM mappings m JOIN streams s ON s.id=m.stream_id JOIN accounts a ON a.id=s.account_id
 WHERE m.origin!='manual' AND m.enabled=1 AND s.enabled=1 AND s.state NOT IN ('retired','manually_blocked')),
 ranked AS (SELECT *,row_number() OVER(PARTITION BY channel_id,quality ORDER BY provider_position,score DESC,last_success DESC,id) position FROM provider_rank)
 SELECT DISTINCT r.id FROM ranked r LEFT JOIN channel_policies p ON p.channel_id=r.channel_id
 WHERE r.position<=max(60,3*coalesce(nullif(p.target,0),?))`, target); err != nil {
		return out, err
	}
	if _, err = tx.Exec("CREATE UNIQUE INDEX reserve_keep_id ON reserve_keep(id)"); err != nil {
		return out, err
	}
	if _, err = tx.Exec(`CREATE TEMP TABLE excess_streams AS SELECT s.id FROM streams s
 WHERE NOT EXISTS(SELECT 1 FROM reserve_keep k WHERE k.id=s.id)
 AND NOT EXISTS(SELECT 1 FROM roster r WHERE r.stream_id=s.id)
 AND NOT EXISTS(SELECT 1 FROM mappings m WHERE m.stream_id=s.id AND m.origin='manual')
 AND NOT EXISTS(SELECT 1 FROM prune_protected p WHERE p.id=s.account_id)
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.until>? AND ((l.kind='account' AND l.resource=s.account_id) OR(l.kind='stream' AND l.resource=s.id)))`, now); err != nil {
		return out, err
	}
	if _, err = tx.Exec("CREATE UNIQUE INDEX excess_streams_id ON excess_streams(id)"); err != nil {
		return out, err
	}
	// Mark only accounts losing their last candidate for rediscovery; do not retire
	// an authorised account simply because its cached catalogue was trimmed.
	if _, err = tx.Exec(`UPDATE accounts SET catalogue_checked=0 WHERE kind='xtream' AND id IN(SELECT account_id FROM streams WHERE id IN(SELECT id FROM excess_streams)) AND NOT EXISTS(SELECT 1 FROM streams s WHERE s.account_id=accounts.id AND s.id NOT IN(SELECT id FROM excess_streams))`); err != nil {
		return out, err
	}

	_, err = tx.Exec(`CREATE TEMP TABLE prune_accounts AS SELECT a.id FROM accounts a
 WHERE NOT EXISTS(SELECT 1 FROM prune_protected p WHERE p.id=a.id)
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.kind='account' AND l.resource=a.id AND l.until>?)
 AND NOT EXISTS(SELECT 1 FROM streams s JOIN work_leases l ON l.resource=s.id WHERE s.account_id=a.id AND l.kind='stream' AND l.until>?)
 AND (a.state='retired' OR ((a.kind!='xtream' OR a.catalogue_checked>0) AND EXISTS(SELECT 1 FROM channels)
 AND NOT EXISTS(SELECT 1 FROM streams s JOIN mappings m ON m.stream_id=s.id JOIN channels c ON c.id=m.channel_id WHERE s.account_id=a.id AND s.state!='retired')))`, now, now)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(`CREATE TEMP TABLE prune_streams AS SELECT s.id FROM streams s WHERE s.account_id IN(SELECT id FROM prune_accounts)
 OR s.id IN(SELECT id FROM excess_streams) OR (s.state='retired' AND NOT EXISTS(SELECT 1 FROM prune_protected p WHERE p.id=s.account_id)
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.kind='account' AND l.resource=s.account_id AND l.until>?)
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.kind='stream' AND l.resource=s.id AND l.until>?))`, now, now)
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow("SELECT count(*) FROM prune_accounts").Scan(&out.Accounts); err != nil {
		return out, err
	}
	if err = tx.QueryRow("SELECT count(*) FROM prune_streams").Scan(&out.Streams); err != nil {
		return out, err
	}
	if out.Accounts+out.Streams > 0 {
		if err = preserveHighwater(tx); err != nil {
			return out, err
		}
	}
	if err = recordCatalogueRemovals(tx, "", true); err != nil {
		return out, err
	}
	for _, q := range []string{
		`DELETE FROM validations WHERE stream_id IN(SELECT id FROM prune_streams)`,
		`DELETE FROM mappings WHERE stream_id IN(SELECT id FROM prune_streams)`,
		`DELETE FROM events WHERE source_id IN(SELECT id FROM prune_streams)`,
		`DELETE FROM streams WHERE id IN(SELECT id FROM prune_streams)`,
		`DELETE FROM provenance WHERE account_id IN(SELECT id FROM prune_accounts)`,
		`DELETE FROM accounts WHERE id IN(SELECT id FROM prune_accounts)`,
		`DELETE FROM work_leases WHERE (kind='account' AND NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=resource)) OR (kind IN('stream','cooldown','deferred','quality_invalid') AND NOT EXISTS(SELECT 1 FROM streams s WHERE s.id=resource))`,
		`DELETE FROM provider_rates WHERE provider!='*' AND NOT EXISTS(SELECT 1 FROM accounts a WHERE a.rate_key=provider_rates.provider)`,
	} {
		if _, err = tx.Exec(q); err != nil {
			return out, err
		}
	}
	// Cached UHD rosters have JSON references rather than foreign keys. Filter
	// dangling entries too, retaining the valid entries and their original age.
	channels := map[string]bool{}
	channelRows, e := tx.Query("SELECT id FROM channels")
	if e != nil {
		return out, e
	}
	for channelRows.Next() {
		var id string
		if e = channelRows.Scan(&id); e != nil {
			channelRows.Close()
			return out, e
		}
		channels[id] = true
	}
	e = channelRows.Err()
	channelRows.Close()
	if e != nil {
		return out, e
	}
	for _, mode := range []bool{false, true} {
		var raw string
		if err = tx.QueryRow("SELECT value FROM settings WHERE name=?", qualityCacheKey(mode)).Scan(&raw); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return out, err
		}
		var cache qualityRosterCache
		if json.Unmarshal([]byte(raw), &cache) != nil {
			if _, err = tx.Exec("DELETE FROM settings WHERE name=?", qualityCacheKey(mode)); err != nil {
				return out, err
			}
			continue
		}
		rows, e := tx.Query(`SELECT s.id,s.account_id FROM streams s JOIN accounts a ON a.id=s.account_id WHERE s.id IN(SELECT json_extract(value,'$.Stream') FROM json_each(?,'$.entries'))`, raw)
		if e != nil {
			return out, e
		}
		valid := map[int64]int64{}
		for rows.Next() {
			var id, account int64
			if e = rows.Scan(&id, &account); e != nil {
				rows.Close()
				return out, e
			}
			valid[id] = account
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		kept := cache.Entries[:0]
		for _, v := range cache.Entries {
			if a, ok := valid[v.Stream]; ok && a == v.Account && channels[v.Channel] {
				kept = append(kept, v)
			}
		}
		if len(kept) != len(cache.Entries) {
			cache.Entries = kept
			b, e := json.Marshal(cache)
			if e != nil {
				return out, e
			}
			if _, err = tx.Exec("UPDATE settings SET value=? WHERE name=?", string(b), qualityCacheKey(mode)); err != nil {
				return out, err
			}
		}
	}
	for _, q := range []string{"DROP TABLE prune_streams", "DROP TABLE prune_accounts", "DROP TABLE prune_protected", "DROP TABLE reserve_keep", "DROP TABLE excess_streams"} {
		if _, err = tx.Exec(q); err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}
