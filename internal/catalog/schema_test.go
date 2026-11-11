package catalog

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentSchemaReopenPreservesCatalogue(t *testing.T) {
	root := t.TempDir()
	s, e := OpenWithUSDefaults(root)
	if e != nil {
		t.Fatal(e)
	}
	channels, e := s.Channels()
	if e != nil || len(channels) != 150 {
		t.Fatal(len(channels), e)
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	again, e := s.Channels()
	if e != nil || len(again) != len(channels) || again[0].ID != channels[0].ID {
		t.Fatal("channel identity changed", e)
	}
	var version int
	s.DB.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 8 {
		t.Fatal(version)
	}
}
func TestOlderAndUnversionedDatabasesArePreserved(t *testing.T) {
	for _, v := range []int{0, 1, 7, 9} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, "database"), 0700)
			db, e := sql.Open("sqlite", filepath.Join(root, "database/antennarr.db"))
			if e != nil {
				t.Fatal(e)
			}
			db.Exec("CREATE TABLE preserved(value TEXT); INSERT INTO preserved VALUES('keep')")
			db.Exec(fmt.Sprintf("PRAGMA user_version=%d", v))
			db.Close()
			s, e := Open(root)
			if e == nil {
				s.Close()
				t.Fatal("unsupported database accepted")
			}
			if v > 0 && v < 8 && !strings.Contains(e.Error(), "Beta 2") {
				t.Fatal(e)
			}
			db, _ = sql.Open("sqlite", filepath.Join(root, "database/antennarr.db"))
			defer db.Close()
			var value string
			if e = db.QueryRow("SELECT value FROM preserved").Scan(&value); e != nil || value != "keep" {
				t.Fatal("existing data changed", value, e)
			}
		})
	}
}
