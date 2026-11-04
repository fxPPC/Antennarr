package catalog

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"
)

func IsUHD(name string) bool      { return strings.Contains(strings.ToUpper(name), "UHD") }
func (s *Store) UHDAllowed() bool { return s.allowUHD.Load() }

// Persist the previous choice separately so restart or an unrelated settings save
// does not rebuild again. A toggle caches the departing roster for one hour.
func (s *Store) SetUHDAllowed(allowed bool) error {
	return s.setUHDAllowedAt(allowed, time.Now().Unix())
}
func (s *Store) setUHDAllowedAt(allowed bool, now int64) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old string
	e = tx.QueryRow("SELECT value FROM settings WHERE name='allow_uhd'").Scan(&old)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	changed := (old == "true") != allowed
	if changed {
		if e = s.saveQualityRoster(tx, old == "true", now); e != nil {
			return e
		}
		if _, e = tx.Exec("DELETE FROM roster"); e != nil {
			return e
		}
		if e = s.restoreQualityRoster(tx, allowed, now); e != nil {
			return e
		}
		if _, e = tx.Exec(`INSERT INTO events(happened,kind,channel_id,reason) SELECT ?,CASE WHEN EXISTS(SELECT 1 FROM roster r WHERE r.channel_id=c.id) THEN 'roster_restored' ELSE 'roster_rebuild' END,c.id,CASE WHEN EXISTS(SELECT 1 FROM roster r WHERE r.channel_id=c.id) THEN 'Restored mode roster cached less than one hour ago; missing places will refill' ELSE 'No eligible recent mode roster; rebuilding verified roster' END FROM channels c WHERE enabled=1`, now); e != nil {
			return e
		}
	} else if !allowed {
		if _, e = tx.Exec("DELETE FROM roster WHERE stream_id IN (SELECT id FROM streams WHERE instr(upper(name),'UHD')>0)"); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(expireQualityCacheSQL, now-3600); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO settings(name,value) VALUES('allow_uhd',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", strconv.FormatBool(allowed)); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.allowUHD.Store(allowed)
	return nil
}
func (s *Store) qualitySQL() string {
	if s.UHDAllowed() {
		return ""
	}
	return " AND instr(upper(s.name),'UHD')=0"
}

// Order within each quality class by freshness, then spread distinct canonical
// hostnames before taking additional accounts from a hostname already represented.
func OrderCandidates(in []Candidate, uhd, diversity bool) []Candidate {
	out := append([]Candidate(nil), in...)
	if uhd {
		sort.SliceStable(out, func(i, j int) bool { return out[i].UHD && !out[j].UHD })
	}
	if !diversity {
		return out
	}
	result := make([]Candidate, 0, len(out))
	for _, class := range []bool{true, false} {
		seen := map[string]bool{}
		duplicates := []Candidate{}
		for _, c := range out {
			if c.UHD != class {
				continue
			}
			host := ProviderKey(c.Provider)
			if seen[host] {
				duplicates = append(duplicates, c)
			} else {
				seen[host] = true
				result = append(result, c)
			}
		}
		result = append(result, duplicates...)
	}
	return result
}
