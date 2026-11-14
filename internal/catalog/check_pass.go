package catalog

import (
	"database/sql"
)

// Each pass snapshots unique roster streams oldest-verification first, with
// random ties. Input queue ordering remains random. Provider pacing
// leaves work pending, and released memberships are counted separately.
// These tables preserve progress through restarts and backups.
func (s *Store) ensureCheckPass() error {
	_, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS roster_pass (
 id INTEGER PRIMARY KEY CHECK(id=1),generation INTEGER NOT NULL,started INTEGER NOT NULL,
 total INTEGER NOT NULL,checked INTEGER NOT NULL,last_seconds INTEGER NOT NULL DEFAULT 0,
 last_checked INTEGER NOT NULL DEFAULT 0,last_skipped INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS roster_pass_work (
 stream_id INTEGER PRIMARY KEY REFERENCES streams(id) ON DELETE CASCADE,
 position INTEGER NOT NULL,status INTEGER NOT NULL DEFAULT 0 CHECK(status IN(0,1,2)));
 CREATE INDEX IF NOT EXISTS roster_pass_pending ON roster_pass_work(status,position);
 CREATE TABLE IF NOT EXISTS roster_pass_accounts (
 account_id INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE);`)
	return e
}

type CheckPass struct {
	Deferred     int   `json:"deferred"`
	Generation   int64 `json:"generation"`
	Started      int64 `json:"started"`
	Total        int   `json:"total"`
	Checked      int   `json:"checked"`
	Remaining    int   `json:"remaining"`
	Released     int   `json:"released"`
	LastSeconds  int64 `json:"last_seconds"`
	LastChecked  int   `json:"last_checked"`
	LastReleased int   `json:"last_released"`
}

func (s *Store) CheckPassStatus() (CheckPass, error) { return checkPassStatus(s.DB) }
func checkPassStatus(q interface{ QueryRow(string, ...any) *sql.Row }) (CheckPass, error) {
	var p CheckPass
	e := q.QueryRow(`SELECT generation,started,total,checked,last_seconds,last_checked,last_skipped,
 (SELECT count(*) FROM roster_pass_work WHERE status=0),
 (SELECT count(*) FROM roster_pass_work pw WHERE pw.status=2 AND EXISTS(SELECT 1 FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 WHERE r.stream_id=pw.stream_id)) FROM roster_pass WHERE id=1`).Scan(&p.Generation, &p.Started, &p.Total, &p.Checked, &p.LastSeconds, &p.LastChecked, &p.LastReleased, &p.Remaining, &p.Deferred)
	if e == sql.ErrNoRows {
		return p, nil
	}
	p.Released = max(0, p.Total-p.Checked-p.Remaining-p.Deferred)
	return p, e
}
func (s *Store) EnsureRosterPass(now int64) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// A released membership leaves the pass; pacing alone never completes work.
	if _, e = tx.Exec(`UPDATE roster_pass_work SET status=0 WHERE status=2 AND EXISTS(SELECT 1 FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 JOIN accounts a ON a.id=r.account_id WHERE r.stream_id=roster_pass_work.stream_id AND a.external_connections<a.connection_limit); DELETE FROM roster_pass_accounts WHERE account_id IN(SELECT id FROM accounts WHERE external_connections>=connection_limit)`); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE roster_pass_work SET status=2 WHERE status=0 AND NOT EXISTS(SELECT 1 FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 WHERE r.stream_id=roster_pass_work.stream_id)`); e != nil {
		return e
	}
	p, e := checkPassStatus(tx)
	if e != nil {
		return e
	}
	if p.Generation > 0 && p.Remaining > 0 {
		return tx.Commit()
	}
	var n int
	if e = tx.QueryRow("SELECT count(DISTINCT r.stream_id) FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1").Scan(&n); e != nil {
		return e
	}
	if n == 0 {
		if _, e = tx.Exec("DELETE FROM roster_pass_work; DELETE FROM roster_pass_accounts; DELETE FROM roster_pass"); e != nil {
			return e
		}
		return tx.Commit()
	}
	if _, e = tx.Exec("DELETE FROM roster_pass_work; DELETE FROM roster_pass_accounts"); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO roster_pass_work(stream_id,position) SELECT r.stream_id,row_number() OVER (ORDER BY min(r.verified),random()) FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 GROUP BY r.stream_id`); e != nil {
		return e
	}
	elapsed := int64(0)
	if p.Generation > 0 {
		elapsed = max(0, now-p.Started)
	}
	_, e = tx.Exec(`INSERT INTO roster_pass VALUES(1,?,?,?,0,?,?,?) ON CONFLICT(id) DO UPDATE SET generation=excluded.generation,started=excluded.started,total=excluded.total,checked=0,last_seconds=excluded.last_seconds,last_checked=excluded.last_checked,last_skipped=excluded.last_skipped`, p.Generation+1, now, n, elapsed, p.Checked, p.Released)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// NextRosterCheck deliberately allows accounts with stale full-slot metadata.
// Their access/connection metadata must be refreshed once per pass before media.
func (s *Store) NextRosterCheck(now int64) (Candidate, bool, error) {
	q := `SELECT s.id,s.account_id,a.provider,a.connection_limit-a.external_connections,s.secret,s.codec,s.score,s.last_success,instr(upper(s.name),'UHD')>0
 FROM roster_pass_work w JOIN streams s ON s.id=w.stream_id JOIN accounts a ON a.id=s.account_id
 WHERE w.status=0 AND s.enabled=1 AND s.state NOT IN('retired','manually_blocked') AND a.state NOT IN('retired','inactive','manually_blocked')
 AND EXISTS(SELECT 1 FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 WHERE r.stream_id=s.id)
 AND (a.connection_limit>a.external_connections OR NOT EXISTS(SELECT 1 FROM roster_pass_accounts pa WHERE pa.account_id=a.id))
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE ((l.kind='stream' AND l.resource=s.id) OR(l.kind='account' AND l.resource=a.id)) AND l.until>?)
 AND NOT EXISTS(SELECT 1 FROM provider_rates p WHERE p.provider=a.rate_key AND
 (p.blocked_until>? OR (NOT EXISTS(SELECT 1 FROM roster_pass_accounts pa WHERE pa.account_id=a.id) AND p.next_api>?) OR(EXISTS(SELECT 1 FROM roster_pass_accounts pa WHERE pa.account_id=a.id) AND p.next_probe>?)))`
	q += s.qualitySQL() + " ORDER BY w.position,s.id LIMIT 1"
	rows, e := s.readCandidates(q, now, now*1000, now*1000, now*1000)
	if e != nil {
		return Candidate{}, false, e
	}
	if len(rows) == 0 {
		return Candidate{}, false, sql.ErrNoRows
	}
	var seen int
	e = s.DB.QueryRow("SELECT count(*) FROM accounts a WHERE id=? AND kind='xtream' AND NOT EXISTS(SELECT 1 FROM roster_pass_accounts pa WHERE pa.account_id=a.id)", rows[0].AccountID).Scan(&seen)
	return rows[0], seen > 0, e
}
func (s *Store) MarkPassAccount(id int64) error {
	_, e := s.DB.Exec("INSERT OR IGNORE INTO roster_pass_accounts VALUES(?)", id)
	return e
}
func (s *Store) FinishRosterCheck(id int64) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.Exec("UPDATE roster_pass_work SET status=1 WHERE stream_id=? AND status=0", id)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n > 0 {
		if _, e = tx.Exec("UPDATE roster_pass SET checked=checked+? WHERE id=1", n); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// NextRosterCheckAt describes the earliest saved provider/lease window for the
// pending pass. It does not reserve a slot or shorten any upstream cooldown.
func (s *Store) NextRosterCheckAt(now int64) (int64, error) {
	var at int64
	err := s.DB.QueryRow(`SELECT coalesce(min(max(coalesce(p.blocked_until,0),
 CASE WHEN EXISTS(SELECT 1 FROM roster_pass_accounts pa WHERE pa.account_id=a.id) THEN coalesce(p.next_probe,0) ELSE coalesce(p.next_api,0) END,
 coalesce((SELECT max(l.until)*1000 FROM work_leases l WHERE (l.kind='stream' AND l.resource=s.id) OR(l.kind='account' AND l.resource=a.id)),0))),0)
 FROM roster_pass_work w JOIN streams s ON s.id=w.stream_id JOIN accounts a ON a.id=s.account_id LEFT JOIN provider_rates p ON p.provider=a.rate_key
 WHERE w.status=0 AND s.enabled=1 AND s.state NOT IN('retired','manually_blocked') AND a.state NOT IN('retired','inactive','manually_blocked')
 AND EXISTS(SELECT 1 FROM roster r JOIN active_channels c ON c.id=r.channel_id AND c.enabled=1 WHERE r.stream_id=s.id)` + s.qualitySQL()).Scan(&at)
	if err != nil {
		return 0, err
	}
	if at <= now*1000 {
		return 0, nil
	}
	return (at + 999) / 1000, nil
}
