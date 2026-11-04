package catalog

import (
	"antennarr/internal/ingest"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

type Candidate struct {
	UHD         bool
	StreamID    int64
	AccountID   int64
	Provider    string
	Limit       int
	URL         string
	Codec       string
	Score       float64
	LastSuccess int64
}

func (s *Store) Candidates(channel string, onlyHealthy bool) ([]Candidate, error) {
	query := `SELECT s.id,s.account_id,a.provider,(a.connection_limit-a.external_connections),s.secret,s.codec,s.score,s.last_success FROM streams s JOIN accounts a ON a.id=s.account_id JOIN mappings m ON m.stream_id=s.id JOIN channels c ON c.id=m.channel_id WHERE m.channel_id=? AND m.enabled=1 AND c.enabled=1 AND s.enabled=1 AND a.state NOT IN ('manually_blocked','inactive','retired') AND s.state NOT IN ('manually_blocked','retired')`
	if onlyHealthy {
		query += ` AND s.state='healthy'`
	}
	query += ` ORDER BY s.score DESC,s.startup_ms,s.id`
	args := []any{channel}
	rows, e := s.DB.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		var b []byte
		if e = rows.Scan(&c.StreamID, &c.AccountID, &c.Provider, &c.Limit, &b, &c.Codec, &c.Score, &c.LastSuccess); e != nil {
			return nil, e
		}
		var entry ingest.Entry
		if json.Unmarshal(b, &entry) != nil {
			return nil, errors.New("invalid source record")
		}
		c.URL = entry.URL
		c.UHD = IsUHD(entry.Name)
		out = append(out, c)
	}
	return out, rows.Err()
}

func HealthState(failures int) string {
	if failures < 1 {
		return "healthy"
	}
	return "suspect"
}

type Result struct {
	Success               bool
	StartupMS, DurationMS int64
	Reason, Codec         string
	Width, Height         int
	PlaybackSeconds       float64
	Stalls                int
}

func (s *Store) Record(id int64, r Result) error { return s.record(id, r, nil) }

// RecordForChannels saves shared stream health but restricts successful admission
// to the named outputs. A genuine failure still invalidates every membership.
func (s *Store) RecordForChannels(id int64, r Result, channels map[string]bool) error {
	return s.record(id, r, channels)
}
func (s *Store) record(id int64, r Result, channels map[string]bool) error {
	s.catalogueWrites.Lock()
	defer s.catalogueWrites.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var successes, failures, consecutive int
	var oldStartup float64
	if e = tx.QueryRow("SELECT successes,failures,consecutive_failures,startup_ms FROM streams WHERE id=?", id).Scan(&successes, &failures, &consecutive, &oldStartup); e != nil {
		return e
	}
	weight := 1
	if r.PlaybackSeconds >= 30 {
		weight = 4
	}
	recent := 0.0
	lastSuccess := int64(0)
	if r.Success {
		successes += weight
		consecutive = 0
		recent = 1
		lastSuccess = time.Now().Unix()
	} else {
		failures += weight
		consecutive++
	}
	startup := float64(r.StartupMS)
	if startup == 0 {
		startup = oldStartup
	}
	speed := 1000 / (1000 + startup)
	quality := float64(r.Height) / 1080
	if quality > 1 {
		quality = 1
	}
	score := 0.55*recent + 0.25*float64(successes+1)/float64(successes+failures+2) + 0.15*speed + 0.05*quality
	state := HealthState(consecutive)
	policy := policyIn(tx)
	if !r.Success && policy.StreamFailures > 0 && consecutive >= policy.StreamFailures {
		state = "retired"
	}
	_, e = tx.Exec(`UPDATE streams SET successes=?,failures=?,consecutive_failures=?,state=?,score=?,startup_ms=?,last_check=?,next_check=0,last_success=CASE WHEN ?>0 THEN ? ELSE last_success END,codec=CASE WHEN ?!='' THEN ? ELSE codec END,width=CASE WHEN ?>0 THEN ? ELSE width END,height=CASE WHEN ?>0 THEN ? ELSE height END,playback_seconds=playback_seconds+?,stalls=stalls+? WHERE id=? AND state NOT IN ('manually_blocked','retired')`, successes, failures, consecutive, state, score, startup, time.Now().Unix(), lastSuccess, lastSuccess, r.Codec, r.Codec, r.Width, r.Width, r.Height, r.Height, r.PlaybackSeconds, r.Stalls, id)
	if e != nil {
		return e
	}
	reason := r.Reason
	if len(reason) > 100 {
		reason = "media check completed"
	}
	_, e = tx.Exec("INSERT INTO validations(stream_id,checked,success,reason,startup_ms,duration_ms) VALUES(?,?,?,?,?,?)", id, time.Now().Unix(), r.Success, reason, r.StartupMS, r.DurationMS)
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return s.rosterObserved(id, r.Success, channels)
}

func (s *Store) Event(kind, channel string, source int64, reason string) {
	// Docker logs intentionally omit free-form reasons, URLs, and provider names.
	// Detailed, bounded event history remains available through authenticated UI.
	switch kind {
	case "failover":
		slog.Info("live backup promoted", "channel_id", channel, "stream_id", source)
	case "output_recovery":
		slog.Warn("channel output recovering", "channel_id", channel, "stream_id", source)
	case "source_failure":
		code := map[string]string{"source stopped": "source_stopped", "standby stalled": "standby_stalled", "source startup timeout": "startup_timeout", "video, audio, or transport stalled": "media_stalled", "source audio could not be decoded": "audio_invalid"}[reason]
		if code == "" {
			code = "media_failure"
		}
		slog.Warn("playback source failed", "channel_id", channel, "stream_id", source, "failure_kind", code)
	case "source_released":
		slog.Debug("startup candidate released without failure", "channel_id", channel, "stream_id", source)
	}
	if _, err := s.DB.Exec("INSERT INTO events(happened,kind,channel_id,source_id,reason) VALUES(?,?,?,?,?)", time.Now().Unix(), kind, channel, source, reason); err != nil {
		slog.Warn("playback event could not be saved", "event", kind)
	}
}
func (s *Store) Housekeep() error {
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{expireQualityCacheSQL, []any{time.Now().Unix() - 3600}},
		{"DELETE FROM settings WHERE name GLOB 'accounts_retired_minute_*' AND CAST(substr(name,length('accounts_retired_minute_')+1) AS INTEGER)<?", []any{time.Now().Unix()/60 - 2880}},
		{"DELETE FROM work_leases WHERE until<=?", []any{time.Now().Unix()}},
		{trimValidationRowsSQL, nil},
		{trimEventRowsSQL, nil},
		{trimOldEventsSQL, nil},
	} {
		if _, err := s.DB.Exec(q.sql, q.args...); err != nil {
			return err
		}
	}
	return nil
}
