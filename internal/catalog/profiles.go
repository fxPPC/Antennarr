package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	// Keep IANA zone validation portable in minimal container images.
	_ "time/tzdata"
)

// Profiles are independently addressable outputs. Device identity and listener
// slots are immutable; changing labels or switching off a tuner retains its data.
type Profile struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Market        string `json:"market"`
	Country       string `json:"country"`
	Timezone      string `json:"timezone"`
	GuideLocation string `json:"guide_location"`
	Preset        string `json:"preset_id"`
	Enabled       bool   `json:"enabled"`
	DeviceID      string `json:"device_id"`
	PortOffset    int    `json:"port_offset"`
}

//go:embed data/uk_london_channels.json
var defaultLondonChannelData []byte

func DefaultLondonChannels() ([]Channel, error) {
	var out []Channel
	err := json.Unmarshal(defaultLondonChannelData, &out)
	return out, err
}

// One registry owns market metadata and its versioned channel definitions.
// Future markets add one entry and matching fixtures, without changing routing.
type presetDefinition struct {
	profile  Profile
	channels func() ([]Channel, error)
}

var tunerPresets = []presetDefinition{
	{Profile{Kind: "hdhomerun", Name: "USA - New York City", Market: "New York City", Country: "US", Timezone: "America/New_York", GuideLocation: "10001", Preset: "us-nyc"}, DefaultUSChannels},
	{Profile{Kind: "hdhomerun", Name: "UK - London", Market: "London", Country: "GB", Timezone: "Europe/London", GuideLocation: "SW1A 1AA", Preset: "uk-london"}, DefaultLondonChannels},
}

func presetProfile(id string) (Profile, error) {
	for _, preset := range tunerPresets {
		if preset.profile.Preset == id {
			return preset.profile, nil
		}
	}
	return Profile{}, errors.New("unknown tuner preset")
}
func presetChannels(id string) ([]Channel, error) {
	for _, preset := range tunerPresets {
		if preset.profile.Preset == id {
			return preset.channels()
		}
	}
	return nil, errors.New("unknown tuner preset")
}

const profileColumns = `id,kind,name,market,country,timezone,guide_location,preset_id,enabled,device_id,port_offset`

