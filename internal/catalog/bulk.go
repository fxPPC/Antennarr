package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Legacy bulk operations retain their original primary-tuner scope.
func (s *Store) ReplaceChannels(defaultUS bool) error {
	preset := ""
	if defaultUS {
		preset = "us-nyc"
	}
	return s.ReplaceProfileChannels("primary", preset)
}

// ReplaceProfileChannels replaces one tuner only. Retain matching channel IDs
// during an explicit preset reset, so unrelated outputs and their rosters survive.
func (s *Store) ReplaceProfileChannels(profile, preset string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var present int
	if e = tx.QueryRow("SELECT count(*) FROM output_profiles WHERE id=?", profile).Scan(&present); e != nil {
		return e
	}
	if present != 1 {
		return errors.New("tuner not found")
	}
	old := map[string]string{}
	rows, e := tx.Query("SELECT id,name,region FROM channels WHERE profile_id=?", profile)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id, name, region string
		if e = rows.Scan(&id, &name, &region); e != nil {
			rows.Close()
			return e
		}
		key, _ := channelKey(name, region)
		old[key] = id
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if e = clearProfileChannels(tx, profile); e != nil {
		return e
	}
	if preset != "" {
		chs, e := presetChannels(preset)
		if e != nil {
			return e
		}
		for _, c := range chs {
			key, _ := channelKey(c.Name, c.Region)
			c.ID = old[key]
			if c.ID == "" {
				c.ID = profile + "-default-" + c.Number
			}
			c.ProfileID = profile
			c.Enabled = true
			if e = insertProfileChannel(tx, c); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("UPDATE accounts SET catalogue_checked=0 WHERE kind='xtream' AND state NOT IN ('retired','manually_blocked')"); e != nil {
			return e
		}
	}
	// Removing a channel can resolve an ambiguity in another tuner's saved list.
	if e = autoMap(tx); e != nil {
		return e
	}
	return tx.Commit()
}

func clearProfileChannels(tx *sql.Tx, profile string) error {
	for _, q := range []string{
		"DELETE FROM settings WHERE name IN(SELECT 'channel_station:'||id FROM channels WHERE profile_id=?)",
		"DELETE FROM channel_aliases WHERE channel_id IN(SELECT id FROM channels WHERE profile_id=?)",
		"DELETE FROM channels WHERE profile_id=?",
	} {
		if _, e := tx.Exec(q, profile); e != nil {
			return e
		}
	}
	// Cached quality-mode memberships use JSON references, so remove just this
	// tuner's deleted channel IDs rather than flushing another tuner's snapshot.
	for _, mode := range []bool{false, true} {
		var raw string
		e := tx.QueryRow("SELECT value FROM settings WHERE name=?", qualityCacheKey(mode)).Scan(&raw)
		if e == sql.ErrNoRows {
			continue
		}
		if e != nil {
			return e
		}
		var cache qualityRosterCache
		if json.Unmarshal([]byte(raw), &cache) != nil {
			continue
		}
		kept := cache.Entries[:0]
		for _, v := range cache.Entries {
			var n int
			if e = tx.QueryRow("SELECT count(*) FROM channels WHERE id=?", v.Channel).Scan(&n); e != nil {
				return e
			}
			if n > 0 {
				kept = append(kept, v)
			}
		}
		cache.Entries = kept
		b, e := json.Marshal(cache)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE settings SET value=? WHERE name=?", string(b), qualityCacheKey(mode)); e != nil {
			return e
		}
	}
	// Pass snapshots reference stream IDs shared by all outputs. Drop only work
	// with no remaining active membership; another tuner's progress is retained.
	_, e := tx.Exec("DELETE FROM roster_pass_work WHERE NOT EXISTS(SELECT 1 FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 WHERE r.stream_id=roster_pass_work.stream_id)")
	return e
}
