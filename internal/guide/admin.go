package guide

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"antennarr/internal/netguard"
)

func (s *Service) SetFeed(ctx context.Context, profile, raw string) error {
	if profile == "" {
		return errors.New("choose a tuner")
	}
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, e := url.Parse(raw)
		if e != nil || len(raw) > 4096 || netguard.URL(u) != nil {
			return errors.New("enter a public HTTP or HTTPS guide URL")
		}
	}
	tx, e := s.store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT 1 FROM output_profiles WHERE id=?", profile).Scan(&exists); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM guide_stations WHERE profile_id=?", profile); e != nil {
		return e
	}
	if raw == "" {
		_, e = tx.ExecContext(ctx, "DELETE FROM guide_feeds WHERE profile_id=?", profile)
	} else {
		_, e = tx.ExecContext(ctx, "INSERT INTO guide_feeds(profile_id,url) VALUES(?,?) ON CONFLICT(profile_id) DO UPDATE SET url=excluded.url", profile, raw)
	}
	if e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		s.Trigger()
	}
	return e
}
func (s *Service) SetPin(ctx context.Context, profile, id, candidate string) error {
	tx, e := s.store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var raw string
	e = tx.QueryRowContext(ctx, `SELECT coalesce(g.snapshot,'{}') FROM channels c LEFT JOIN guide_channels g ON g.channel_id=c.id WHERE c.profile_id=? AND c.id=?`, profile, id).Scan(&raw)
	if e != nil {
		return e
	}
	if candidate != "" {
		var snap snapshot
		if e = json.Unmarshal([]byte(raw), &snap); e != nil {
			return e
		}
		found := false
		for _, c := range snap.Candidates {
			if c.ID == candidate {
				found = true
				break
			}
		}
		if !found {
			var match int
			e = tx.QueryRowContext(ctx, "SELECT 1 FROM guide_stations WHERE profile_id=? AND id=?", profile, candidate).Scan(&match)
			if e != nil {
				return errors.New("station is unavailable on this tuner")
			}
		}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO guide_channels(channel_id,pin) VALUES(?,?) ON CONFLICT(channel_id) DO UPDATE SET pin=excluded.pin", id, candidate)
	if e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		s.Trigger()
	}
	return e
}

