package catalog

import (
	"context"
	"database/sql"
	"time"
)

// MonitorSummary is a read-only database snapshot. Counts refer to saved
// verification, not a promise that an idle upstream is currently working.
type CoverageSnapshot struct {
	Vacancies int `json:"vacancies"`
	Total     int `json:"total"`
	Empty     int `json:"empty"`
	Single    int `json:"single"`
	Multiple  int `json:"multiple"`
	Full      int `json:"full"`
	Verified  int `json:"verified"`
	Target    int `json:"target"`
	Overdue   int `json:"overdue"`
}
type QueueSnapshot struct {
	Access    int `json:"access"`
	Discovery int `json:"discovery"`
	Available int `json:"available"`
	Resting   int `json:"resting"`
	InRosters int `json:"in_rosters"`
	Streams   int `json:"streams"`
}
type ProgressSnapshot struct {
	Successful    int   `json:"successful"`
	Failed        int   `json:"failed"`
	Added         int   `json:"added"`
	Removed       int   `json:"removed"`
	LastCheck     int64 `json:"last_check"`
	LastSuccess   int64 `json:"last_success"`
	LastDiscovery int64 `json:"last_discovery"`
	LastAccess    int64 `json:"last_access"`
}
type MonitorSummary struct {
	Pass              CheckPass         `json:"check_pass"`
	Collected         int64             `json:"collected"`
	Channels          map[string]string `json:"channels"`
	Coverage          CoverageSnapshot  `json:"coverage"`
	Queue             QueueSnapshot     `json:"queue"`
	Progress          ProgressSnapshot  `json:"progress"`
	NextProbe         int64             `json:"next_probe"`
	NextDiscovery     int64             `json:"next_discovery"`
	EligibleMedia     bool              `json:"eligible_media"`
	EligibleDiscovery int               `json:"eligible_discovery"`
}

