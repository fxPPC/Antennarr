package catalog

import (
	"database/sql"
	"time"
)

// PruneDeadProviders removes a hostname only when every account has been checked
// and is inactive or retired. Suspect, cooling, blocked, and unverified accounts
// are deliberately inconclusive. Call while idle, or before starting playback.
func (s *Store) PruneDeadProviders() (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TEMP TABLE prune_providers AS
 SELECT rate_key FROM accounts GROUP BY rate_key
 HAVING rate_key!='' AND min(last_check)>0 AND sum(state NOT IN ('inactive','retired'))=0
 AND sum(EXISTS(SELECT 1 FROM work_leases l WHERE l.until>? AND
 ((l.kind='account' AND l.resource=accounts.id) OR (l.kind='stream' AND l.resource IN(SELECT id FROM streams WHERE account_id=accounts.id)))))=0`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var count int64
	if err = tx.QueryRow(`SELECT count(*) FROM prune_providers`).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		// Deleted IDs must not be reused by a later import or late playback result.
		if err = preserveHighwater(tx); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(`CREATE TEMP TABLE prune_accounts AS SELECT id FROM accounts WHERE rate_key IN (SELECT rate_key FROM prune_providers);
 CREATE TEMP TABLE prune_streams AS SELECT id FROM streams WHERE account_id IN(SELECT id FROM prune_accounts);`); err != nil {
			return 0, err
		}
		if err = recordCatalogueRemovals(tx, "provider has no usable accounts"); err != nil {
			return 0, err
		}
		for _, q := range []string{
			`DELETE FROM validations WHERE stream_id IN (SELECT id FROM prune_streams)`,
			`DELETE FROM mappings WHERE stream_id IN (SELECT id FROM prune_streams)`,
			`DELETE FROM events WHERE source_id IN (SELECT id FROM prune_streams)`,
			`DELETE FROM streams WHERE id IN (SELECT id FROM prune_streams)`,
			`DELETE FROM provenance WHERE account_id IN (SELECT id FROM prune_accounts)`,
			`DELETE FROM accounts WHERE id IN (SELECT id FROM prune_accounts)`,
			`DELETE FROM provider_rates WHERE provider!='*' AND provider IN (SELECT rate_key FROM prune_providers)`,
			`DROP TABLE prune_streams`, `DROP TABLE prune_accounts`,
		} {
			if _, err = tx.Exec(q); err != nil {
				return 0, err
			}
		}
	}
	if _, err = tx.Exec(`DROP TABLE prune_providers`); err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

func preserveHighwater(tx *sql.Tx) error {
	for _, table := range []string{"accounts", "streams"} {
		if _, e := tx.Exec(`INSERT INTO settings(name,value) VALUES(?,(SELECT coalesce(max(id),0) FROM `+table+`)) ON CONFLICT(name) DO UPDATE SET value=max(CAST(value AS INTEGER),CAST(excluded.value AS INTEGER))`, "deleted_"+table+"_highwater"); e != nil {
			return e
		}
	}
	return nil
}
