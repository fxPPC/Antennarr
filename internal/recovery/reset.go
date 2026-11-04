package recovery

import (
	"errors"
	"os"
	"path/filepath"
)

const resetMarker = "factory-reset-v1"

// Reset is staged until the next startup, after all old SQLite handles close.
// Manual backup archives are deliberately retained, never created implicitly.
func StageReset(root string) error {
	if _, e := os.Stat(filepath.Join(root, "restore-pending")); e == nil {
		return errors.New("restore already pending")
	}
	return os.WriteFile(filepath.Join(root, "reset-request"), []byte(resetMarker), 0600)
}
func ApplyReset(root string) error {
	p := filepath.Join(root, "reset-request")
	st, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return errors.New("invalid reset request")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	if string(b) != resetMarker {
		return errors.New("invalid reset request")
	}
	for _, d := range []string{"config", "database", "secrets", "sources", "logs"} {
		if e = os.RemoveAll(filepath.Join(root, d)); e != nil {
			return e
		}
	}
	return os.Remove(p)
}
