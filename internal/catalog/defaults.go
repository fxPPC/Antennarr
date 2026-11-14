package catalog

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const MaxSelectedChannels = 256

//go:embed data/us_channels.json
var defaultUSChannelData []byte

// These are names and rules only: never accounts, credentials, or stream URLs.
func DefaultUSChannels() ([]Channel, error) {
	var channels []Channel
	err := json.Unmarshal(defaultUSChannelData, &channels)
	return channels, err
}
func (s *Store) seedInitialChannels() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var preset string
	err = tx.QueryRow("SELECT value FROM settings WHERE name='initial_channel_preset'").Scan(&preset)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if preset != "US" {
		return nil
	}
	channels, err := DefaultUSChannels()
	if err != nil {
		return err
	}
	for _, c := range channels {
		if err = saveStation(tx, "us-default-"+c.Number, c.Station); err != nil {
			return err
		}
		aliases, _ := json.Marshal(c.Aliases)
		if _, err = tx.Exec("INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES(?,?,?,'US',1,?)", "us-default-"+c.Number, c.Number, c.Name, string(aliases)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM settings WHERE name='initial_channel_preset'"); err != nil {
		return err
	}
	return tx.Commit()
}

type PresetMerge struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
}

// ApplyUSPreset is explicit and transactional. Existing IDs and unrelated
// channels survive. Audited rules replace old rules on recognised US channels.
func (s *Store) ApplyUSPreset() (PresetMerge, error) {
	result := PresetMerge{}
	preset, err := DefaultUSChannels()
	if err != nil {
		return result, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT id,number,name,region FROM channels WHERE profile_id='primary'")
	if err != nil {
		return result, err
	}
	existing := map[string]Channel{}
	used := map[string]bool{}
	count := 0
	numberKey := func(s string) string {
		v, e := strconv.ParseFloat(s, 64)
		if e != nil {
			return s
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	for rows.Next() {
		var c Channel
		if err = rows.Scan(&c.ID, &c.Number, &c.Name, &c.Region); err != nil {
			rows.Close()
			return result, err
		}
		count++
		used[numberKey(c.Number)] = true
		if r := regionCountry(c.Region); r == "us" || strings.TrimSpace(c.Region) == "" {
			k, _ := channelKey(c.Name, c.Region)
			if _, exists := existing[k]; exists {
				rows.Close()
				return result, fmt.Errorf("multiple existing channels match %s; resolve duplicates first", c.Name)
			}
			existing[k] = c
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	// Free all recognised channels' numbers before assigning guide numbers. This
	// avoids UNIQUE conflicts when old custom numbers overlap another guide row.
	for _, c := range preset {
		k, _ := channelKey(c.Name, c.Region)
		if old, ok := existing[k]; ok {
			delete(used, numberKey(old.Number))
			if _, err = tx.Exec("UPDATE channels SET number=? WHERE id=?", "preset-"+old.ID, old.ID); err != nil {
				return PresetMerge{}, err
			}
		}
	}
	for _, c := range preset {
		k, _ := channelKey(c.Name, c.Region)
		stationID := "us-default-" + c.Number
		if old, ok := existing[k]; ok {
			stationID = old.ID
		}
		if err = saveStation(tx, stationID, c.Station); err != nil {
			return PresetMerge{}, err
		}
		aliases, _ := json.Marshal(c.Aliases)
		if old, ok := existing[k]; ok {
			if used[numberKey(c.Number)] {
				return PresetMerge{}, fmt.Errorf("guide channel %s is occupied by an unrelated channel", c.Number)
			}
			used[numberKey(c.Number)] = true
			if _, err = tx.Exec("UPDATE channels SET number=?,aliases=?,enabled=1 WHERE id=?", c.Number, string(aliases), old.ID); err != nil {
				return PresetMerge{}, err
			}
			result.Updated++
			continue
		}
		if count >= MaxSelectedChannels {
			return PresetMerge{}, fmt.Errorf("preset exceeds %d-channel limit", MaxSelectedChannels)
		}
		number := c.Number
		if used[numberKey(number)] {
			return PresetMerge{}, fmt.Errorf("guide channel %s is occupied by an unrelated channel; renumber that channel first", number)
		}
		used[numberKey(number)] = true
		count++
		if _, err = tx.Exec("INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES(?,?,?,'US',1,?)", "us-default-"+c.Number, number, c.Name, string(aliases)); err != nil {
			return PresetMerge{}, err
		}
		result.Added++
	}
	if err = autoMap(tx); err != nil {
		return PresetMerge{}, err
	}
	if _, err = tx.Exec("UPDATE accounts SET next_check=0,catalogue_checked=0 WHERE state NOT IN ('manually_blocked','retired')"); err != nil {
		return PresetMerge{}, err
	}
	return result, tx.Commit()
}
