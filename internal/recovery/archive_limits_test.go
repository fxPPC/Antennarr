package recovery

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"antennarr/internal/catalog"
	"antennarr/internal/config"
)

func TestCreateCountsManifestInExpandedLimit(t *testing.T) {
	root := t.TempDir()
	if _, err := config.Load(root); err != nil {
		t.Fatal(err)
	}
	if _, err := config.AdminPassword(root); err != nil {
		t.Fatal(err)
	}
	s, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const base = "http://fixture.invalid"
	name, err := Create(root, base, s)
	if err != nil {
		t.Fatal(err)
	}
	f, err := zip.OpenReader(filepath.Join(root, "backups", name))
	if err != nil {
		t.Fatal(err)
	}
	var expanded int64
	for _, entry := range f.File {
		expanded += int64(entry.UncompressedSize64)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := create(root, base, s, expanded-1); err == nil {
		t.Fatal("created a backup one byte above the expanded limit")
	}
	name, err = create(root, base, s, expanded)
	if err != nil {
		t.Fatalf("backup at the expanded limit: %v", err)
	}
	archive, err := os.Open(filepath.Join(root, "backups", name))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	st, err := archive.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Stage(t.TempDir(), archive, st.Size()); err != nil {
		t.Fatalf("restore refused the boundary backup: %v", err)
	}
}
