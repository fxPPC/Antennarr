package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

type RosterPolicy struct {
	Target     int    `json:"target"`
	Preference string `json:"preference"`
	Race       int    `json:"race"`
}

func (p RosterPolicy) Valid(override bool) bool {
	min := 2
	mr := 2
	if override {
		min = 0
		mr = 0
	}
	return p.Target != 1 && p.Race != 1 && p.Target >= min && p.Target <= 200 && p.Race >= mr && p.Race <= 10 && (p.Preference == "stability" || p.Preference == "diversity" || (override && p.Preference == ""))
}
func (s *Store) RosterEnabled() bool { return s.rosterPolicy.Load() != nil }
func (s *Store) EffectivePolicy(channel string) (RosterPolicy, error) {
	return s.effectivePolicy(s.DB, channel)
}
func (s *Store) effectivePolicy(q interface{ QueryRow(string, ...any) *sql.Row }, channel string) (RosterPolicy, error) {
	base := s.rosterPolicy.Load()
	if base == nil {
		return RosterPolicy{30, "stability", 5}, nil
	}
	p := *base
	var o RosterPolicy
	err := q.QueryRow("SELECT target,preference,race FROM channel_policies WHERE channel_id=?", channel).Scan(&o.Target, &o.Preference, &o.Race)
	if err != nil && err != sql.ErrNoRows {
		return p, err
	}
	if o.Target != 0 {
		p.Target = o.Target
	}
	if o.Preference != "" {
		p.Preference = o.Preference
	}
	if o.Race != 0 {
		p.Race = o.Race
	}
	return p, nil
}
func (s *Store) ChannelPolicy(channel string) (RosterPolicy, error) {
	var p RosterPolicy
	e := s.DB.QueryRow("SELECT target,preference,race FROM channel_policies WHERE channel_id=?", channel).Scan(&p.Target, &p.Preference, &p.Race)
	if e == sql.ErrNoRows {
		return p, nil
	}
	return p, e
}
func (s *Store) SaveChannelPolicy(channel string, p RosterPolicy) error {
	if !p.Valid(true) {
		return errors.New("invalid channel roster policy")
	}
	_, e := s.DB.Exec("INSERT INTO channel_policies(channel_id,target,preference,race) VALUES(?,?,?,?) ON CONFLICT(channel_id) DO UPDATE SET target=excluded.target,preference=excluded.preference,race=excluded.race", channel, p.Target, p.Preference, p.Race)
	return e
}

