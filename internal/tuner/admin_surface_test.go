package tuner

import (
	"antennarr/internal/admin"
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestPublicAdministrationNeverServesTuner(t *testing.T) {
	s := testServer(t)
	public := s.AdminHandler()
	for _, path := range []string{"/auto/v901", "/lineup.json", "/discover.json", "/lineup_status.json", "/lineup.post", "/device.xml"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "172.17.0.8:9192"
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		r.SetBasicAuth("admin", s.Password)
		w := httptest.NewRecorder()
		public.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("public route %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	public.ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))
	if w.Code != 401 {
		t.Fatal("public administration unprotected", w.Code)
	}
	r := httptest.NewRequest("HEAD", "/auto/v901", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("private tuner was blocked", w.Code)
	}
}
func TestCorrectPasswordBypassesFailedLoginCooldown(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest("GET", "/api/status", nil)
		r.SetBasicAuth("admin", "wrong")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 4 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("no brute-force cooldown", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/status", nil)
	r.SetBasicAuth("admin", s.Password)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("correct login locked out", w.Code)
	}
}

func TestOnlyBrandLogoIsPublic(t *testing.T) {
	s := testServer(t)
	s.Admin = &admin.API{}
	h := s.AdminHandler()
	for path, want := range map[string]int{"/branding/logo.png": 200, "/branding/other.png": 404, "/admin/logo.png": 401, "/api/status": 401, "/": 401, "/discover.json": 404, "/auto/v901": 404} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatal(path, w.Code, want)
		}
		if want == 200 {
			if !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG")) || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("ETag") == "" {
				t.Fatal("invalid public logo")
			}
		}
	}
}

func TestDashboardReloadHasSeparateAuthenticatedAssetBudget(t *testing.T) {
	s := testServer(t)
	s.Admin = &admin.API{}
	h := s.AdminHandler()
	get := func(path, password string) int {
		r := httptest.NewRequest("GET", path, nil)
		if password != "" {
			r.SetBasicAuth("admin", password)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		want := 401
		if i == 4 {
			want = 429
		}
		if got := get("/admin/ui/core.js", "wrong"); got != want {
			t.Fatalf("wrong password for asset: got %d, want %d", got, want)
		}
	}
	if got := get("/admin", ""); got != 401 {
		t.Fatalf("anonymous page: %d", got)
	}
	if got := get("/api/status", s.Password); got != 200 {
		t.Fatalf("initial API request: %d", got)
	}
	// Drive each budget to its limit, then check the other class still works.
	for i := 0; ; i++ {
		code := get("/api/status", s.Password)
		if code == 429 {
			break
		}
		if code != 200 || i > 1000 {
			t.Fatalf("API burst failed to reach its limit: %d", code)
		}
	}
	if got := get("/admin/ui/missing.js", s.Password); got != 429 {
		t.Fatalf("unrecognised module used the asset budget: %d", got)
	}
	for _, path := range []string{"/", "/admin", "/admin/app.js", "/admin/style.css", "/admin/logo.png", "/admin/favicon.png", "/admin/ui/core.js"} {
		if got := get(path, s.Password); got != 200 {
			t.Fatalf("dashboard reload after API burst: %s returned %d", path, got)
		}
	}
	s = testServer(t)
	s.Admin = &admin.API{}
	h = s.AdminHandler()
	for i := 0; ; i++ {
		code := get("/admin/ui/core.js", s.Password)
		if code == 429 {
			break
		}
		if code != 200 || i > 1000 {
			t.Fatalf("asset burst failed to reach its limit: %d", code)
		}
	}
	if got := get("/api/status", s.Password); got != 200 {
		t.Fatalf("API unavailable after asset burst: %d", got)
	}
}
