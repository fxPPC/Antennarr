package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCustomAdminLoginAtomicPersistenceAndValidation(t *testing.T) {
	root := t.TempDir()
	Load(root)
	l, e := OpenAdminLogin(root)
	if e != nil {
		t.Fatal(e)
	}
	old := l.Current()
	if old.Username != "admin" {
		t.Fatal("legacy username lost")
	}
	if e = l.Change("wrong-current-password", "viewer-admin", "fixture-new-password"); e != ErrCurrentPassword {
		t.Fatal("accepted wrong current password")
	}
	for _, username := range []string{"", "bad:name", " spaced", "line\nname"} {
		if e = l.Change(old.Password, username, "fixture-new-password"); e == nil {
			t.Fatal("invalid username accepted")
		}
	}
	if e = l.Change(old.Password, "viewer-admin", "short"); e == nil {
		t.Fatal("weak password accepted")
	}
	if e = l.Change(old.Password, "viewer-admin", "fixture-new-password"); e != nil {
		t.Fatal(e)
	}
	reloaded, e := OpenAdminLogin(root)
	if e != nil || reloaded.Current().Username != "viewer-admin" || reloaded.Current().Password != "fixture-new-password" {
		t.Fatal("login did not persist")
	}
	if e = l.Change(old.Password, "other", "fixture-next-password"); e != ErrCurrentPassword {
		t.Fatal("old password remained valid")
	}
	if e = l.Change("fixture-new-password", "renamed-admin", ""); e != nil {
		t.Fatal(e)
	}
	if l.Current().Password != "fixture-new-password" {
		t.Fatal("username-only change replaced password")
	}
	st, e := os.Stat(filepath.Join(root, "secrets/admin-login.json"))
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("login file permissions")
	}
	before := l.Current()
	os.Rename(filepath.Join(root, "secrets"), filepath.Join(root, "old-secrets"))
	if e = l.Change(before.Password, "cannot-save", ""); e == nil {
		t.Fatal("expected save failure")
	}
	if l.Current() != before {
		t.Fatal("save failure changed active credentials")
	}
}
