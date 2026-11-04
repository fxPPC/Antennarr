package tuner

import (
	"antennarr/internal/access"
	"antennarr/internal/config"
	"antennarr/internal/stream"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	c, e := config.Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	m := stream.New(3, time.Millisecond, func(ctx context.Context, _ string, emit func([]byte)) error {
		emit(make([]byte, 1316))
		<-ctx.Done()
		return nil
	})
	t.Cleanup(m.Close)
	return &Server{Config: c, Streams: m, BaseURL: "http://172.17.0.8:9192", Password: "test-only-password"}
}
func TestStableLineupAndAuth(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	for _, path := range []string{"/discover.json", "/lineup.json", "/lineup_status.json", "/healthz", "/device.xml"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), s.Password) {
			t.Fatal("secret leaked")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/lineup.json", nil))
	var rows []map[string]string
	if json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 3 {
		t.Fatal("invalid lineup")
	}
	if rows[0]["URL"] != "http://172.17.0.8:9192/auto/v901" {
		t.Fatal(rows)
	}
	for _, p := range []string{"/", "/api/status"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 401 {
			t.Fatal("unprotected admin")
		}
		r := httptest.NewRequest("GET", p, nil)
		r.SetBasicAuth("admin", s.Password)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("auth failed")
		}
	}
}
func TestSessionLimitAndUnknownChannel(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	for i := 0; i < 3; i++ {
		_, release, e := s.Streams.Subscribe("a", "720")
		if e != nil {
			t.Fatal(e)
		}
		defer release()
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/auto/v901", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auto/v999", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestFailedMediaDoesNotReturnSuccess(t *testing.T) {
	s := testServer(t)
	s.Streams.Close()
	s.Streams = stream.New(3, time.Millisecond, func(context.Context, string, func([]byte)) error { return nil })
	defer s.Streams.Close()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/auto/v901", nil))
	if w.Code != 502 {
		t.Fatal(w.Code)
	}
}

// Browsers may challenge each module/image before sending cached Basic auth.
// These requests must not become failed-password cooldowns that block retries.
func TestMissingCredentialsDoNotConsumePasswordAttempts(t *testing.T) {
	s := testServer(t)
	s.Guard, _ = access.New("")
	h := s.auth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	request := func(header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/admin/ui/status.js", nil)
		if header == "wrong" {
			r.SetBasicAuth("admin", "incorrect-password")
		}
		if header == "correct" {
			r.SetBasicAuth("admin", s.Password)
		}
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	for i := 0; i < 100; i++ {
		w := request("")
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("challenge %d: %d", i, w.Code)
		}
	}
	for i := 0; i < 4; i++ {
		if w := request("wrong"); w.Code != 401 {
			t.Fatalf("password attempt %d: %d", i, w.Code)
		}
	}
	if w := request("wrong"); w.Code != 429 {
		t.Fatalf("bad passwords not limited: %d", w.Code)
	}
	if w := request(""); w.Code != 401 {
		t.Fatalf("challenge blocked by cooldown: %d", w.Code)
	}
	if w := request("correct"); w.Code != 200 {
		t.Fatalf("correct login blocked: %d", w.Code)
	}
	if w := request("wrong"); w.Code != 401 {
		t.Fatalf("correct login did not clear failures: %d", w.Code)
	}
}
