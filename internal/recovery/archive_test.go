package recovery_test

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"antennarr/internal/recovery"
	"antennarr/internal/tuner"
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFullBackupRestorePreservesTunerAndHistory(t *testing.T) {
	root := t.TempDir()
	c, e := config.Load(root)
	if e != nil {
		t.Fatal(e)
	}
	pw, e := config.AdminPassword(root)
	if e != nil {
		t.Fatal(e)
	}
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.SaveChannel(catalog.Channel{Number: "102", Name: "MTV", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	entries, _ := ingest.Parse([]byte("https://provider.example/get.php?username=fixture&password=only-test-password"))
	if _, e = s.Import(entries, "fixture"); e != nil {
		t.Fatal(e)
	}
	stream, _ := ingest.FromURL("https://provider.example/live/fixture/only-test-password/1.ts", "MTV", "")
	if _, e = s.Import([]ingest.Entry{stream}, "fixture"); e != nil {
		t.Fatal(e)
	}
	cs, e := s.Candidates(id, false)
	if e != nil || len(cs) != 1 {
		t.Fatal(cs, e)
	}
	s.Record(cs[0].StreamID, catalog.Result{Success: true})
	if e = s.EnableRosters(catalog.RosterPolicy{Target: 30, Preference: "diversity", Race: 5}); e != nil {
		t.Fatal(e)
	}
	var dueBefore int64
	s.DB.QueryRow("SELECT due FROM roster").Scan(&dueBefore)
	c.Settings.BufferSeconds = 6
	if e = config.Save(root, c); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "sources/imports/saved.txt"), []byte("fixture source"), 0600)
	base := "http://172.17.0.6:9192"
	capture := func(c config.Config, s *catalog.Store) map[string]string {
		server := tuner.Server{Config: c, BaseURL: base, ChannelList: func() []config.Channel {
			chs, _ := s.Channels()
			var out []config.Channel
			for _, ch := range chs {
				out = append(out, config.Channel{ID: ch.ID, Number: ch.Number, Name: ch.Name})
			}
			return out
		}}
		out := map[string]string{}
		for _, path := range []string{"/discover.json", "/lineup.json", "/device.xml"} {
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			out[path] = w.Body.String()
		}
		return out
	}
	before := capture(c, s)
	name, e := recovery.Create(root, base, s)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "backups", name))
	if e != nil {
		t.Fatal(e)
	}
	target := t.TempDir()
	config.Load(target)
	config.AdminPassword(target)
	m, e := recovery.Stage(target, bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	if m.TunerURL != base {
		t.Fatal(m.TunerURL)
	}
	if e = recovery.ApplyPending(target); e != nil {
		t.Fatal(e)
	}
	if e = recovery.ApplyPending(target); e != nil {
		t.Fatal(e)
	} // idempotent completed restore
	restored, e := catalog.Open(target)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	var assignments int
	if e = restored.DB.QueryRow("SELECT count(*) FROM output_assignments WHERE profile_id='primary'").Scan(&assignments); e != nil || assignments != 1 {
		t.Fatal("output assignments lost", assignments, e)
	}
	afterC, e := config.Load(target)
	if e != nil {
		t.Fatal(e)
	}
	var dueAfter int64
	if e = restored.DB.QueryRow("SELECT due FROM roster").Scan(&dueAfter); e != nil || dueAfter != dueBefore {
		t.Fatal("roster deadline lost", dueAfter, e)
	}
	after := capture(afterC, restored)
	for path, want := range before {
		if after[path] != want {
			t.Fatalf("Plex identity changed at %s", path)
		}
	}
	chs, _ := restored.Channels()
	if len(chs) != 1 || chs[0].ID != id {
		t.Fatal(chs)
	}
	var accounts int
	restored.DB.QueryRow("SELECT count(*) FROM accounts").Scan(&accounts)
	if accounts != 1 {
		t.Fatal(accounts)
	}
	if afterC.Settings.BufferSeconds != 6 {
		t.Fatal(afterC.Settings)
	}
	pw2, _ := config.AdminPassword(target)
	if pw2 != pw {
		t.Fatal("admin state lost")
	}
	b, _ := os.ReadFile(filepath.Join(target, "sources/imports/saved.txt"))
	if string(b) != "fixture source" {
		t.Fatal("source files lost")
	}
	s.Close()
}
func TestRejectUnsafeOrIncompleteArchive(t *testing.T) {
	for _, entry := range []string{"../outside", "config/../../outside", "/absolute", "config\\evil", "database/antennarr.db-wal", "config/config.json"} {
		t.Run(entry, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			w, _ := z.Create(entry)
			w.Write([]byte("bad"))
			z.Close()
			root := t.TempDir()
			if _, e := recovery.Stage(root, bytes.NewReader(b.Bytes()), int64(b.Len())); e == nil {
				t.Fatal("accepted invalid archive")
			}
			if _, e := os.Stat(filepath.Join(root, "restore-pending")); !os.IsNotExist(e) {
				t.Fatal("staged rejected archive")
			}
		})
	}
}

