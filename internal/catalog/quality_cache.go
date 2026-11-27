package catalog

import (
	"database/sql"
	"encoding/json"
	"strconv"
)

const expireQualityCacheSQL = `DELETE FROM settings WHERE name GLOB 'roster_cache_uhd_*' AND CASE WHEN json_valid(value) THEN coalesce(json_extract(value,'$.saved_at'),0) ELSE 0 END<=?`

type cachedRosterEntry struct {
	Channel                                  string `json:"channel"`
	Stream, Account, Admitted, Verified, Due int64
	Failures                                 int
}
type qualityRosterCache struct {
	SavedAt int64               `json:"saved_at"`
	Entries []cachedRosterEntry `json:"entries"`
}

func qualityCacheKey(uhd bool, profile ...string) string {
	key := "roster_cache_uhd_" + strconv.FormatBool(uhd)
	if len(profile) > 0 {
		key += ":" + profile[0]
	}
	return key
}
func (s *Store) saveQualityRoster(tx *sql.Tx, mode bool, now int64, profile ...string) error {
	cache := qualityRosterCache{SavedAt: now, Entries: []cachedRosterEntry{}}
	query := "SELECT r.channel_id,r.stream_id,r.account_id,r.admitted,r.verified,r.due,s.failures FROM roster r JOIN streams s ON s.id=r.stream_id"
	var args []any
	if len(profile) > 0 {
		query += " WHERE r.channel_id IN (SELECT id FROM channels WHERE profile_id=?)"
		args = append(args, profile[0])
	}
	rows, e := tx.Query(query+" ORDER BY r.verified DESC,r.stream_id", args...)
	if e != nil {
		return e
	}
	for rows.Next() {
		var v cachedRosterEntry
		if e = rows.Scan(&v.Channel, &v.Stream, &v.Account, &v.Admitted, &v.Verified, &v.Due, &v.Failures); e != nil {
			rows.Close()
			return e
		}
		cache.Entries = append(cache.Entries, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	raw, e := json.Marshal(cache)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO settings(name,value) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", qualityCacheKey(mode, profile...), string(raw))
	return e
}
func (s *Store) restoreQualityRoster(tx *sql.Tx, mode bool, now int64, profile ...string) error {
	var raw string
	e := tx.QueryRow("SELECT value FROM settings WHERE name=?", qualityCacheKey(mode, profile...)).Scan(&raw)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	var cache qualityRosterCache
	if json.Unmarshal([]byte(raw), &cache) != nil || cache.SavedAt > now || now-cache.SavedAt >= 3600 {
		return nil
	}
	// Snapshot order is newest verification first. The guarded insert applies the
	// current mappings, health, expiry, and UHD eligibility.
	for _, v := range cache.Entries {
		due := v.Verified // Schema-8 verification value; pass work owns scheduling.
		_, e = tx.Exec(`INSERT OR IGNORE INTO roster(channel_id,stream_id,account_id,admitted,verified,due)
 SELECT ?,s.id,s.account_id,?,?,? FROM streams s JOIN accounts a ON a.id=s.account_id JOIN mappings m ON m.stream_id=s.id JOIN channels c ON c.id=m.channel_id
 WHERE s.id=? AND s.account_id=? AND m.channel_id=? AND m.enabled=1 AND c.enabled=1 AND s.enabled=1 AND s.state='healthy' AND s.failures=? AND a.state IN ('healthy','unverified') AND (a.expires=0 OR a.expires>?) AND (? OR NOT is_uhd(s.name))
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.resource=s.id AND ((l.kind='cooldown' AND l.until>?) OR (l.kind='quality_invalid' AND l.until>=?)))`, v.Channel, v.Admitted, v.Verified, due, v.Stream, v.Account, v.Channel, v.Failures, now, mode, now, cache.SavedAt+3600)
		if e != nil {
			return e
		}
	}
	return nil
}
