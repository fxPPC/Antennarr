package admin

import (
	"antennarr/internal/recovery"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// BeginTune closes the gap between checking idle state and subscribing a viewer.
func (a *API) BeginTune() (func(), bool) {
	a.operations.RLock()
	if a.maintenance.Load() {
		a.operations.RUnlock()
		return func() {}, false
	}
	return a.operations.RUnlock, true
}
func (a *API) fullBackup(w http.ResponseWriter, r *http.Request) {
	if a.DataRoot == "" {
		failure(w, 503, "Full backup unavailable")
		return
	}
	name, e := recovery.Create(a.DataRoot, a.BaseURL, a.Store)
	if e != nil {
		failure(w, 500, "Full backup failed")
		return
	}
	response(w, map[string]string{"backup": name, "download": "/api/backups/" + name})
}
func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != filepath.Base(name) || !strings.HasPrefix(name, "antennarr-") || !strings.HasSuffix(name, ".zip") || a.DataRoot == "" {
		failure(w, 404, "Backup not found")
		return
	}
	path := filepath.Join(a.DataRoot, "backups", name)
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() {
		failure(w, 404, "Backup not found")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}
func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if a.DataRoot == "" || a.Restart == nil || a.Idle == nil {
		failure(w, 503, "Restore unavailable")
		return
	}
	if !a.Idle() {
		failure(w, 409, "Stop watching before restoring a backup")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, recovery.MaxArchive+1<<20)
	if e := r.ParseMultipartForm(4 << 20); e != nil {
		failure(w, 400, "Choose a backup ZIP of at most 128 MiB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, h, e := r.FormFile("backup")
	if e != nil {
		failure(w, 400, "Choose a backup ZIP")
		return
	}
	defer f.Close()
	manifest, e := recovery.Stage(a.DataRoot, f, h.Size)
	if e != nil {
		failure(w, 400, e.Error())
		return
	}
	a.maintenance.Store(true)
	response(w, map[string]any{"restoring": true, "restart_required": true, "tuner_url": manifest.TunerURL})
	a.Restart()
}
