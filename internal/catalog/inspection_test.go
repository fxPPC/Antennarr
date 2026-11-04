package catalog

import "errors"

func (s *Store) Accounts() ([]Account, error) {
	rows, e := s.DB.Query(`SELECT a.id,a.provider,a.kind,a.state,a.connection_limit,a.last_check,count(s.id),a.created,a.failures,coalesce(sum(s.last_success>0),0),coalesce(sum(s.state='healthy' AND s.enabled=1),0),a.check_successes FROM accounts a LEFT JOIN streams s ON s.account_id=a.id GROUP BY a.id ORDER BY a.id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if e = rows.Scan(&a.ID, &a.Provider, &a.Kind, &a.State, &a.Limit, &a.LastCheck, &a.Streams, &a.Created, &a.Failures, &a.SuccessfulStreams, &a.HealthyStreams, &a.SuccessfulChecks); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Streams(search string) ([]Stream, error) {
	if len(search) > 120 {
		return nil, errors.New("search too long")
	}
	rows, e := s.DB.Query(`SELECT s.id,s.account_id,s.name,s.tvg_id,s.region,s.state,s.score,s.last_success,s.enabled,a.provider FROM streams s JOIN accounts a ON a.id=s.account_id WHERE s.name LIKE ? OR s.tvg_id LIKE ? OR a.provider LIKE ? ORDER BY s.score DESC,s.id LIMIT 1000`, "%"+search+"%", "%"+search+"%", "%"+search+"%")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Stream{}
	for rows.Next() {
		var v Stream
		if e = rows.Scan(&v.ID, &v.AccountID, &v.Name, &v.TVGID, &v.Region, &v.State, &v.Score, &v.LastSuccess, &v.Enabled, &v.Provider); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Quarantine(account int64, blocked bool) error {
	state := "unverified"
	if blocked {
		state = "manually_blocked"
	}
	_, e := s.DB.Exec("UPDATE accounts SET state=?,failures=0,next_check=0,check_cycle=0 WHERE id=?", state, account)
	return e
}
func (s *Store) Assignments() ([]Assignment, error) {
	rows, e := s.DB.Query(`SELECT m.channel_id,s.id,s.account_id,s.name,a.provider,m.enabled,m.origin,CASE WHEN a.state IN ('retired','manually_blocked','inactive') THEN a.state ELSE s.state END,s.score,s.last_success FROM mappings m JOIN streams s ON s.id=m.stream_id JOIN accounts a ON a.id=s.account_id ORDER BY m.channel_id,s.score DESC,s.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		var v Assignment
		if e = rows.Scan(&v.ChannelID, &v.StreamID, &v.AccountID, &v.Name, &v.Provider, &v.Enabled, &v.Origin, &v.State, &v.Score, &v.LastSuccess); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Assignment struct {
	ChannelID   string  `json:"channel_id"`
	StreamID    int64   `json:"stream_id"`
	AccountID   int64   `json:"account_id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	Enabled     bool    `json:"enabled"`
	Origin      string  `json:"origin"`
	State       string  `json:"state"`
	Score       float64 `json:"score"`
	LastSuccess int64   `json:"last_success"`
}

type Stream struct {
	ID          int64   `json:"id"`
	AccountID   int64   `json:"account_id"`
	Name        string  `json:"name"`
	TVGID       string  `json:"tvg_id"`
	Region      string  `json:"region"`
	State       string  `json:"state"`
	Score       float64 `json:"score"`
	LastSuccess int64   `json:"last_success"`
	Enabled     bool    `json:"enabled"`
	Provider    string  `json:"provider"`
}
