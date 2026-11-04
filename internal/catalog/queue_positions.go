package catalog

// Optional schema-8 metadata: older v1 readers ignore this table. Queue order is
// persisted, so pagination, a restart, or a repeated poll never reshuffles it.
func (s *Store) ensureQueuePositions() error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS queue_positions(kind TEXT NOT NULL CHECK(kind IN('account','stream')),source_id INTEGER NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(kind,source_id));
 CREATE INDEX IF NOT EXISTS roster_account ON roster(account_id);
 CREATE INDEX IF NOT EXISTS queue_positions_order ON queue_positions(kind,position,source_id);
 CREATE TRIGGER IF NOT EXISTS queue_account_add AFTER INSERT ON accounts BEGIN INSERT OR IGNORE INTO queue_positions VALUES('account',new.id,random() & 9223372036854775807); END;
 CREATE TRIGGER IF NOT EXISTS queue_stream_add AFTER INSERT ON streams BEGIN INSERT OR IGNORE INTO queue_positions VALUES('stream',new.id,random() & 9223372036854775807); END;
 CREATE TRIGGER IF NOT EXISTS queue_account_remove AFTER DELETE ON accounts BEGIN DELETE FROM queue_positions WHERE kind='account' AND source_id=old.id; END;
 CREATE TRIGGER IF NOT EXISTS queue_stream_remove AFTER DELETE ON streams BEGIN DELETE FROM queue_positions WHERE kind='stream' AND source_id=old.id; END;
 INSERT OR IGNORE INTO queue_positions SELECT 'account',id,random() & 9223372036854775807 FROM accounts;
 INSERT OR IGNORE INTO queue_positions SELECT 'stream',id,random() & 9223372036854775807 FROM streams;
 DELETE FROM queue_positions WHERE (kind='account' AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=source_id)) OR (kind='stream' AND NOT EXISTS(SELECT 1 FROM streams WHERE id=source_id));
 DELETE FROM work_leases WHERE kind='cooldown';`)
	return err
}
