package admin

import (
	"antennarr/internal/catalog"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProfileAPIIsolationAndConfirmation(t *testing.T) {
	s, err := catalog.OpenWithUSDefaults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := API{Store: s, BaseURL: "http://tuner.example:19192", PrimaryListen: ":19192", Idle: func() bool { return true }}
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
	if json.Unmarshal(w.Body.Bytes(), &ps) != nil || len(ps) != 2 {
		t.Fatal(w.Body.String())
	}
	if ps[0].ListenPort != 19192 || ps[1].TunerURL != "http://tuner.example:9200" || ps[0].Enabled || ps[1].Enabled {
		t.Fatal(ps)
	}
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
	if len(chs) != 110 {
		t.Fatal(len(chs))
	}
	w = call("GET", "/api/roster?profile_id="+id, "")
	var summary catalog.RosterSummary
	if json.Unmarshal(w.Body.Bytes(), &summary) != nil || len(summary.Channels) != 110 {
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
	w = call("POST", "/api/profiles", `{"preset_id":"uk-london"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var replacement catalog.Profile
	json.Unmarshal(w.Body.Bytes(), &replacement)
	if replacement.Enabled || replacement.ID == id || replacement.PortOffset != 2 {
		t.Fatal(replacement)
	}
}
