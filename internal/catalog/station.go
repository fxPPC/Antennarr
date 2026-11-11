package catalog

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
)

// Optional channel metadata uses the schema-8 settings namespace. It travels
// with full backups and lineup exports, without rewriting existing lineups.
func stationSetting(id string) string  { return "channel_station:" + id }
func normaliseStation(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

var stationPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,31}$`)

func validStation(s string) bool { return s == "" || stationPattern.MatchString(s) }
func saveStation(tx *sql.Tx, id, station string) error {
	station = normaliseStation(station)
	if station == "" {
		_, err := tx.Exec("DELETE FROM settings WHERE name=?", stationSetting(id))
		return err
	}
	_, err := tx.Exec("INSERT INTO settings(name,value) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", stationSetting(id), station)
	return err
}

func readStation(q interface{ QueryRow(string, ...any) *sql.Row }, id string) (string, error) {
	var station string
	err := q.QueryRow("SELECT value FROM settings WHERE name=?", stationSetting(id)).Scan(&station)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return station, err
}

// One read path keeps explicit assignments and automatic matching on the same
// station rules. Callers close rows before issuing writes on the transaction.
func readChannelMatchers(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) ([]channelMatcher, error) {
	rows, err := q.Query(`SELECT c.id,c.number,c.name,c.region,c.enabled,c.aliases,coalesce((SELECT value FROM settings WHERE name='channel_station:'||c.id),'') FROM channels c`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channelMatcher
	for rows.Next() {
		var c Channel
		var raw string
		if err := rows.Scan(&c.ID, &c.Number, &c.Name, &c.Region, &c.Enabled, &raw, &c.Station); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c.Aliases); err != nil {
			return nil, err
		}
		out = append(out, compileChannel(c))
	}
	return out, rows.Err()
}

func compactIdentity(s string) string { return strings.Join(nameWords(s), "") }

// Station evidence must be a whole token (WNBC), or the exact station/network
// combination used by guide IDs (nbcwnbc.us). Substrings such as WNBC2 do not
// qualify. Country/quality decoration has already been removed by channelKey.
func (m channelMatcher) stationEvidence(key string) bool {
	station := m.stationKey
	if station == "" {
		return true
	}
	for _, token := range strings.Fields(key) {
		if strings.ReplaceAll(token, " ", "") == station {
			return true
		}
	}
	compact := strings.ReplaceAll(key, " ", "")
	if compact == station {
		return true
	}
	network := m.networkKey
	return compact == network+station || compact == station+network
}
func (m channelMatcher) stationMatches(label sourceLabel) bool {
	if m.channel.Station == "" {
		return true
	}
	// A recognised country conflict is never cured by a matching callsign.
	if !m.compatible(label) {
		return false
	}
	if label.guide.key != "" && label.guide.country != "" && label.country != "" && label.guide.country != label.country {
		return false
	}
	return m.stationEvidence(label.key) || (m.compatible(sourceLabel{channelLabel: label.guide}) && m.stationEvidence(label.guide.key))
}

// Only exact normalised guide identities count. Numeric/opaque provider IDs
// remain usable, and a prefix such as NBC must never match CNBC or NBC News.
func (m channelMatcher) identifiesGuide(guide channelLabel) bool {
	if guide.key == "" || !m.compatible(sourceLabel{channelLabel: guide}) {
		return false
	}
	if m.guideKeys[strings.ReplaceAll(guide.key, " ", "")] {
		return true
	}
	return m.channel.Station != "" && m.stationEvidence(guide.key)
}
func guideConflict(channels []channelMatcher, target channelMatcher, label sourceLabel) bool {
	if label.guide.key == "" {
		return false
	}
	// A learned/manual guide association cannot override another recognised name.
	if !target.nameMatches(label) {
		for _, other := range channels {
			if other.channel.ID != target.channel.ID && other.nameMatches(label) {
				return true
			}
		}
	}
	if label.guide.country != "" && !target.compatible(sourceLabel{channelLabel: label.guide}) {
		return true
	}
	// Recognise bundled service IDs even if the user has removed that service
	// from their output. For example CNBC remains different from NBC when NBC
	// is the only selected channel. This supplies vetoes, never assignments.
	if !target.identifiesGuide(label.guide) {
		for _, reference := range referenceGuideMatchers() {
			if !target.guideKeys[reference.networkKey] && reference.identifiesGuide(label.guide) {
				return true
			}
		}
	}
	// Multiple recognised services sharing the same ID are unsafe, even if one
	// is the proposed target. Explicit manual mappings must obey this too.
	for _, other := range channels {
		if other.channel.ID != target.channel.ID && other.identifiesGuide(label.guide) {
			return true
		}
	}
	return false
}

var referenceGuideMatchers = sync.OnceValue(func() []channelMatcher {
	channels, err := DefaultUSChannels()
	if err != nil {
		panic("invalid bundled channel definitions: " + err.Error())
	}
	// A distinct service that providers sometimes incorrectly label simply NBC.
	channels = append(channels, Channel{Name: "NBC News Now", Region: "US"})
	out := make([]channelMatcher, 0, len(channels))
	for _, c := range channels {
		out = append(out, compileChannel(c))
	}
	return out
})