func scanProfile(row interface{ Scan(...any) error }) (Profile, error) {
	var p Profile
	e := row.Scan(&p.ID, &p.Kind, &p.Name, &p.Market, &p.Country, &p.Timezone, &p.GuideLocation, &p.Preset, &p.Enabled, &p.DeviceID, &p.PortOffset)
	return p, e
}
func (s *Store) Profiles() ([]Profile, error) {
	rows, e := s.DB.Query("SELECT " + profileColumns + " FROM output_profiles ORDER BY port_offset,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		p, e := scanProfile(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Profile(id string) (Profile, error) {
	return scanProfile(s.DB.QueryRow("SELECT "+profileColumns+" FROM output_profiles WHERE id=?", id))
}

func (s *Store) SaveProfile(p Profile) (Profile, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.Market = strings.TrimSpace(p.Market)
	p.Country = strings.ToUpper(strings.TrimSpace(p.Country))
	if p.Name == "" || len(p.Name) > 100 || len(p.Market) > 100 || len(p.GuideLocation) > 160 || len(p.Country) > 3 {
		return Profile{}, errors.New("invalid tuner metadata")
	}
	if p.Timezone != "" {
		if _, e := time.LoadLocation(p.Timezone); e != nil {
			return Profile{}, errors.New("invalid tuner time zone")
		}
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return Profile{}, e
	}
	defer tx.Rollback()
	old, e := scanProfile(tx.QueryRow("SELECT "+profileColumns+" FROM output_profiles WHERE id=?", p.ID))
	if e != nil {
		return Profile{}, e
	}
	// Names and guide recommendations may change without giving Plex a new device.
	if _, e = tx.Exec("UPDATE output_profiles SET name=?,market=?,country=?,timezone=?,guide_location=?,enabled=? WHERE id=?", p.Name, p.Market, p.Country, p.Timezone, p.GuideLocation, p.Enabled, p.ID); e != nil {
		return Profile{}, e
	}
	if !old.Enabled && p.Enabled {
		if _, e = tx.Exec("UPDATE accounts SET catalogue_checked=0 WHERE kind='xtream' AND state NOT IN ('retired','manually_blocked')"); e != nil {
			return Profile{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return Profile{}, e
	}
	return s.Profile(p.ID)
}

// Allocate offsets monotonically, including removed profiles. A replacement
// tuner must not silently inherit a removed device's identity or address.
func addProfilePreset(tx *sql.Tx, preset string, maxOffset ...int) (Profile, error) {
	p, e := presetProfile(preset)
	if e != nil {
		return p, e
	}
	var exists int
	if e = tx.QueryRow("SELECT count(*) FROM output_profiles WHERE preset_id=?", preset).Scan(&exists); e != nil {
		return p, e
	}
	if exists > 0 {
		return p, errors.New("tuner preset already added")
	}
	b := make([]byte, 12)
	if _, e = rand.Read(b); e != nil {
		return p, e
	}
	p.ID = "tuner-" + hex.EncodeToString(b)
	p.DeviceID, e = newProfileDeviceID()
	if e != nil {
		return p, e
	}
	if e = tx.QueryRow("SELECT max(coalesce((SELECT max(port_offset) FROM output_profiles),0),coalesce((SELECT CAST(value AS INTEGER) FROM settings WHERE name='profile_slot_highwater'),0))+1").Scan(&p.PortOffset); e != nil {
		return p, e
	}
	if len(maxOffset) > 0 && p.PortOffset > maxOffset[0] {
		return p, errors.New("tuner listener range is full; configure more private tuner ports")
	}
	if _, e = tx.Exec("INSERT INTO output_profiles("+profileColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?)", p.ID, p.Kind, p.Name, p.Market, p.Country, p.Timezone, p.GuideLocation, p.Preset, p.Enabled, p.DeviceID, p.PortOffset); e != nil {
		return p, e
	}
	if _, e = tx.Exec("INSERT INTO settings(name,value) VALUES('profile_slot_highwater',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", fmt.Sprint(p.PortOffset)); e != nil {
		return p, e
	}
	chs, e := presetChannels(preset)
	if e != nil {
		return p, e
	}
	for _, c := range chs {
		c.ID = p.ID + "-" + c.Number
		c.ProfileID = p.ID
		c.Enabled = true
		if e = insertProfileChannel(tx, c); e != nil {
			return p, e
		}
	}
	// Presets start disabled. Enabling the tuner invalidates catalogue checks
	// when its channels actually become eligible for recruitment.
	return p, nil
}
func (s *Store) AddProfilePreset(preset string, maxOffset ...int) (Profile, error) {
	tx, e := s.DB.Begin()
	if e != nil {
		return Profile{}, e
	}
	defer tx.Rollback()
	p, e := addProfilePreset(tx, preset, maxOffset...)
	if e != nil {
		return p, e
	}
	if e = autoMap(tx); e != nil {
		return p, e
	}
	return p, tx.Commit()
}
func insertProfileChannel(tx *sql.Tx, c Channel) error {
	raw, e := json.Marshal(c.Aliases)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO channels(id,profile_id,number,name,region,enabled,aliases) VALUES(?,?,?,?,?,?,?)", c.ID, c.ProfileID, c.Number, c.Name, c.Region, c.Enabled, string(raw)); e != nil {
		return e
	}
	return saveStation(tx, c.ID, c.Station)
}

func (s *Store) seedLondonProfile() error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var marker string
	e = tx.QueryRow("SELECT value FROM settings WHERE name='multi_tuner_presets_seeded'").Scan(&marker)
	if e == nil {
		return nil
	}
	if e != sql.ErrNoRows {
		return e
	}
	if _, e = addProfilePreset(tx, "uk-london"); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO settings(name,value) VALUES('multi_tuner_presets_seeded','1')"); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) DeleteProfile(id string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = clearProfileChannels(tx, id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM output_profiles WHERE id=?", id); e != nil {
		return e
	}
	if e = autoMap(tx); e != nil {
		return e
	}
	return tx.Commit()
}

