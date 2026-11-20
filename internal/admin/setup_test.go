package admin

import (
	"antennarr/internal/catalog"
	"antennarr/internal/ingest"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetupProgressPersistsAndCanReopen(t *testing.T) {
	root := t.TempDir()
	s, err := catalog.OpenWithDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	a := API{Store: s}
	defer func() { a.Store.Close() }()
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(method, body string, expected int) catalog.SetupProgress {
		t.Helper()
		r := httptest.NewRequest(method, "/api/setup", strings.NewReader(body))
		r.Header.Set("X-Antennarr-Request", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s %s: %d %s", method, body, w.Code, w.Body.String())
		}
		var p catalog.SetupProgress
		if expected == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	if p := call("GET", "", 200); p != (catalog.SetupProgress{State: "pending", Step: "tuner"}) {
		t.Fatal("unexpected fresh state", p)
	}
	withoutHeader := httptest.NewRecorder()
	mux.ServeHTTP(withoutHeader, httptest.NewRequest("POST", "/api/setup", strings.NewReader(`{"state":"dismissed"}`)))
	if withoutHeader.Code != 403 {
		t.Fatal("setup changed without request header", withoutHeader.Code)
	}
	call("POST", `{"state":"complete"}`, 409)
	call("POST", `{"state":"pending"}`, 400)
	call("POST", `{"state":"active","step":"typo"}`, 400)
	call("POST", `{"state":"active","profile_id":"missing"}`, 409)
	if p := call("GET", "", 200); p.State != "pending" {
		t.Fatal("invalid requests changed setup", p)
	}
	profile, err := s.AddProfilePreset("de-berlin")
	if err != nil {
		t.Fatal(err)
	}
	selection, _ := json.Marshal(map[string]string{"state": "active", "step": "sources", "profile_id": profile.ID})
	p := call("POST", string(selection), 200)
	if p.ProfileID != profile.ID || p.HasTuner || p.Step != "sources" {
		t.Fatal("disabled tuner selection", p)
	}
	call("POST", `{"state":"dismissed"}`, 200)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = catalog.OpenWithDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	a.Store = s
	p = call("GET", "", 200)
	if p.State != "dismissed" || p.Step != "sources" || p.ProfileID != profile.ID {
		t.Fatal("dismissed progress lost on restart", p)
	}
	call("POST", `{"state":"active"}`, 200)
	profile.Enabled = true
	if _, err = s.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	call("POST", `{"state":"complete"}`, 409)
	if _, err = s.Import([]ingest.Entry{{Kind: "xtream", Endpoint: "https://example.test", Username: "test", Password: "test"}}, "test"); err != nil {
		t.Fatal(err)
	}
	if p = call("GET", "", 200); p.State != "active" || !p.HasTuner || !p.HasSources {
		t.Fatal("in-progress guide skipped before Plex", p)
	}
	other, err := s.AddProfilePreset("nl-amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ := json.Marshal(map[string]string{"state": "complete", "profile_id": other.ID})
	call("POST", string(disabled), 409)
	if p = call("POST", `{"state":"complete","step":"plex"}`, 200); p.State != "complete" {
		t.Fatal(p)
	}
	if err = s.DeleteProfile(profile.ID); err != nil {
		t.Fatal(err)
	}
	p = call("GET", "", 200)
	if p.ProfileID != "" || p.HasTuner || p.State != "complete" {
		t.Fatal("completed setup or deleted tuner reference", p)
	}
}
