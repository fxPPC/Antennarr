package catalog

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestEnableWALWithReader(t *testing.T) {
	for _, release := range []bool{true, false} {
		name := "persistent"
		if release {
			name = "transient"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalogue.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("CREATE TABLE channels (id INTEGER)"); err != nil {
				t.Fatal(err)
			}
			reader, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Rollback()
			var rows int
			if err := reader.QueryRow("SELECT count(*) FROM channels").Scan(&rows); err != nil {
				t.Fatal(err)
			}
			// The reader keeps a shared lock. The other connection returns BUSY
			// immediately, so recovery requires retrying the journal mode change.
			wait := 350 * time.Millisecond
			if release {
				wait = 5 * time.Second
			}
			done := make(chan error, 1)
			go func() { done <- enableWAL(db, wait) }()
			if release {
				select {
				case err := <-done:
					t.Fatalf("WAL change ended with the reader still active: %v", err)
				case <-time.After(150 * time.Millisecond):
				}
				if err := reader.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				var se *sqlite.Error
				if release && err != nil {
					t.Fatal(err)
				}
				if !release && (!errors.As(err, &se) || se.Code()&0xff != sqlite3.SQLITE_BUSY) {
					t.Fatalf("want SQLite busy, got %v", err)
				}
			case <-time.After(wait + time.Second):
				t.Fatal("WAL change exceeded the wait limit")
			}
		})
	}
}
