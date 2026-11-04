package catalog

import (
	"log/slog"
	"time"
)

// Additive v1 metadata: old images ignore these columns, and manual backups
// preserve them. No credentials or unbounded failure archive is added.
func (s *Store) ensureAccountDiagnostics() error {
	rows, e := s.DB.Query("PRAGMA table_info(accounts)")
	if e != nil {
		return e
	}
	cols := map[string]bool{}
	for rows.Next() {
		var n, pk, required int
		var name, kind string
		var d any
		if e = rows.Scan(&n, &name, &kind, &required, &d, &pk); e != nil {
			rows.Close()
			return e
		}
		cols[name] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, c := range []struct{ name, definition string }{{"last_failure_kind", "TEXT NOT NULL DEFAULT ''"}, {"last_failure_phase", "TEXT NOT NULL DEFAULT ''"}, {"last_failure_status", "INTEGER NOT NULL DEFAULT 0"}} {
		if !cols[c.name] {
			if _, e = s.DB.Exec("ALTER TABLE accounts ADD COLUMN " + c.name + " " + c.definition); e != nil {
				return e
			}
		}
	}
	return s.repairCatalogueLimitStrikes()
}

// Beta 4 counted the local catalogue bounds as provider failures. Clear those
// retained false streaks once; never touch credentials, mappings, or rosters.
// Subsequent genuine failures and explicit full-capacity policies are unaffected.
func (s *Store) repairCatalogueLimitStrikes() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM settings WHERE name='catalogue_limit_strikes_repaired_v1'").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}
	r, err := tx.Exec("UPDATE accounts SET failures=0,state='unverified' WHERE last_failure_phase='catalogue' AND last_failure_kind IN ('oversized_catalogue','oversized_response') AND failures>0 AND state IN ('suspect','retired')")
	if err != nil {
		return err
	}
	changed, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err = tx.Exec("INSERT INTO events(happened,kind,reason) VALUES(?,'account_health_repaired','Cleared retained failure streaks caused by old local catalogue limits; rosters preserved')", time.Now().Unix()); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO settings(name,value) VALUES('catalogue_limit_strikes_repaired_v1','1')"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if changed > 0 {
		slog.Info("legacy local catalogue strikes cleared", "accounts", changed)
	}
	return nil
}
