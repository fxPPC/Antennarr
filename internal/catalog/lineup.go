package catalog

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Lineup contains portable channel definitions only, never source credentials,
// roster assignments, learned account metadata, or the device identity.
type Lineup struct {
	Version  int             `json:"version"`
	Channels []LineupChannel `json:"channels"`
}
type LineupChannel struct {
	Station string       `json:"station,omitempty"`
	ID      string       `json:"id"`
	Number  string       `json:"number"`
	Name    string       `json:"name"`
	Region  string       `json:"region"`
	Enabled bool         `json:"enabled"`
	Aliases []string     `json:"aliases"`
	Policy  RosterPolicy `json:"policy"`
}

func (s *Store) ExportLineup() (Lineup, error) { return s.ExportLineupForProfile("primary") }

func (s *Store) ExportLineupForProfile(profile string) (Lineup, error) {
	out := Lineup{Version: 1, Channels: []LineupChannel{}}
	// A single SELECT gives a coherent export during edits without fetching
	// source counts or reading any account/stream credential records.
	rows, err := s.DB.Query(`SELECT c.id,c.number,c.name,c.region,c.enabled,c.aliases,coalesce(p.target,0),coalesce(p.preference,''),coalesce(p.race,0),coalesce((SELECT value FROM settings WHERE name='channel_station:'||c.id),'') FROM channels c LEFT JOIN channel_policies p ON p.channel_id=c.id WHERE c.profile_id=? ORDER BY CAST(c.number AS REAL),c.id`, profile)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch LineupChannel
		var raw string
		if err = rows.Scan(&ch.ID, &ch.Number, &ch.Name, &ch.Region, &ch.Enabled, &raw, &ch.Policy.Target, &ch.Policy.Preference, &ch.Policy.Race, &ch.Station); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &ch.Aliases); err != nil {
			return out, err
		}
		out.Channels = append(out.Channels, ch)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}
func (v Lineup) Validate() error {
	if v.Version != 1 || len(v.Channels) > 1000 {
		return errors.New("unsupported or oversized channel lineup")
	}
	ids := map[string]bool{}
	numbers := map[string]bool{}
	for _, ch := range v.Channels {
		if !validStation(normaliseStation(ch.Station)) || strings.TrimSpace(ch.ID) == "" || len(ch.ID) > 100 || ids[ch.ID] || numbers[ch.Number] || strings.TrimSpace(ch.Name) == "" || len(ch.Name) > 100 || len(ch.Region) > 80 || len(ch.Aliases) > 200 || len(ch.Number) == 0 || len(ch.Number) > 10 || !ch.Policy.Valid(true) {
			return errors.New("invalid or duplicate channel definition")
		}
		for _, r := range ch.Number {
			if (r < '0' || r > '9') && r != '.' {
				return errors.New("invalid channel number")
			}
		}
		for _, a := range ch.Aliases {
			if len(a) > 200 || strings.TrimSpace(a) == "" {
				return errors.New("invalid channel alias")
			}
		}
		ids[ch.ID] = true
		numbers[ch.Number] = true
	}
	return nil
}

// Validate the whole file before the atomic replacement. A failed rematch rolls
// back everything. Accounts, settings, and tuner identity are never replaced.
func (s *Store) ImportLineup(v Lineup) error { return s.ImportLineupForProfile("primary", v) }

func (s *Store) ImportLineupForProfile(profile string, v Lineup) error {
	if err := v.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var present int
	if err = tx.QueryRow("SELECT count(*) FROM output_profiles WHERE id=?", profile).Scan(&present); err != nil {
		return err
	}
	if present != 1 {
		return errors.New("tuner not found")
	}
	if err = clearProfileChannels(tx, profile); err != nil {
		return err
	}
	for _, ch := range v.Channels {
		var conflict int
		if err = tx.QueryRow("SELECT count(*) FROM channels WHERE id=?", ch.ID).Scan(&conflict); err != nil {
			return err
		}
		if conflict > 0 {
			ch.ID = profile + "-import-" + ch.Number
		}
		aliases, _ := json.Marshal(ch.Aliases)
		if _, err = tx.Exec("INSERT INTO channels(id,number,name,region,enabled,aliases,profile_id) VALUES(?,?,?,?,?,?,?)", ch.ID, ch.Number, ch.Name, ch.Region, ch.Enabled, string(aliases), profile); err != nil {
			return err
		}
		if err = saveStation(tx, ch.ID, ch.Station); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO channel_policies(channel_id,target,preference,race) VALUES(?,?,?,?)", ch.ID, ch.Policy.Target, ch.Policy.Preference, ch.Policy.Race); err != nil {
			return err
		}
	}
	// Stored stream lists contain only the previous selected channels. Re-audition
	// Xtream catalogues before cleanup can call an account unmatched in the new market.
	if _, err = tx.Exec("UPDATE accounts SET catalogue_checked=0 WHERE kind='xtream' AND state NOT IN ('retired','manually_blocked')"); err != nil {
		return err
	}
	if err = autoMap(tx); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO events(happened,kind,reason) VALUES(?,'lineup_replaced','Channel lineup replaced; verified rosters will rebuild')", time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
