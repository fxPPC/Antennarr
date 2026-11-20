package catalog

import (
	"database/sql"
	"errors"
)

type SetupProgress struct {
	State      string `json:"state"`
	Step       string `json:"step"`
	ProfileID  string `json:"profile_id"`
	HasTuner   bool   `json:"has_tuner"`
	HasSources bool   `json:"has_sources"`
}

type SetupUpdate struct {
	State     string  `json:"state"`
	Step      *string `json:"step"`
	ProfileID *string `json:"profile_id"`
}

var (
	ErrSetupChoice     = errors.New("unknown setup state or step")
	ErrSetupProfile    = errors.New("the selected tuner no longer exists")
	ErrSetupIncomplete = errors.New("enable a tuner and import a source before finishing setup")
)

func readSetup(q interface{ QueryRow(string, ...any) *sql.Row }) (SetupProgress, error) {
	var p SetupProgress
	// Only new installations have a pending marker. An upgrade, including an
	// intentionally empty catalogue, opens the usual dashboard.
	err := q.QueryRow(`SELECT
		coalesce((SELECT value FROM settings WHERE name='setup_state'),'dismissed'),
		coalesce((SELECT value FROM settings WHERE name='setup_step'),'tuner'),
		coalesce((SELECT id FROM output_profiles WHERE id=(SELECT value FROM settings WHERE name='setup_profile_id')),''),
		EXISTS (SELECT 1 FROM output_profiles WHERE enabled=1),
		EXISTS (SELECT 1 FROM accounts)`).Scan(&p.State, &p.Step, &p.ProfileID, &p.HasTuner, &p.HasSources)
	return p, err
}

func (s *Store) SetupProgress() (SetupProgress, error) {
	p, err := readSetup(s.DB)
	if err != nil || p.State != "pending" || !p.HasTuner || !p.HasSources {
		return p, err
	}
	// An installation configured outside the guide should stay on the dashboard.
	// Recheck all conditions in the write so a concurrent reopen or edit wins.
	if _, err = s.DB.Exec(`UPDATE settings SET value='complete' WHERE name='setup_state' AND value='pending'
		AND EXISTS (SELECT 1 FROM output_profiles WHERE enabled=1) AND EXISTS (SELECT 1 FROM accounts)`); err != nil {
		return p, err
	}
	return readSetup(s.DB)
}

func (s *Store) UpdateSetup(u SetupUpdate) (SetupProgress, error) {
	var p SetupProgress
	if u.State != "active" && u.State != "dismissed" && u.State != "complete" {
		return p, ErrSetupChoice
	}
	if u.Step != nil && *u.Step != "tuner" && *u.Step != "sources" && *u.Step != "plex" {
		return p, ErrSetupChoice
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	p, err = readSetup(tx)
	if err != nil {
		return p, err
	}
	if u.State == "complete" && (!p.HasTuner || !p.HasSources) {
		return p, ErrSetupIncomplete
	}
	if u.ProfileID != nil {
		p.ProfileID = *u.ProfileID
	}
	if p.ProfileID != "" && (u.ProfileID != nil || u.State == "complete") {
		var enabled bool
		err = tx.QueryRow("SELECT enabled FROM output_profiles WHERE id=?", p.ProfileID).Scan(&enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return p, ErrSetupProfile
		}
		if err != nil {
			return p, err
		}
		if u.State == "complete" && !enabled {
			return p, ErrSetupIncomplete
		}
	}
	if u.Step != nil {
		p.Step = *u.Step
	}
	p.State = u.State
	if _, err = tx.Exec(`INSERT INTO settings(name,value) VALUES('setup_state',?),('setup_step',?),('setup_profile_id',?)
		ON CONFLICT(name) DO UPDATE SET value=excluded.value`, p.State, p.Step, p.ProfileID); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
