package admin

import (
	"antennarr/internal/catalog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOutputSearchAPIIsAuthenticatedAndBounded(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := API{Store: s}
	mux := http.NewServeMux()
	a.Register(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Test-Auth") != "yes" {
				http.Error(w, "Denied", 401)
				return
			}
			next(w, r)
		}
	})
	for _, tc := range []struct {
		auth, q string
		code    int
	}{{"", "CNN", 401}, {"yes", "stream:123", 200}, {"yes", "stream:0", 400}, {"yes", strings.Repeat("x", 121), 400}} {
		r := httptest.NewRequest("GET", "/api/output/search?q="+tc.q, nil)
		r.Header.Set("X-Test-Auth", tc.auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
