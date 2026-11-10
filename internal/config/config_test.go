package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistentIdentityAndSecret(t *testing.T) {
	d := t.TempDir()
	a, e := Load(d)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Load(d)
	if e != nil || a.DeviceID != b.DeviceID {
		t.Fatal("identity changed")
	}
	p, e := AdminPassword(d)
	if e != nil || p != "admin" {
		t.Fatal("missing password")
	}
	q, _ := AdminPassword(d)
	if p != q {
		t.Fatal("password changed")
	}
	st, _ := os.Stat(filepath.Join(d, "secrets/admin-password"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("secret permissions")
	}
	b.Channels[1].Number = b.Channels[0].Number
	if Validate(b) == nil {
		t.Fatal("duplicate number accepted")
	}
}

func TestSettingsDefaultsAndSavedRestartConfiguration(t *testing.T) {
	root := t.TempDir()
	c, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	if c.Settings.StartupRace != 5 {
		t.Fatal(c.Settings)
	}
	c.Settings.StallTimeoutMS = 1000
	if e = Save(root, c); e != nil {
		t.Fatal(e)
	}
	next, e := Load(root)
	if e != nil || next.Settings.StallTimeoutMS != 1000 || next.DeviceID != c.DeviceID {
		t.Fatal(next, e)
	}
	next.Settings.GlobalConnections = 1
	if next.Settings.Validate() == nil {
		t.Fatal("invalid budget accepted")
	}
}

func TestExistingBootstrapAndCustomLoginArePreserved(t *testing.T) {
	root := t.TempDir()
	Load(root)
	const existing = "existing-bootstrap-password"
	path := filepath.Join(root, "secrets/admin-password")
	os.WriteFile(path, []byte(existing), 0600)
	p, e := AdminPassword(root)
	if e != nil || p != existing {
		t.Fatal("existing bootstrap changed", e)
	}
	login, e := OpenAdminLogin(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = login.Change(existing, "custom-user", "custom-long-password"); e != nil {
		t.Fatal(e)
	}
	login, e = OpenAdminLogin(root)
	if e != nil || login.Current().Username != "custom-user" || login.Current().Password != "custom-long-password" {
		t.Fatal("custom login changed", e)
	}
}
func TestFreshDefaultCanChangeLoginAndRestart(t *testing.T) {
	root := t.TempDir()
	Load(root)
	login, e := OpenAdminLogin(root)
	if e != nil || login.Current().Username != "admin" || login.Current().Password != "admin" {
		t.Fatal("wrong initial login", e)
	}
	if e = login.Change("admin", "owner", "new-long-password"); e != nil {
		t.Fatal(e)
	}
	login, e = OpenAdminLogin(root)
	if e != nil || login.Current().Username != "owner" || login.Current().Password != "new-long-password" {
		t.Fatal("changed login not persisted", e)
	}
}

func TestFutureFieldsAndIdentitySurviveSave(t *testing.T) {
	root := t.TempDir()
	c, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	id := c.DeviceID
	c.Extensions = map[string]json.RawMessage{"future_input": json.RawMessage(`{"enabled":false}`)}
	if e = Save(root, c); e != nil {
		t.Fatal(e)
	}
	d, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	var ext struct {
		Enabled bool `json:"enabled"`
	}
	err := json.Unmarshal(d.Extensions["future_input"], &ext)
	if d.DeviceID != id || d.ProfileID != "primary" || d.FormatVersion != 1 || err != nil || ext.Enabled {
		t.Fatalf("identity/extension changed: %+v", d)
	}
	d.FormatVersion = 999
	if Save(root, d) == nil {
		t.Fatal("future config silently rewritten")
	}
}

func TestFreshDefaultsDoNotOverwriteExistingSettings(t *testing.T) {
	root := t.TempDir()
	c, err := Load(root)
	if err != nil || c.Settings != FreshSettings() {
		t.Fatalf("fresh settings: %+v %v", c.Settings, err)
	}
	// Fresh values must be persisted, not just returned in memory.
	raw, err := os.ReadFile(filepath.Join(root, "config/config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err = json.Unmarshal(raw, &saved); err != nil || saved.Settings != FreshSettings() {
		t.Fatal("defaults not persisted", err)
	}
	saved.Settings = Settings{}
	saved.Settings.Defaults()
	saved.Settings.MaxOutgoingStreams = 7
	saved.Settings.AllowUHD = true
	if err = Save(root, saved); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "config/config.json"))
	next, err := Load(root)
	after, _ := os.ReadFile(filepath.Join(root, "config/config.json"))
	if err != nil || next.Settings != saved.Settings || string(before) != string(after) {
		t.Fatal("saved settings overwritten", err)
	}
}

func TestObsoleteFullAccountStrikeSettingDoesNotSurviveSave(t *testing.T) {
	var c Config
	if e := json.Unmarshal([]byte(`{"settings":{"count_full_connections_as_failures":true,"roster_target":20}}`), &c); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	var saved map[string]json.RawMessage
	if e = json.Unmarshal(raw, &saved); e != nil {
		t.Fatal(e)
	}
	var settings map[string]json.RawMessage
	if e = json.Unmarshal(saved["settings"], &settings); e != nil {
		t.Fatal(e)
	}
	if _, ok := settings["count_full_connections_as_failures"]; ok {
		t.Fatal("obsolete strike option persisted")
	}
	if c.Settings.RosterTarget != 20 {
		t.Fatal("existing settings changed")
	}
}