func (s *Store) EnableRosters(p RosterPolicy) error {
	if !p.Valid(false) {
		return errors.New("invalid roster defaults")
	}
	s.rosterPolicy.Store(&p)
	var marker string
	e := s.DB.QueryRow("SELECT value FROM settings WHERE name='rosters_seeded'").Scan(&marker)
	if e == nil {
		return nil
	}
	if e != sql.ErrNoRows {
		return e
	}
	// Seeding uses existing real-media successes, never account logins alone.
	channels, e := s.Channels()
	if e != nil {
		return e
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		pool, e := s.Candidates(ch.ID, true)
		if e != nil {
			return e
		}
		sort.SliceStable(pool, func(i, j int) bool { return pool[i].LastSuccess > pool[j].LastSuccess })
		for _, c := range pool {
			if e = s.admit(ch.ID, c.StreamID, c.LastSuccess); e != nil {
				return e
			}
		}
	}
	_, e = s.DB.Exec("INSERT OR IGNORE INTO settings(name,value) VALUES('rosters_seeded','1')")
	return e
}
func (s *Store) admit(channel string, id, verified int64) error {
	p, e := s.EffectivePolicy(channel)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// The schema-8 due value mirrors verification time; pass work owns scheduling.
	due := verified
	res, e := tx.Exec(`INSERT OR IGNORE INTO roster(channel_id,stream_id,account_id,admitted,verified,due)
 SELECT ?,s.id,s.account_id,?,?,? FROM streams s JOIN accounts a ON a.id=s.account_id JOIN mappings m ON m.stream_id=s.id JOIN channels ch ON ch.id=m.channel_id
 WHERE s.id=? AND m.channel_id=? AND m.enabled=1 AND ch.enabled=1 AND s.enabled=1 AND s.state='healthy' AND (? OR instr(upper(s.name),'UHD')=0) AND a.state NOT IN ('inactive','retired','manually_blocked') AND (SELECT count(*) FROM roster WHERE channel_id=?)<?`, channel, time.Now().Unix(), verified, due, id, channel, s.UHDAllowed(), channel, p.Target)
	if e != nil {
		return e
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if _, e = tx.Exec("INSERT INTO events(happened,kind,channel_id,source_id,reason) VALUES(?,'roster_added',?,?,'Verified stream admitted to roster')", time.Now().Unix(), channel, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Reconcile removes ineligible memberships, never the channel or candidate record.
func (s *Store) ReconcileRosters() error {
	_, e := s.DB.Exec(`DELETE FROM roster WHERE NOT EXISTS(SELECT 1 FROM streams s JOIN accounts a ON a.id=s.account_id JOIN mappings m ON m.stream_id=s.id JOIN channels c ON c.id=m.channel_id WHERE s.id=roster.stream_id AND m.channel_id=roster.channel_id AND c.enabled=1 AND m.enabled=1 AND s.enabled=1 AND s.state NOT IN ('retired','manually_blocked') AND a.state NOT IN ('inactive','retired','manually_blocked'))`)
	if e != nil {
		return e
	}
	// Targets govern admission, not retention. Existing healthy members may
	// exceed a lowered target and continue to be checked until they leave naturally.
	return nil
}
func (s *Store) RosterMembers(channel string) ([]Candidate, error) {
	q := `SELECT s.id,s.account_id,a.provider,a.connection_limit-a.external_connections,s.secret,s.codec,s.score,r.verified,instr(upper(s.name),'UHD')>0 FROM roster r JOIN streams s ON s.id=r.stream_id JOIN accounts a ON a.id=s.account_id JOIN mappings m ON m.channel_id=r.channel_id AND m.stream_id=r.stream_id JOIN channels ch ON ch.id=r.channel_id WHERE r.channel_id=? AND ch.enabled=1 AND m.enabled=1 AND s.enabled=1 AND a.state NOT IN ('inactive','retired','manually_blocked') AND s.state NOT IN ('retired','manually_blocked')`
	args := []any{channel}
	q += s.qualitySQL()
	q += " ORDER BY r.verified DESC,s.id LIMIT 200"
	out, e := s.readCandidates(q, args...)
	if e != nil {
		return nil, e
	}
	p, e := s.EffectivePolicy(channel)
	if e != nil {
		return nil, e
	}
	return OrderCandidates(out, s.UHDAllowed(), p.Preference == "diversity"), nil
}
func (s *Store) readCandidates(q string, args ...any) ([]Candidate, error) {
	rows, e := s.DB.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		var raw []byte
		if e = rows.Scan(&c.StreamID, &c.AccountID, &c.Provider, &c.Limit, &raw, &c.Codec, &c.Score, &c.LastSuccess, &c.UHD); e != nil {
			return nil, e
		}
		var entry struct {
			URL string `json:"url"`
		}
		if e = json.Unmarshal(raw, &entry); e != nil {
			return nil, e
		}
		c.URL = entry.URL
		out = append(out, c)
	}
	return out, rows.Err()
}

// QueueCandidates only fetches a bounded window. No media or provider API is requested here.
func (s *Store) QueueCandidates(channel string, limit int) ([]Candidate, error) {
	return s.queueCandidates(channel, limit, 0)
}

// QueueAccountCandidates reuses all normal eligibility, quality, and provider limits.
func (s *Store) QueueAccountCandidates(channel string, account int64) ([]Candidate, error) {
	return s.queueCandidates(channel, 1, account)
}

func (s *Store) queueCandidates(channel string, limit int, account int64) ([]Candidate, error) {
	q := `SELECT s.id,s.account_id,a.provider,a.connection_limit-a.external_connections,s.secret,s.codec,s.score,s.last_success,instr(upper(s.name),'UHD')>0 FROM mappings m JOIN streams s ON s.id=m.stream_id JOIN accounts a ON a.id=s.account_id WHERE m.channel_id=? AND m.enabled=1 AND s.enabled=1 AND a.state NOT IN ('inactive','retired','manually_blocked') AND s.state NOT IN ('retired','manually_blocked') AND a.connection_limit>a.external_connections AND NOT EXISTS(SELECT 1 FROM provider_rates p WHERE p.provider=a.rate_key AND max(p.next_probe,p.blocked_until)>?) AND NOT EXISTS(SELECT 1 FROM roster r WHERE r.stream_id=s.id AND r.channel_id=m.channel_id) AND NOT EXISTS(SELECT 1 FROM roster r WHERE r.account_id=a.id AND r.channel_id=m.channel_id) AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.kind IN ('stream','deferred') AND l.resource=s.id AND l.until>?)`
	now := time.Now().Unix()
	args := []any{channel, now * 1000, now}
	if account > 0 {
		q += " AND a.id=?"
		args = append(args, account)
	}
	p, e := s.EffectivePolicy(channel)
	if e != nil {
		return nil, e
	}
	q += s.qualitySQL()
	if p.Preference == "diversity" {
		q += ` ORDER BY EXISTS(SELECT 1 FROM roster r JOIN accounts ra ON ra.id=r.account_id WHERE r.channel_id=m.channel_id AND ra.rate_key=a.rate_key),`
		if s.UHDAllowed() {
			q += "(instr(upper(s.name),'UHD')>0) DESC,"
		}
		q += "coalesce((SELECT position FROM queue_positions q WHERE q.kind='stream' AND q.source_id=s.id),0),s.id"
	} else {
		q += " ORDER BY "
		if s.UHDAllowed() {
			q += "(instr(upper(s.name),'UHD')>0) DESC,"
		}
		q += "coalesce((SELECT position FROM queue_positions q WHERE q.kind='stream' AND q.source_id=s.id),0),s.id"
	}
	q += " LIMIT ?"
	args = append(args, max(1, min(200, limit)))
	return s.readCandidates(q, args...)
}
func (s *Store) Claim(kind string, id, now int64) bool {
	res, e := s.DB.Exec(`INSERT INTO work_leases(kind,resource,until) VALUES(?,?,?) ON CONFLICT(kind,resource) DO UPDATE SET until=excluded.until WHERE work_leases.until<=?`, kind, id, now+120, now)
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}
func (s *Store) ReleaseClaim(kind string, id int64) {
	s.DB.Exec("DELETE FROM work_leases WHERE kind=? AND resource=?", kind, id)
}
