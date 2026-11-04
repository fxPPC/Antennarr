package catalog

import "fmt"

// QueueClear removes only unassigned sources. In-use accounts are protected by
// the caller's reservation lock; current roster membership is protected in SQL.
type QueueClear struct {
	Accounts int64 `json:"accounts"`
	Streams  int64 `json:"streams"`
}

func (s *Store) ClearQueue(protected []int64) (QueueClear, error) {
	var out QueueClear
	tx, e := s.DB.Begin()
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("CREATE TEMP TABLE clear_protected(id INTEGER PRIMARY KEY)"); e != nil {
		return out, e
	}
	for _, id := range protected {
		if _, e = tx.Exec("INSERT OR IGNORE INTO clear_protected VALUES(?)", id); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(`CREATE TEMP TABLE clear_streams AS SELECT id FROM streams s WHERE NOT EXISTS(SELECT 1 FROM roster r WHERE r.stream_id=s.id) AND NOT EXISTS(SELECT 1 FROM clear_protected p WHERE p.id=s.account_id)`); e != nil {
		return out, e
	}
	if e = preserveHighwater(tx); e != nil {
		return out, e
	}
	if e = tx.QueryRow("SELECT count(*) FROM clear_streams").Scan(&out.Streams); e != nil {
		return out, e
	}
	if e = detachRemovalEvents(tx, "clear_streams"); e != nil {
		return out, e
	}
	for _, q := range []string{`DELETE FROM validations WHERE stream_id IN (SELECT id FROM clear_streams)`, `DELETE FROM mappings WHERE stream_id IN (SELECT id FROM clear_streams)`, `DELETE FROM events WHERE source_id IN (SELECT id FROM clear_streams)`, `DELETE FROM work_leases WHERE kind IN ('stream','cooldown','deferred','quality_invalid') AND resource IN (SELECT id FROM clear_streams)`, `DELETE FROM streams WHERE id IN (SELECT id FROM clear_streams)`, `CREATE TEMP TABLE clear_accounts AS SELECT id FROM accounts a WHERE NOT EXISTS(SELECT 1 FROM streams s WHERE s.account_id=a.id) AND NOT EXISTS(SELECT 1 FROM clear_protected p WHERE p.id=a.id)`, `DELETE FROM provenance WHERE account_id IN (SELECT id FROM clear_accounts)`, `DELETE FROM work_leases WHERE kind='account' AND resource IN (SELECT id FROM clear_accounts)`} {
		if _, e = tx.Exec(q); e != nil {
			return out, e
		}
	}
	if e = tx.QueryRow("SELECT count(*) FROM clear_accounts").Scan(&out.Accounts); e != nil {
		return out, e
	}
	if out.Accounts+out.Streams > 0 {
		if _, e = tx.Exec("INSERT INTO events(happened,kind,reason) VALUES(unixepoch(),'queue_cleared',?)", fmt.Sprintf("Cleared input queue: deleted %d accounts and %d streams", out.Accounts, out.Streams)); e != nil {
			return out, e
		}
	}
	for _, q := range []string{`DELETE FROM accounts WHERE id IN (SELECT id FROM clear_accounts)`, `DELETE FROM provider_rates WHERE provider!='*' AND NOT EXISTS(SELECT 1 FROM accounts a WHERE a.rate_key=provider_rates.provider)`, `DROP TABLE clear_accounts`, `DROP TABLE clear_streams`, `DROP TABLE clear_protected`} {
		if _, e = tx.Exec(q); e != nil {
			return out, e
		}
	}
	return out, tx.Commit()
}