func newProfileDeviceID() (string, error) {
	raw := make([]byte, 4)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	raw[0] = 0x12
	id := hex.EncodeToString(raw)[:7]
	lut := []byte{10, 5, 15, 6, 7, 12, 1, 11, 9, 2, 8, 13, 4, 3, 14, 0}
	var sum byte
	for i, v := range id {
		n := byte(v - '0')
		if v >= 'a' {
			n = byte(v - 'a' + 10)
		}
		if i%2 == 0 {
			sum ^= lut[n]
		} else {
			sum ^= n
		}
	}
	return fmt.Sprintf("%s%x", id, sum), nil
}

// Rebuild only the number constraint in a single startup transaction. Foreign
// keys are disabled on this dedicated connection, never globally or mid-worker;
// a full FK check precedes commit, so failure leaves schema 8 untouched.
func migrateProfiles(db *sql.DB) error {
	ctx := context.Background()
	conn, e := db.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	if _, e = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); e != nil {
		return e
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	queries := []string{
		`ALTER TABLE output_profiles ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE output_profiles ADD COLUMN preset_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE output_profiles ADD COLUMN device_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE output_profiles ADD COLUMN port_offset INTEGER NOT NULL DEFAULT 0`,
		`CREATE UNIQUE INDEX output_profile_port ON output_profiles(port_offset)`,
		`DROP TRIGGER IF EXISTS primary_channel_insert`, `DROP TRIGGER IF EXISTS primary_channel_update`,
		`DROP TABLE output_assignments`,
		`CREATE TABLE channels_next(id TEXT PRIMARY KEY,number TEXT NOT NULL,name TEXT NOT NULL,region TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,aliases TEXT NOT NULL DEFAULT '[]',profile_id TEXT NOT NULL DEFAULT 'primary' REFERENCES output_profiles(id) ON DELETE CASCADE,UNIQUE(profile_id,number))`,
		`INSERT INTO channels_next(id,number,name,region,enabled,aliases) SELECT id,number,name,region,enabled,aliases FROM channels`,
		`DROP TABLE channels`, `ALTER TABLE channels_next RENAME TO channels`,
		`UPDATE output_profiles SET name=CASE WHEN name='Antennarr' THEN 'Existing tuner' ELSE name END WHERE id='primary'`,
		`UPDATE output_profiles SET name=CASE WHEN name='Existing tuner' THEN 'USA - New York City' ELSE name END,preset_id='us-nyc',country='US',market='New York City',timezone='America/New_York',guide_location='10001' WHERE id='primary' AND EXISTS(SELECT 1 FROM channels WHERE id GLOB 'us-default-*') AND NOT EXISTS(SELECT 1 FROM channels WHERE upper(region) NOT IN ('US','USA'))`,
		`CREATE VIEW active_channels AS SELECT c.* FROM channels c JOIN output_profiles p ON p.id=c.profile_id WHERE p.enabled=1`,
	}
	for _, q := range queries {
		if _, e = tx.Exec(q); e != nil {
			return fmt.Errorf("tuner format migration failed: %w", e)
		}
	}
	rows, e := tx.Query("PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	bad := rows.Next()
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if bad {
		return errors.New("tuner format migration found inconsistent references")
	}
	if _, e = tx.Exec("PRAGMA user_version=9"); e != nil {
		return e
	}
	return tx.Commit()
}

// Presets contain portable definitions and guide suggestions, never account
// sources or device identities. Each addition copies its versioned definitions.
type Preset struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Country       string `json:"country"`
	Market        string `json:"market"`
	Timezone      string `json:"timezone"`
	GuideLocation string `json:"guide_location"`
	Channels      int    `json:"channels"`
	Version       int    `json:"version"`
}

func Presets() []Preset {
	out := []Preset{}
	for _, preset := range tunerPresets {
		p := preset.profile
		chs, _ := preset.channels()
		out = append(out, Preset{ID: p.Preset, Name: p.Name, Country: p.Country, Market: p.Market, Timezone: p.Timezone, GuideLocation: p.GuideLocation, Channels: len(chs), Version: 1})
	}
	return out
}