func TestFullBackupRestoresCustomAdminLogin(t *testing.T) {
	root := t.TempDir()
	config.Load(root)
	login, e := config.OpenAdminLogin(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = login.Change(login.Current().Password, "custom-admin", "fixture-custom-password"); e != nil {
		t.Fatal(e)
	}
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	name, e := recovery.Create(root, "http://172.17.0.6:9192", s)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "backups", name))
	if e != nil {
		t.Fatal(e)
	}
	target := t.TempDir()
	if _, e = recovery.Stage(target, bytes.NewReader(raw), int64(len(raw))); e != nil {
		t.Fatal(e)
	}
	if e = recovery.ApplyPending(target); e != nil {
		t.Fatal(e)
	}
	restored, e := config.OpenAdminLogin(target)
	if e != nil || restored.Current() != login.Current() {
		t.Fatal("custom login lost on restore")
	}
}

func TestArchiveStagingRejectsSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	marker := filepath.Join(outside, "untouched")
	if e := os.WriteFile(marker, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "config/link", Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0777)
	w, e := z.CreateHeader(h)
	if e != nil {
		t.Fatal(e)
	}
	w.Write([]byte(outside))
	w, _ = z.Create("config/link/untouched")
	w.Write([]byte("overwritten"))
	z.Close()
	root := t.TempDir()
	if _, e := recovery.Stage(root, bytes.NewReader(b.Bytes()), int64(b.Len())); e == nil {
		t.Fatal("accepted an archive symlink")
	}
	if data, e := os.ReadFile(marker); e != nil || string(data) != "original" {
		t.Fatal("archive escaped staging directory")
	}
	if _, e := os.Stat(filepath.Join(root, "restore-pending")); !os.IsNotExist(e) {
		t.Fatal("staged rejected archive")
	}
}

// Large catalogue installations must remain recoverable above the old 256 MiB cap.
func TestBackupAboveFormerExpandedLimit(t *testing.T) {
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
	p := filepath.Join(root, "sources", "large-fixture")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(257 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	name, err := recovery.Create(root, "http://fixture.invalid", s)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.Open(filepath.Join(root, "backups", name))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	st, _ := archive.Stat()
	if _, err = recovery.Stage(root, archive, st.Size()); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCapturesFilesBeforeReleasingLock(t *testing.T) {
	root := t.TempDir()
	c, e := config.Load(root)
	if e != nil {
		t.Fatal(e)
	}
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	original := c.Settings.RosterTarget
	captured := false
	name, e := recovery.Create(root, "http://tuner:9192", s, func() {
		captured = true
		c.Settings.RosterTarget = 99
		if err := config.Save(root, c); err != nil {
			t.Fatal(err)
		}
	})
	if e != nil || !captured {
		t.Fatal(name, e, captured)
	}
	z, e := zip.OpenReader(filepath.Join(root, "backups", name))
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "config/config.json" {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			var saved config.Config
			if e = json.NewDecoder(r).Decode(&saved); e != nil {
				t.Fatal(e)
			}
			if saved.Settings.RosterTarget != original {
				t.Fatal("archive included later changes")
			}
			stages, _ := filepath.Glob(filepath.Join(root, "backups", ".backup-stage-*"))
			if len(stages) != 0 {
				t.Fatal("staging leaked")
			}
			return
		}
	}
	t.Fatal("configuration absent")
}
