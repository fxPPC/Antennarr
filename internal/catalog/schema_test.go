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
	s, e := OpenWithDefaults(root)
	if e != nil {
		t.Fatal(e)
	}
	addTestPresets(t, s, "us-nyc", "uk-london")
	channels, e := s.Channels()
	if e != nil || len(channels) != 258 {
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
	if version != SchemaVersion {
		t.Fatal(version)
	}
}
func TestOlderAndUnversionedDatabasesArePreserved(t *testing.T) {
	for _, v := range []int{0, 1, 7, SchemaVersion + 1} {
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

func TestCatalogueUsesItsOwnDatabaseDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "database")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if s, err := Open(root); err == nil {
		s.Close()
		t.Fatal("opened a database outside the appdata root")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("unexpected external files", entries, err)
	}
}

func TestInterruptedGuideUpgradeCanRetry(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the 1.1 layout, then introduce a conflicting table partway
	// through the new migration. Nothing from a partial attempt may persist.
	_, err = store.DB.Exec(`DROP TABLE guide_feeds; DROP TABLE guide_channels; DROP TABLE guide_stations;
		CREATE TABLE guide_channels(keep TEXT); INSERT INTO guide_channels VALUES ('original');
		PRAGMA user_version=9`)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(root); err == nil {
		reopened.Close()
		t.Fatal("conflicting schema accepted")
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "database", "antennarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, partial int
	var retained string
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 9 {
		t.Fatal("failed migration changed the version", version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='guide_feeds'").Scan(&partial); err != nil || partial != 0 {
		t.Fatal("partial guide tables survived", partial, err)
	}
	if err = db.QueryRow("SELECT keep FROM guide_channels").Scan(&retained); err != nil || retained != "original" {
		t.Fatal("existing data changed", retained, err)
	}
	if _, err = db.Exec("DROP TABLE guide_channels"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("migration did not resume", version, err)
	}
}
