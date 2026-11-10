package admin

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupDownloadRestoreAndGating(t *testing.T) {
	root := t.TempDir()
	config.Load(root)
	config.AdminPassword(root)
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	idle := false
	restarts := 0
	a := &API{Store: s, DataRoot: root, BaseURL: "http://172.17.0.6:9192", Idle: func() bool { return idle }, Restart: func() { restarts++ }}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	request := func(method, path string, b *bytes.Buffer, content string) *httptest.ResponseRecorder {
		if b == nil {
			b = &bytes.Buffer{}
		}
		r := httptest.NewRequest(method, path, b)
		r.Header.Set("X-Antennarr-Request", "1")
		r.Header.Set("Content-Type", content)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/backup", nil, "application/json")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	raw, e := os.ReadFile(filepath.Join(root, "backups", result["backup"]))
	if e != nil {
		t.Fatal(e)
	}
	download := request("GET", result["download"], nil, "")
	if download.Code != 200 || !bytes.Equal(download.Body.Bytes(), raw) {
		t.Fatal("download mismatch")
	}
	upload := func() *httptest.ResponseRecorder {
		var b bytes.Buffer
		form := multipart.NewWriter(&b)
		f, _ := form.CreateFormFile("backup", "restore.zip")
		f.Write(raw)
		form.Close()
		return request("POST", "/api/restore", &b, form.FormDataContentType())
	}
	if w = upload(); w.Code != 409 || restarts != 0 {
		t.Fatal("restore allowed during playback", w.Code)
	}
	idle = true
	release, ok := a.BeginTune()
	if !ok {
		t.Fatal("tuning unexpectedly disabled")
	}
	if w = upload(); w.Code != 409 {
		t.Fatal("restore raced with new tune")
	}
	release()
	if w = upload(); w.Code != 200 || restarts != 1 {
		t.Fatal(w.Code, w.Body, restarts)
	}
	files, _ := filepath.Glob(filepath.Join(root, "backups", "*.zip"))
	if len(files) != 1 {
		t.Fatal("restore made an automatic backup", files)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("safety_backup")) {
		t.Fatal("obsolete safety backup response")
	}
	if _, ok = a.BeginTune(); ok {
		t.Fatal("tuning allowed during restore")
	}
	if w = request("GET", "/api/channels", nil, ""); w.Code != 503 {
		t.Fatal("API accepted post-restore work")
	}
	if _, e = os.Stat(filepath.Join(root, "restore-pending", "manifest.json")); e != nil {
		t.Fatal(e)
	}
}

func TestBackupPackagingAllowsReadsAndTunesButBlocksDestructiveWork(t *testing.T) {
	a := &API{}
	a.backingUp.Store(true)
	release, ok := a.BeginTune()
	if !ok {
		t.Fatal("packaging blocked tune")
	}
	release()
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	for _, path := range []string{"/api/backup", "/api/reset", "/api/restore"} {
		r := httptest.NewRequest("POST", path, bytes.NewBufferString("{}"))
		r.Header.Set("X-Antennarr-Request", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 409 {
			t.Fatal(path, w.Code)
		}
	}
}
