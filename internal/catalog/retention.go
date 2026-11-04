package catalog

// Apply the same limits on inserts as during idle housekeeping, so continuous
// viewing cannot grow the histories indefinitely. Deleted SQLite pages are
// reused; no blocking full-database vacuum is needed for routine retention.
const trimEventRowsSQL = `DELETE FROM events WHERE id<(SELECT coalesce(max(id),0) FROM events)-9999`
const trimOldEventsSQL = `DELETE FROM events WHERE happened<unixepoch()-604800`
const trimValidationRowsSQL = `DELETE FROM validations WHERE id<(SELECT coalesce(max(id),0) FROM validations)-49999`

func (s *Store) ensureRetention() error {
	_, err := s.DB.Exec(`CREATE TRIGGER IF NOT EXISTS events_retention AFTER INSERT ON events BEGIN
 DELETE FROM events WHERE id<NEW.id-9999;
 DELETE FROM events WHERE happened<unixepoch()-604800;
 END;
 CREATE TRIGGER IF NOT EXISTS validations_retention AFTER INSERT ON validations BEGIN
 DELETE FROM validations WHERE id<NEW.id-49999;
 END;` + trimEventRowsSQL + `;` + trimOldEventsSQL + `;` + trimValidationRowsSQL + `;`)
	return err
}
