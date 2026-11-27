package admin

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProfileAPIIsolationAndConfirmation(t *testing.T) {
	s, err := catalog.OpenWithDefaults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := API{Store: s, BaseURL: "http://tuner.example:9192", PrimaryListen: ":9192", Idle: func() bool { return true }}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Antennarr-Request", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := call("GET", "/api/profiles", "")
	var ps []profileView
	if json.Unmarshal(w.Body.Bytes(), &ps) != nil || len(ps) != 0 {
		t.Fatal(w.Body.String())
	}
	for _, preset := range []string{"us-nyc", "uk-london"} {
		resp := call("POST", "/api/profiles", `{"preset_id":"`+preset+`"}`)
		if resp.Code >= 300 {
			t.Fatal(resp.Body.String())
		}
	}
	w = call("GET", "/api/profiles", "")
	if json.Unmarshal(w.Body.Bytes(), &ps) != nil || len(ps) != 2 {
		t.Fatal(w.Body.String())
	}
	if ps[0].ListenPort != 9192 || ps[1].TunerURL != "http://tuner.example:9200" || ps[0].Enabled || ps[1].Enabled {
		t.Fatal(ps)
	}
	checkSlots := func(want int) {
		t.Helper()
		w := call("GET", "/api/listeners", "")
		var listeners struct {
			Ports     []int `json:"tuner_ports"`
			Available int   `json:"available_slots"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &listeners) != nil ||
			len(listeners.Ports) != 9 || listeners.Available != want {
			t.Fatalf("want %d of 9 slots free, got %s", want, w.Body.String())
		}
	}
	// An upgraded installation may already have removed tuners as far as 9207.
	if _, err := s.DB.Exec("INSERT INTO settings(name,value) VALUES('profile_slot_highwater','8') ON CONFLICT(name) DO UPDATE SET value='8'"); err != nil {
		t.Fatal(err)
	}
	checkSlots(7) // Both saved tuners reserve their ports even while disabled.
	id := ps[1].ID
	w = call("POST", "/api/profiles/"+id, `{"enabled":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p, _ := s.Profile(id)
	if p.Name != "UK - London" || p.GuideLocation != "SW1A 1AA" || !p.Enabled {
		t.Fatal("partial update erased metadata", p)
	}
	before, _ := s.ExportLineupForProfile("primary")
	w = call("POST", "/api/channels/bulk?profile_id="+id, `{"confirmed":false}`)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = call("POST", "/api/channels/bulk?profile_id="+id, `{"confirmed":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	chs, _ := s.ChannelsForProfile(id)
	if len(chs) != 0 {
		t.Fatal(len(chs))
	}
	primary, _ := s.ExportLineupForProfile("primary")
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(primary)
	if string(b1) != string(b2) {
		t.Fatal("clearing London changed USA")
	}
	w = call("POST", "/api/channels/bulk?profile_id="+id, `{"confirmed":true,"reset_preset":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	chs, _ = s.ChannelsForProfile(id)
	if len(chs) != 109 {
		t.Fatal(len(chs))
	}
	w = call("GET", "/api/roster?profile_id="+id, "")
	var summary catalog.RosterSummary
	if json.Unmarshal(w.Body.Bytes(), &summary) != nil || len(summary.Channels) != 109 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/lineup?profile_id=missing", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	a.Idle = func() bool { return false }
	w = call("DELETE", "/api/profiles/"+id, `{"confirmed":true}`)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	a.Idle = func() bool { return true }
	w = call("DELETE", "/api/profiles/"+id, `{"confirmed":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	checkSlots(8)
	w = call("POST", "/api/profiles", `{"preset_id":"mx-mexico-city"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var replacement catalog.Profile
	if err := json.Unmarshal(w.Body.Bytes(), &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.Enabled || replacement.ID == id || replacement.DeviceID == p.DeviceID || replacement.PortOffset != 1 {
		t.Fatal(replacement)
	}
	checkSlots(7)
	var after []profileView
	w = call("GET", "/api/profiles", "")
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil || len(after) != 2 {
		t.Fatal(w.Body.String())
	}
	if after[0].DeviceID != ps[0].DeviceID || after[0].TunerURL != ps[0].TunerURL || after[1].ListenPort != 9200 {
		t.Fatal(after)
	}
}

func TestDashboardPollDoesNotRejectTunerCreation(t *testing.T) {
	store, err := catalog.OpenWithDefaults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a := API{Store: store, ReadSettings: func() (config.Settings, error) {
		close(started)
		<-release
		return config.FreshSettings(), nil
	}}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	go func() {
		defer close(done)
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/settings", nil))
	}()
	<-started
	defer func() { close(release); <-done }()
	r := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(`{"preset_id":"us-nyc"}`))
	r.Header.Set("X-Antennarr-Request", "1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("add during poll: %d %s", w.Code, w.Body.String())
	}
}
