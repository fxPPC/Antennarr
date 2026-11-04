package tuner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultPortFallbackUsesSameTunerAndHasNoAdmin(t *testing.T) {
	s := testServer(t)
	fallback := httptest.NewServer(s.StreamHandler())
	defer fallback.Close()
	for _, path := range []string{"/admin", "/api/status", "/api/login", "/auto/v999"} {
		r, e := http.Get(fallback.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatalf("%s: %d", path, r.StatusCode)
		}
	}
	r, e := http.Get(fallback.URL + "/auto/v901")
	if e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 1316)
	_, e = io.ReadFull(r.Body, b)
	r.Body.Close()
	if e != nil || r.StatusCode != 200 {
		t.Fatal(r.StatusCode, e)
	}
}

func TestFallbackSharesTunerLimit(t *testing.T) {
	s := testServer(t)
	var releases []func()
	defer func() {
		for _, f := range releases {
			f()
		}
	}()
	for i := 0; i < 3; i++ {
		_, release, e := s.Streams.Subscribe("busy", "720")
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, release)
	}
	for _, h := range []http.Handler{s.Handler(), s.StreamHandler()} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/auto/v901", nil))
		if w.Code != 503 {
			t.Fatalf("fallback bypassed shared tuner limit: %d", w.Code)
		}
	}
}
