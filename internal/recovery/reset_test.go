package recovery

import (
	"antennarr/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestFactoryResetKeepsManualBackupsAndChangesIdentity(t *testing.T) {
	root := t.TempDir()
	old, e := config.Load(root)
	if e != nil {
		t.Fatal(e)
	}
	os.MkdirAll(filepath.Join(root, "backups"), 0700)
	os.WriteFile(filepath.Join(root, "backups/manual.zip"), []byte("manual"), 0600)
	if e = StageReset(root); e != nil {
		t.Fatal(e)
	}
	if e = ApplyReset(root); e != nil {
		t.Fatal(e)
	}
	if e = ApplyReset(root); e != nil {
		t.Fatal(e)
	}
	next, e := config.Load(root)
	if e != nil || next.DeviceID == old.DeviceID {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "backups/manual.zip")); e != nil {
		t.Fatal(e)
	}
}
