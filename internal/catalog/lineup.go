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
	ID      string       `json:"id"`
	Number  string       `json:"number"`
	Name    string       `json:"name"`
	Region  string       `json:"region"`
	Enabled bool         `json:"enabled"`
	Aliases []string     `json:"aliases"`
	Policy  RosterPolicy `json:"policy"`
}

func (s *Store) ExportLineup() (Lineup, error) {
	out := Lineup{Version: 1, Channels: []LineupChannel{}}
	// A single SELECT gives a coherent export during edits without fetching
	// source counts or reading any account/stream credential records.
	rows, err := s.DB.Query(`SELECT c.id,c.number,c.name,c.region,c.enabled,c.aliases,coalesce(p.target,0),coalesce(p.preference,''),coalesce(p.race,0) FROM channels c LEFT JOIN channel_policies p ON p.channel_id=c.id ORDER BY CAST(c.number AS REAL),c.id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch LineupChannel
		var raw string
		if err = rows.Scan(&ch.ID, &ch.Number, &ch.Name, &ch.Region, &ch.Enabled, &raw, &ch.Policy.Target, &ch.Policy.Preference, &ch.Policy.Race); err != nil {
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
		if strings.TrimSpace(ch.ID) == "" || len(ch.ID) > 100 || ids[ch.ID] || numbers[ch.Number] || strings.TrimSpace(ch.Name) == "" || len(ch.Name) > 100 || len(ch.Region) > 80 || len(ch.Aliases) > 200 || len(ch.Number) == 0 || len(ch.Number) > 10 || !ch.Policy.Valid(true) {
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
func (s *Store) ImportLineup(v Lineup) error {
	if err := v.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM channel_aliases", "DELETE FROM channels", "DELETE FROM roster_pass_work", "DELETE FROM roster_pass_accounts", "DELETE FROM roster_pass", "DELETE FROM settings WHERE name IN ('roster_cache_uhd_true','roster_cache_uhd_false')"} {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	for _, ch := range v.Channels {
		aliases, _ := json.Marshal(ch.Aliases)
		if _, err = tx.Exec("INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES(?,?,?,?,?,?)", ch.ID, ch.Number, ch.Name, ch.Region, ch.Enabled, string(aliases)); err != nil {
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