type summary struct {
	State      string      `json:"state"`
	Pin        string      `json:"pin"`
	Now        *Programme  `json:"now,omitempty"`
	Next       *Programme  `json:"next,omitempty"`
	Until      int64       `json:"until"`
	Origins    int         `json:"origins"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Checked    int64       `json:"checked,omitempty"`
}

func (snap snapshot) info(pin string, now int64) summary {
	v := summary{Pin: pin, State: "missing", Checked: snap.Checked}
	v.Now, v.Next = nowNext(snap.Schedule, now)
	first := v.Now
	if first == nil {
		first = v.Next
	}
	if first != nil {
		v.State = first.State
		v.Origins = first.Origins
		v.Until = snap.Schedule[len(snap.Schedule)-1].Stop
	}
	if snap.Conflict && pin == "" && v.Until >= now {
		v.State = "conflict"
	}
	if snap.Conflict && len(snap.Schedule) == 0 && snap.Checked >= now-3600 {
		v.State = "conflict"
	}
	return v
}
func (s *Service) records(ctx context.Context, profile string, read func(string, string, snapshot)) error {
	rows, e := s.store.DB.QueryContext(ctx, `SELECT c.id,coalesce(g.pin,''),coalesce(g.snapshot,'{}') FROM channels c LEFT JOIN guide_channels g ON c.id=g.channel_id WHERE c.profile_id=? AND c.enabled=1`, profile)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id, pin, data string
		if e = rows.Scan(&id, &pin, &data); e != nil {
			return e
		}
		var snap snapshot
		if e = json.Unmarshal([]byte(data), &snap); e != nil {
			return e
		}
		read(id, pin, snap)
	}
	return rows.Err()
}
func (s *Service) Programmes(ctx context.Context, profile string) (map[string][]Programme, error) {
	out := map[string][]Programme{}
	now := time.Now().Unix()
	err := s.records(ctx, profile, func(id, pin string, snap snapshot) {
		for _, p := range snap.Schedule {
			if p.Stop > now {
				out[id] = append(out[id], p)
			}
		}
	})
	return out, err
}
func currentCandidate(c Candidate, now int64) Candidate {
	var rows []Programme
	if c.Now != nil {
		rows = append(rows, *c.Now)
	}
	if c.Next != nil {
		rows = append(rows, *c.Next)
	}
	c.Now, c.Next = nowNext(rows, now)
	return c
}
func (s *Service) get(r *http.Request) (any, error) {
	ctx := r.Context()
	q := r.URL.Query()
	profile := q.Get("profile_id")
	if _, e := s.store.Profile(profile); e != nil {
		return nil, e
	}
	now := time.Now().Unix()
	switch r.URL.Path {
	case "/api/guide":
		channels := map[string]summary{}
		if err := s.records(ctx, profile, func(id, pin string, snap snapshot) { channels[id] = snap.info(pin, now) }); err != nil {
			return nil, err
		}
		var raw string
		host := ""
		e := s.store.DB.QueryRowContext(ctx, "SELECT url FROM guide_feeds WHERE profile_id=?", profile).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if u, e := url.Parse(raw); e == nil {
			host = u.Hostname()
		}
		return map[string]any{"channels": channels, "feed_configured": raw != "", "feed_host": host}, nil
	case "/api/guide/channel":
		var pin, data string
		e := s.store.DB.QueryRowContext(ctx, `SELECT coalesce(g.pin,''),coalesce(g.snapshot,'{}') FROM channels c LEFT JOIN guide_channels g ON g.channel_id=c.id WHERE c.profile_id=? AND c.id=?`, profile, q.Get("id")).Scan(&pin, &data)
		if e != nil {
			return nil, e
		}
		var snap snapshot
		if e = json.Unmarshal([]byte(data), &snap); e != nil {
			return nil, e
		}
		info := snap.info(pin, now)
		for _, c := range snap.Candidates {
			info.Candidates = append(info.Candidates, currentCandidate(c, now))
		}
		return info, nil
	case "/api/guide/stations":
		term := strings.TrimSpace(q.Get("q"))
		if term == "" || len(term) > 120 {
			return nil, errors.New("enter a search term of up to 120 bytes")
		}
		rows, e := s.store.DB.QueryContext(ctx, `SELECT sample FROM guide_stations WHERE profile_id=? AND (instr(lower(station),lower(?))>0 OR instr(lower(name),lower(?))>0) ORDER BY name,station LIMIT 50`, profile, term, term)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		list := []Candidate{}
		for rows.Next() {
			var data string
			if e = rows.Scan(&data); e != nil {
				return nil, e
			}
			var c Candidate
			if e = json.Unmarshal([]byte(data), &c); e != nil {
				return nil, e
			}
			list = append(list, currentCandidate(c, now))
		}
		return map[string]any{"candidates": list}, rows.Err()
	}
	return nil, sql.ErrNoRows
}

func (s *Service) Admin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	var result any = map[string]bool{"ok": true}
	var e error
	if r.Method == http.MethodGet {
		result, e = s.get(r)
	} else if r.Method == http.MethodPost {
		var v struct {
			Profile   string `json:"profile_id"`
			Channel   string `json:"channel_id"`
			Candidate string `json:"candidate_id"`
			URL       string `json:"url"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		d.DisallowUnknownFields()
		if e = d.Decode(&v); e == nil {
			var trailing any
			if d.Decode(&trailing) != io.EOF {
				e = errors.New("malformed request")
			}
		}
		if e == nil {
			switch r.URL.Path {
			case "/api/guide/feed":
				e = s.SetFeed(r.Context(), v.Profile, v.URL)
			case "/api/guide/pin":
				e = s.SetPin(r.Context(), v.Profile, v.Channel, v.Candidate)
			case "/api/guide/refresh":
				if _, e = s.store.Profile(v.Profile); e == nil {
					s.Trigger()
				}
			default:
				e = sql.ErrNoRows
			}
		}
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if e != nil {
		status := http.StatusBadRequest
		if errors.Is(e, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		result = map[string]string{"error": "Guide request failed. Check the tuner, station, or feed URL."}
	}
	_ = json.NewEncoder(w).Encode(result)
}
