package admin

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"antennarr/internal/recovery"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueueClearDoesNotBackupAndManualRestoreWorks(t *testing.T) {
	root := t.TempDir()
	config.Load(root)
	config.AdminPassword(root)
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	s.SaveChannel(catalog.Channel{Number: "101", Name: "CNN", Region: "US", Enabled: true})
	entry, _ := ingest.FromURL("https://fixture.example/1.ts", "CNN", "")
	s.Import([]ingest.Entry{entry}, "fixture")
	paused := false
	a := API{Store: s, DataRoot: root, BaseURL: "http://antennarr:9192", WithChecksPaused: func(fn func() error) error { paused = true; return fn() }, WithProtectedAccounts: func(fn func([]int64) error) error { return fn(nil) }}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(body string, header bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/input/clear", strings.NewReader(body))
		if header {
			r.Header.Set("X-Antennarr-Request", "1")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call(`{"confirmed":true}`, false); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call(`{}`, true); w.Code != 400 || paused {
		t.Fatal(w.Code, paused)
	}
	// Only an explicit manual backup request creates a backup.
	br := httptest.NewRequest("POST", "/api/backup", strings.NewReader("{}"))
	br.Header.Set("X-Antennarr-Request", "1")
	bw := httptest.NewRecorder()
	mux.ServeHTTP(bw, br)
	if bw.Code != 200 {
		t.Fatal(bw.Code, bw.Body)
	}
	var manual map[string]string
	json.Unmarshal(bw.Body.Bytes(), &manual)
	w := call(`{"confirmed":true}`, true)
	if w.Code != 200 || !paused {
		t.Fatal(w.Code, w.Body)
	}
	var result struct {
		Backup  string             `json:"backup"`
		Removed catalog.QueueClear `json:"removed"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Removed.Accounts != 1 || result.Removed.Streams != 1 {
		t.Fatal(result)
	}
	if result.Backup != "" {
		t.Fatal("clear created backup", result.Backup)
	}
	saved, _ := filepath.Glob(filepath.Join(root, "backups", "*.zip"))
	if len(saved) != 1 {
		t.Fatal("automatic backup during clear", saved)
	}
	f, e := os.Open(filepath.Join(root, "backups", manual["backup"]))
	if e != nil {
		t.Fatal(e)
	}
	info, _ := f.Stat()
	if _, e = recovery.Stage(root, f, info.Size()); e != nil {
		t.Fatal(e)
	}
	f.Close()
	s.Close()
	if e = recovery.ApplyPending(root); e != nil {
		t.Fatal(e)
	}
	s, e = catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	summary, e := s.RosterSummary()
	if e != nil || summary.QueuedAccounts != 1 || summary.QueuedStreams != 1 {
		t.Fatal(summary, e)
	}
}
func TestSettingsPartialUpdatesAndViewerCapacity(t *testing.T) {
	var current config.Settings
	current.Defaults()
	current.RosterTarget = 37
	a := API{ReadSettings: func() (config.Settings, error) { return current, nil }, SaveSettings: func(v config.Settings) error { current = v; return nil }}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/settings", strings.NewReader(body))
		r.Header.Set("X-Antennarr-Request", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call(`{"max_outgoing_streams":6}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if current.MaxOutgoingStreams != 6 || current.GlobalConnections != 15 || current.RosterTarget != 37 {
		t.Fatal(current)
	}
	if w := call(`{"max_outgoing_streams":2}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if current.GlobalConnections != 15 || current.RosterTarget != 37 {
		t.Fatal(current)
	}
	if w := call(`{"max_outgoing_streams":0}`); w.Code != 400 {
		t.Fatal("invalid limit accepted", w.Code)
	}
}
