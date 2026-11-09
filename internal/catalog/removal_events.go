package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Removal notices outlive their source rows. Only IDs, provider hostnames, fixed failure categories, and
// counts are retained; the existing bounded event history limits their lifetime.
func recordCatalogueRemovals(tx *sql.Tx, providerReason string, reserveTrim ...bool) error {
	if err := detachRemovalEvents(tx, "prune_streams"); err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO events(happened,kind,reason)
 SELECT unixepoch(),'account_removed','Deleted account #'||a.id||': '||
 CASE WHEN ?!='' THEN ? WHEN a.state='retired' AND a.failures>0 THEN a.failures||' consecutive failed account checks' WHEN a.state='retired' THEN 'account retired' ELSE 'no remaining matching streams' END||
 '; '||(SELECT count(*) FROM streams s WHERE s.account_id=a.id)||' streams removed; provider '||a.provider||'; last failure '||CASE WHEN a.last_failure_kind='' THEN 'not recorded' ELSE a.last_failure_kind END||CASE WHEN a.last_failure_phase='' THEN '' ELSE ' ('||a.last_failure_phase||')' END||CASE WHEN a.last_failure_status=0 THEN '' ELSE ' HTTP '||a.last_failure_status END
 FROM accounts a JOIN prune_accounts p ON p.id=a.id`, providerReason, providerReason)
	if err != nil {
		return err
	}
	// Count the notices actually inserted, in the same transaction as deletion.
	// This single write path prevents counters and notices drifting independently.
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n > 0 {
		if _, e = tx.Exec("INSERT INTO settings(name,value) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=CAST(value AS INTEGER)+CAST(excluded.value AS INTEGER)", fmt.Sprintf("accounts_retired_minute_%d", time.Now().Unix()/60), n); e != nil {
			return e
		}
	}
	query := `INSERT INTO events(happened,kind,reason)
 SELECT unixepoch(),'stream_removed','Deleted stream #'||s.id||' from account #'||s.account_id||': '||CASE WHEN s.consecutive_failures>0 THEN s.consecutive_failures||' consecutive failed media checks' ELSE 'stream retired' END
 FROM streams s JOIN prune_streams p ON p.id=s.id WHERE NOT EXISTS(SELECT 1 FROM prune_accounts a WHERE a.id=s.account_id)`
	if len(reserveTrim) > 0 && reserveTrim[0] {
		query += " AND NOT EXISTS(SELECT 1 FROM excess_streams x WHERE x.id=s.id)"
	}
	if _, err = tx.Exec(query); err != nil {
		return err
	}
	if len(reserveTrim) > 0 && reserveTrim[0] {
		_, err = tx.Exec(`INSERT INTO events(happened,kind,reason) SELECT unixepoch(),'catalogue_trimmed',count(*)||' unused candidate streams removed; account records retained for rediscovery' FROM excess_streams HAVING count(*)>0`)
	}
	return err
}

// Before deleting a stream, detach its roster-removal notices from the source
// reference and retain the old ID as plain text for the UI's removed-ID link.
func detachRemovalEvents(tx *sql.Tx, table string) error {
	if table != "prune_streams" && table != "clear_streams" {
		return errors.New("invalid removal table")
	}
	_, err := tx.Exec(`UPDATE events SET reason=reason||' · stream #'||source_id,source_id=0 WHERE kind='roster_removed' AND source_id IN(SELECT id FROM ` + table + `)`)
	return err
}
