package catalog

import (
	"fmt"
	"time"
)

// Metadata alone is not a successful discovery. In particular it must not reset
// the failure streak immediately before a broken catalogue request.
func (s *Store) AccountMetadata(id int64, limit int, expires int64, connections int) error {
	_, e := s.DB.Exec(`UPDATE accounts SET connection_limit=?,expires=?,external_connections=?,last_check=?,state=CASE WHEN state='unverified' THEN 'healthy' ELSE state END WHERE id=? AND state NOT IN('retired','manually_blocked')`, max(1, min(20, limit)), expires, max(0, connections), time.Now().Unix(), id)
	return e
}
func (s *Store) DiscoveryFailed(id int64, inactive bool, catalogue ...bool) error {
	return s.DiscoveryFailedReason(id, inactive, "unspecified", "access", 0, catalogue...)
}

// A full account is temporarily unusable, not proof of invalid credentials.
// Keep verified memberships and randomise its queue position without changing
// the genuine-failure streak or invalidating its saved channel catalogue.
func (s *Store) AccountFull(id int64) error {
	return s.accountUnavailable(id, false, false, "full_connections", "access", 0)
}
func (s *Store) DiscoveryFailedReason(id int64, inactive bool, kind, phase string, status int, catalogue ...bool) error {
	return s.accountUnavailable(id, inactive, true, kind, phase, status, catalogue...)
}
func (s *Store) accountUnavailable(id int64, inactive, countFailure bool, kind, phase string, status int, catalogue ...bool) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	p := policyIn(tx)
	var failures int
	if e = tx.QueryRow("SELECT failures FROM accounts WHERE id=?", id).Scan(&failures); e != nil {
		return e
	}
	if countFailure {
		failures++
	}
	state := "suspect"
	if countFailure {
		state = HealthState(failures)
	}
	if inactive && p.AccountFailures == 0 {
		state = "inactive"
	}
	if countFailure && p.AccountFailures > 0 && failures >= p.AccountFailures {
		state = "retired"
	}
	_, e = tx.Exec(`UPDATE accounts SET failures=?,state=?,next_check=0,last_check=?,catalogue_checked=CASE WHEN ? THEN 0 ELSE catalogue_checked END WHERE id=? AND state NOT IN('retired','manually_blocked')`, failures, state, time.Now().Unix(), len(catalogue) > 0 && catalogue[0], id)
	if e != nil {
		return e
	}
	if countFailure || kind != "" {
		if _, e = tx.Exec("UPDATE accounts SET last_failure_kind=?,last_failure_phase=?,last_failure_status=? WHERE id=?", kind, phase, status, id); e != nil {
			return e
		}
	}
	if countFailure || kind == "full_connections" {
		if _, e = tx.Exec("UPDATE roster_pass_work SET status=2 WHERE status=0 AND stream_id IN(SELECT id FROM streams WHERE account_id=?)", id); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("DELETE FROM roster_pass_accounts WHERE account_id=?", id); e != nil {
		return e
	}
	reason := "Account unavailable; recruiting replacement"

	if countFailure {
		if _, e = tx.Exec(`INSERT INTO events(happened,kind,channel_id,source_id,reason) SELECT ?,'roster_removed',channel_id,stream_id,? FROM roster WHERE account_id=?`, time.Now().Unix(), reason, id); e != nil {
			return e
		}
	} else {
		if _, e = tx.Exec("INSERT INTO events(happened,kind,source_id,reason) VALUES(?,'account_deferred',0,?)", time.Now().Unix(), fmt.Sprintf("Account #%d temporarily unavailable; verified rosters retained (%s)", id, kind)); e != nil {
			return e
		}
	}
	if countFailure {
		if _, e = tx.Exec("DELETE FROM roster WHERE account_id=?", id); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("UPDATE queue_positions SET position=random() & 9223372036854775807 WHERE kind='account' AND source_id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}

// A complete access check can reset failures only when a usable catalogue is
// already saved. Catalogue failures clear that marker until discovery succeeds.
func (s *Store) AccountUsable(id int64) error {
	_, e := s.DB.Exec("UPDATE accounts SET failures=0,last_failure_kind='',last_failure_phase='',last_failure_status=0,state='healthy',next_check=0,check_successes=check_successes+1 WHERE id=? AND catalogue_checked>0 AND state NOT IN('retired','manually_blocked')", id)
	return e
}

// A local catalogue bound is not evidence that the provider account is invalid.
func (s *Store) DeferCatalogue(id int64, kind string) error {
	return s.accountUnavailable(id, false, false, kind, "catalogue", 0)
}
