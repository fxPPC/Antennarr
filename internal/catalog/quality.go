package catalog

import (
	"database/sql"
	"database/sql/driver"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

// Preserve joined UHD labels such as ESPNUHD. Other resolution markers must
// be complete tokens so that channel names like 4Kids and 24Kitchen stay HD-eligible.
const qualityBoundary = `[^\p{L}\p{M}\p{N}]`

var resolutionMarker = regexp.MustCompile(`(?i)(?:^|` + qualityBoundary + `)(?:[48]k|2160[pi](?:[0-9]{2,3}(?:fps)?)?)(?:$|` + qualityBoundary + `)`)
var ultraHDLabel = regexp.MustCompile(`(?i)(^|` + qualityBoundary + `)ultra[\s_\p{Z}\p{Pd}]*hd($|` + qualityBoundary + `)`)

func IsUHD(name string) bool {
	name = norm.NFKC.String(html.UnescapeString(name))
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "UHD") ||
		(strings.Contains(upper, "ULTRA") && ultraHDLabel.MatchString(name)) ||
		(strings.ContainsAny(name, "248") && resolutionMarker.MatchString(name))
}

func init() {
	// Discovery sees incoming labels in Go. SQLite must apply the same rule to
	// previously imported streams, saved rosters, and cached assignments.
	sqlite.MustRegisterDeterministicScalarFunction("is_uhd", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		name, _ := args[0].(string)
		if IsUHD(name) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}

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
		if _, e = tx.Exec("DELETE FROM roster WHERE stream_id IN (SELECT id FROM streams WHERE is_uhd(name))"); e != nil {
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
	return " AND (NOT is_uhd(s.name) OR EXISTS (SELECT 1 FROM mappings qm JOIN active_channels c ON c.id=qm.channel_id JOIN output_profiles p ON p.id=c.profile_id WHERE qm.stream_id=s.id AND qm.enabled=1 AND c.enabled=1 AND json_extract(p.extensions,'$.preferred_height')=2160))"
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

func (s *Store) PreferredHeight(channel string) int {
	var height int
	_ = s.DB.QueryRow("SELECT coalesce(json_extract(p.extensions,'$.preferred_height'),0) FROM output_profiles p JOIN channels c ON c.profile_id=p.id WHERE c.id=?", channel).Scan(&height)
	return height
}

// Copy the old shared UHD preference once. A restart must preserve later tuner edits.
func (s *Store) MigratePlaybackQuality(legacyUHD bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("INSERT OR IGNORE INTO settings(name,value) VALUES('tuner_quality_migrated','1')")
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 0 && legacyUHD {
		if _, err = tx.Exec("UPDATE output_profiles SET extensions=json_set(extensions,'$.preferred_height',2160)"); err != nil {
			return err
		}
	}
	// Older catalogues can have 4K entries counted towards an HD tuner's target.
	// Drop only the ineligible memberships and keep their sources and health history.
	if _, err = tx.Exec(`DELETE FROM roster WHERE EXISTS (
  SELECT 1 FROM streams s JOIN channels c ON c.id=roster.channel_id JOIN output_profiles p ON p.id=c.profile_id
  WHERE s.id=roster.stream_id AND is_uhd(s.name) AND coalesce(json_extract(p.extensions,'$.preferred_height'),0)<>2160)`); err != nil {
		return err
	}
	return tx.Commit()
}