func (s *Store) MonitorSummary(ctx context.Context, now int64) (MonitorSummary, error) {
	var out MonitorSummary
	out.Collected = now
	out.Channels = map[string]string{}
	tx, e := s.DB.Conn(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Close()
	// The writer pool uses BEGIN IMMEDIATE. Explicit DEFERRED keeps this read
	// snapshot from reserving SQLite's writer lock while the dashboard scans.
	if _, e = tx.ExecContext(ctx, "BEGIN DEFERRED"); e != nil {
		return out, e
	}
	defer tx.ExecContext(context.Background(), "ROLLBACK")
	target := 30
	if p := s.rosterPolicy.Load(); p != nil {
		target = p.Target
	}
	rows, e := tx.QueryContext(ctx, `SELECT c.id,c.name,coalesce(nullif(p.target,0),?),count(r.stream_id),0 FROM active_channels c LEFT JOIN channel_policies p ON p.channel_id=c.id LEFT JOIN roster r ON r.channel_id=c.id WHERE c.enabled=1 GROUP BY c.id`, target)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var t, n, d int
		var id, name string
		if e = rows.Scan(&id, &name, &t, &n, &d); e != nil {
			rows.Close()
			return out, e
		}
		out.Channels[id] = name
		out.Coverage.Total++
		out.Coverage.Target += t
		out.Coverage.Verified += n
		out.Coverage.Vacancies += max(0, t-n)
		out.Coverage.Overdue += d
		switch n {
		case 0:
			out.Coverage.Empty++
		case 1:
			out.Coverage.Single++
		default:
			out.Coverage.Multiple++
		}
		if n >= t {
			out.Coverage.Full++
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	// Each eligible account belongs to exactly one stage; streams are a separate unit.
	rows, e = tx.QueryContext(ctx, `SELECT CASE WHEN EXISTS(SELECT 1 FROM roster r WHERE r.account_id=a.id) THEN 'roster' WHEN EXISTS(SELECT 1 FROM provider_rates p WHERE p.provider=a.rate_key AND p.blocked_until>?) THEN 'resting' WHEN kind='xtream' AND last_check=0 THEN 'access' WHEN kind='xtream' AND catalogue_checked=0 THEN 'discovery' ELSE 'available' END,count(*) FROM accounts a WHERE state NOT IN ('inactive','retired','manually_blocked') GROUP BY 1`, now*1000)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var k string
		var n int
		if e = rows.Scan(&k, &n); e != nil {
			rows.Close()
			return out, e
		}
		switch k {
		case "roster":
			out.Queue.InRosters = n
		case "access":
			out.Queue.Access = n
		case "resting":
			out.Queue.Resting = n
		case "discovery":
			out.Queue.Discovery = n
		case "available":
			out.Queue.Available = n
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	queries := []struct {
		q    string
		args []any
		dest []any
	}{
		{`SELECT count(*) FROM streams s JOIN accounts a ON a.id=s.account_id WHERE s.enabled=1 AND s.state NOT IN ('retired','manually_blocked') AND a.state NOT IN ('inactive','retired','manually_blocked') AND NOT EXISTS(SELECT 1 FROM roster r WHERE r.stream_id=s.id) AND EXISTS(SELECT 1 FROM mappings m JOIN active_channels c ON c.id=m.channel_id WHERE m.stream_id=s.id AND m.enabled=1 AND c.enabled=1)`, nil, []any{&out.Queue.Streams}},
		{`SELECT coalesce(sum(success=1),0),coalesce(sum(success=0),0) FROM validations WHERE checked>?`, []any{now - 3600}, []any{&out.Progress.Successful, &out.Progress.Failed}},
		{`SELECT coalesce(max(checked),0),coalesce(max(CASE WHEN success=1 THEN checked END),0) FROM validations`, nil, []any{&out.Progress.LastCheck, &out.Progress.LastSuccess}},
		{`SELECT coalesce(max(catalogue_checked),0),coalesce(max(last_check),0) FROM accounts`, nil, []any{&out.Progress.LastDiscovery, &out.Progress.LastAccess}},
		{`SELECT coalesce(sum(kind='roster_added'),0),coalesce(sum(kind='roster_removed'),0) FROM events WHERE happened>?`, []any{now - 3600}, []any{&out.Progress.Added, &out.Progress.Removed}},
		{`SELECT coalesce((SELECT next_probe FROM provider_rates WHERE provider='*'),0)`, nil, []any{&out.NextProbe}},
		{`SELECT count(*) FROM accounts a WHERE kind='xtream' AND state NOT IN ('inactive','retired','manually_blocked') AND catalogue_checked=0 AND NOT EXISTS(SELECT 1 FROM provider_rates p WHERE p.provider=a.rate_key AND max(p.next_api,p.blocked_until)>?)`, []any{now * 1000}, []any{&out.EligibleDiscovery}},
		{`SELECT coalesce(min(max(p.next_api,p.blocked_until)),0) FROM accounts a JOIN provider_rates p ON p.provider=a.rate_key WHERE kind='xtream' AND state NOT IN ('inactive','retired','manually_blocked') AND catalogue_checked=0`, nil, []any{&out.NextDiscovery}},
	}
	for _, q := range queries {
		if e = tx.QueryRowContext(ctx, q.q, q.args...).Scan(q.dest...); e != nil {
			return out, e
		}
	}

	// Unexamined input is reserve stock, not runnable discovery work when the
	// selected rosters are full. Keep queue totals separate from worker eligibility.
	if out.Coverage.Vacancies == 0 {
		out.EligibleDiscovery = 0
		out.NextDiscovery = 0
	}
	// Eligibility ignores only the short global interval; provider cooldowns,
	// account capacity, leases, quality mode, and actual channel vacancies apply.
	// Start with vacancies/pending roster work, rather than scanning every
	// catalogue stream and recomputing channel counts for each candidate.
	e = tx.QueryRowContext(ctx, `WITH vacancies AS MATERIALIZED (
 SELECT c.id FROM active_channels c LEFT JOIN channel_policies cp ON cp.channel_id=c.id
 LEFT JOIN roster r ON r.channel_id=c.id WHERE c.enabled=1 GROUP BY c.id
 HAVING count(r.stream_id)<coalesce(nullif(cp.target,0),?)),
 candidates AS (SELECT r.stream_id FROM roster r JOIN active_channels ac ON ac.id=r.channel_id AND ac.enabled=1 JOIN roster_pass_work pw ON pw.stream_id=r.stream_id WHERE pw.status=0
 UNION SELECT m.stream_id FROM vacancies v JOIN mappings m ON m.channel_id=v.id WHERE m.enabled=1)
 SELECT EXISTS(SELECT 1 FROM candidates x JOIN streams s ON s.id=x.stream_id JOIN accounts a ON a.id=s.account_id
 WHERE s.enabled=1 AND s.state NOT IN ('retired','manually_blocked') AND a.state NOT IN ('inactive','retired','manually_blocked')
 AND a.connection_limit>a.external_connections
 AND NOT EXISTS(SELECT 1 FROM work_leases l WHERE l.resource=s.id AND l.kind='stream' AND l.until>?)
 AND NOT EXISTS(SELECT 1 FROM provider_rates p WHERE p.provider=a.rate_key AND max(p.next_probe,p.blocked_until)>?)`+s.qualitySQL()+`)`, target, now, now*1000).Scan(&out.EligibleMedia)
	if e != nil {
		return out, e
	}
	out.Pass, e = checkPassStatus(monitorQuery{ctx: ctx, conn: tx})
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, "COMMIT")
	return out, e
}

// Background collection is bounded independently of the HTTP request. Large
// existing catalogues need time to return their first snapshot during cleanup.
func (s *Store) ReadMonitor(ctx context.Context) (MonitorSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.MonitorSummary(ctx, time.Now().Unix())
}

// Adapt the context-bound connection to the saved-pass read interface.
type monitorQuery struct {
	ctx  context.Context
	conn *sql.Conn
}

func (q monitorQuery) QueryRow(query string, args ...any) *sql.Row {
	return q.conn.QueryRowContext(q.ctx, query, args...)
}
