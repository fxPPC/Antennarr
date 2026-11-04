package catalog

import (
	"context"
	"errors"
	"time"
)

// Validate before pruning on startup. Integrity failure never silently discards
// the catalogue or substitutes an empty database; the operator keeps the files.
func (s *Store) checkIntegrity() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result string
	if e := s.DB.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return errors.New("catalogue integrity check failed")
	}
	rows, e := s.DB.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("catalogue contains inconsistent references")
	}
	return rows.Err()
}
