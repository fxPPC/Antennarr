package admin

import (
	"antennarr/internal/catalog"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLineupHTTPGuardsAndRoundTrip(t *testing.T) {
	s, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SaveChannel(catalog.Channel{Name: "CNN", Number: "1", Region: "US", Enabled: true, Aliases: []string{"CNN HD", "-international"}})
	idle := true
	a := API{Store: s, Idle: func() bool { return idle }}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(body string, header bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/lineup", strings.NewReader(body))
		if header {
			r.Header.Set("X-Antennarr-Request", "1")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/lineup", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(w.Code)
	}
	v := json.RawMessage(w.Body.Bytes())
	b, _ := json.Marshal(map[string]any{"confirmed": true, "lineup": v})
	if w = call(string(b), false); w.Code != 403 {
		t.Fatal("mutation header missing", w.Code)
	}
	idle = false
	if w = call(string(b), true); w.Code != 409 {
		t.Fatal("accepted during viewing", w.Code)
	}
	idle = true
	if w = call(`{"confirmed":true,"lineup":{"version":1,"channels":[],"accounts":[]}}`, true); w.Code != 400 {
		t.Fatal("accepted source fields", w.Code)
	}
	if w = call(string(b), true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
