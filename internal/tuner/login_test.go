package tuner

import (
	"antennarr/internal/admin"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginChangeReauthenticatesWithoutChangingTuner(t *testing.T) {
	root := t.TempDir()
	config.Load(root)
	login, e := config.OpenAdminLogin(root)
	if e != nil {
		t.Fatal(e)
	}
	store, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	server := testServer(t)
	server.ReadLogin = login.Current
	server.Admin = &admin.API{Store: store, ReadLogin: login.Current, ChangeLogin: login.Change}
	h := server.Handler()
	old := login.Current()
	call := func(method, path, body, user, pass string, header bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.SetBasicAuth(user, pass)
		if header {
			r.Header.Set("X-Antennarr-Request", "1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	before := call("GET", "/discover.json", "", "", "", false).Body.String()
	body := func(current, user, password string) string {
		b, _ := json.Marshal(map[string]string{"current_password": current, "username": user, "password": password})
		return string(b)
	}
	w := call("GET", "/api/login", "", old.Username, old.Password, false)
	var loginView map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &loginView) != nil || loginView["username"] != old.Username || len(loginView) != 1 {
		t.Fatal("login GET leaked or failed")
	}
	change := body(old.Password, "custom-admin", "fixture-password-new")
	if w = call("POST", "/api/login", change, old.Username, old.Password, false); w.Code != 403 {
		t.Fatal("missing mutation header accepted")
	}
	if w = call("POST", "/api/login", body("wrong-current-password", "custom-admin", "fixture-password-new"), old.Username, old.Password, true); w.Code != 403 {
		t.Fatal("current password not checked")
	}
	if w = call("POST", "/api/login", change, old.Username, old.Password, true); w.Code != 200 || strings.Contains(w.Body.String(), "fixture-password-new") {
		t.Fatal("login save failed or leaked")
	}
	for _, path := range []string{"/admin", "/api/login", "/api/status", "/admin/app.js"} {
		if w = call("GET", path, "", old.Username, old.Password, false); w.Code != 401 {
			t.Fatal("old login still works", path)
		}
		if w = call("GET", path, "", "custom-admin", "fixture-password-new", false); w.Code != 200 {
			t.Fatal("new login failed", path, w.Code)
		}
	}
	if after := call("GET", "/discover.json", "", "", "", false).Body.String(); after != before {
		t.Fatal("login change altered tuner discovery")
	}
	if w = call("GET", "/lineup.json", "", "", "", false); w.Code != 200 {
		t.Fatal("Plex lineup affected")
	}
}
