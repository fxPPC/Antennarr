package catalog

import "database/sql"

func (s *Store) RefreshMatching() error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revision string
	e = tx.QueryRow("SELECT value FROM settings WHERE name='matching_revision'").Scan(&revision)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if revision == matchingRevision {
		return tx.Commit()
	}
	if e = autoMap(tx); e != nil {
		return e
	}
	return tx.Commit()
}
