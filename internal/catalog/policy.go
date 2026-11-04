package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
)

type RemovalPolicy struct {
	AccountFailures int `json:"account_failures"`
	StreamFailures  int `json:"stream_failures"`
}

func (s *Store) RemovalPolicy() (RemovalPolicy, error) {
	var p RemovalPolicy
	var raw string
	e := s.DB.QueryRow("SELECT value FROM settings WHERE name='removal_policy'").Scan(&raw)
	if e == sql.ErrNoRows {
		return p, nil
	}
	if e != nil {
		return p, e
	}
	e = json.Unmarshal([]byte(raw), &p)
	return p, e
}
func (s *Store) SaveRemovalPolicy(p RemovalPolicy) error {
	if p.AccountFailures < 0 || p.AccountFailures > 100 || p.StreamFailures < 0 || p.StreamFailures > 100 {
		return errors.New("failure thresholds must be between 0 and 100")
	}
	raw, _ := json.Marshal(p)
	_, e := s.DB.Exec("INSERT INTO settings(name,value) VALUES('removal_policy',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", string(raw))
	return e
}
func policyIn(tx *sql.Tx) RemovalPolicy {
	var p RemovalPolicy
	var raw string
	if tx.QueryRow("SELECT value FROM settings WHERE name='removal_policy'").Scan(&raw) == nil {
		json.Unmarshal([]byte(raw), &p)
	}
	return p
}
