package admin

import (
	"antennarr/internal/activity"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/monitor"
	"antennarr/internal/systeminfo"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed ui/*.js
var uiFiles embed.FS

//go:embed app.js
var appJS []byte

//go:embed style.css
var styleCSS []byte

//go:embed favicon.png
var favicon []byte

//go:embed logo.png
var logo []byte
var logoHash = fmt.Sprintf("\"%x\"", sha256.Sum256(logo))

//go:embed index.html
var page []byte

type API struct {
	TunerPorts            config.TunerPorts
	PrimaryListen         string
	SystemInfo            func() systeminfo.Snapshot
	Monitor               func(context.Context) (monitor.Snapshot, error)
	WithIdle              func(func() error) error
	settingsMu            sync.Mutex
	WithChecksPaused      func(func() error) error
	WithProtectedAccounts func(func([]int64) error) error
	DataRoot, BaseURL     string
	ReadLogin             func() config.AdminCredentials
	ChangeLogin           func(string, string, string) error
	Idle                  func() bool
	StopStream            func(string) bool
	Restart               func()
	operations            sync.RWMutex
	importing             atomic.Bool
	backingUp             atomic.Bool
	maintenance           atomic.Bool
	Activity              *activity.Tracker
	ReadSettings          func() (config.Settings, error)
	SaveSettings          func(config.Settings) error
	Store                 *catalog.Store
	Client                *http.Client
}

func (a *API) Register(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	// The public brand asset contains no account data. All administration assets
	// and APIs retain their existing authentication.
	mux.HandleFunc("GET /branding/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("ETag", logoHash)
		http.ServeContent(w, r, "logo.png", time.Time{}, bytes.NewReader(logo))
	})
	mux.HandleFunc("GET /admin/ui/{name}", auth(func(w http.ResponseWriter, r *http.Request) {
		b, e := uiFiles.ReadFile("ui/" + r.PathValue("name"))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(b)
	}))
	mux.HandleFunc("GET /admin/app.js", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(appJS)
	}))
	mux.HandleFunc("GET /admin/style.css", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(styleCSS)
	}))
	mux.HandleFunc("GET /admin/favicon.png", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(favicon)
	}))
	mux.HandleFunc("GET /admin/logo.png", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(logo)
	}))
	mux.HandleFunc("GET /api/connection", auth(func(w http.ResponseWriter, r *http.Request) {
		links := map[string]string{"tuner_url": a.BaseURL}
		addrs, _ := net.InterfaceAddrs()
		for _, addr := range addrs {
			ip, _, e := net.ParseCIDR(addr.String())
			if e == nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() {
				links["private_tuner_url"] = "http://" + net.JoinHostPort(ip.String(), "9192")
				links["stream_fallback_url"] = "http://" + net.JoinHostPort(ip.String(), "5004") + "/auto/v{channel}"
				break
			}
		}
		response(w, links)
	}))
	mux.HandleFunc("GET /admin", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Write(page)
	}))
	for _, route := range []struct {
		pattern string
		fn      http.HandlerFunc
	}{
		{"GET /api/profiles", a.profiles}, {"POST /api/profiles", a.addProfile}, {"POST /api/profiles/{id}", a.saveProfile}, {"DELETE /api/profiles/{id}", a.deleteProfile}, {"GET /api/presets", a.presets},
		{"GET /api/lineup", a.exportLineup}, {"POST /api/lineup", a.importLineup}, {"GET /api/output/search", a.outputSearch}, {"GET /api/system", a.systemStatus}, {"POST /api/channels/bulk", a.bulkChannels}, {"POST /api/reset", a.resetApplication}, {"GET /api/monitor", a.monitorStatus}, {"POST /api/outgoing/{id}/stop", a.stopOutgoing}, {"POST /api/input/clear", a.clearQueue}, {"GET /api/roster", a.rosterSummary}, {"GET /api/roster/{id}", a.rosterEntries}, {"GET /api/channels/{id}/excluded", a.excludedStreams}, {"POST /api/roster/{id}/policy", a.rosterPolicy}, {"GET /api/input/accounts", a.inputAccounts}, {"GET /api/input/import", a.importStatus}, {"GET /api/events", a.events}, {"GET /api/login", a.login}, {"POST /api/login", a.saveLogin}, {"GET /api/settings", a.settings}, {"POST /api/settings", a.saveSettings}, {"GET /api/removal-policy", a.removalPolicy}, {"POST /api/removal-policy", a.saveRemovalPolicy}, {"POST /api/import", a.importSources}, {"GET /api/channels", a.channels}, {"POST /api/channels", a.saveChannel}, {"DELETE /api/channels/{id}", a.deleteChannel}, {"POST /api/mappings", a.mapping}, {"POST /api/backup", a.fullBackup}, {"GET /api/backups/{name}", a.downloadBackup}, {"POST /api/restore", a.restoreBackup},
	} {
		fn := route.fn
		mux.HandleFunc(route.pattern, auth(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && r.Header.Get("X-Antennarr-Request") != "1" {
				http.Error(w, "Required request header missing", 403)
				return
			}
			// Backup releases its own operations lock after taking an immutable
			// snapshot; ZIP compression must not block status reads or new viewers.
			if r.URL.Path == "/api/backup" {
				fn(w, r)
				return
			}
			exclusive := (r.URL.Path == "/api/lineup" && r.Method == "POST") || (strings.HasPrefix(r.URL.Path, "/api/profiles") && r.Method != "GET") || r.URL.Path == "/api/reset" || r.URL.Path == "/api/channels/bulk" || r.URL.Path == "/api/restore" || r.URL.Path == "/api/input/clear"
			if exclusive {
				if a.backingUp.Load() {
					failure(w, 409, "Backup is being packaged; try again shortly")
					return
				}
				if !a.operations.TryLock() {
					failure(w, 409, "Another operation is running; try again shortly")
					return
				}
				defer a.operations.Unlock()
				if a.backingUp.Load() {
					failure(w, 409, "Backup is being packaged; try again shortly")
					return
				}
			} else {
				a.operations.RLock()
				defer a.operations.RUnlock()
			}
			if a.maintenance.Load() {
				failure(w, 503, "Antennarr is restarting after an administration operation")
				return
			}
			fn(w, r)
		}))
	}
}
func response(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, message string) { http.Error(w, message, status) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		failure(w, 400, "Invalid request")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		failure(w, 400, "Invalid trailing request data")
		return false
	}
	return true
}

func (a *API) channels(w http.ResponseWriter, r *http.Request) {
	var v []catalog.Channel
	var e error
	if id := r.URL.Query().Get("profile_id"); id != "" {
		v, e = a.Store.ChannelsForProfile(id)
	} else {
		v, e = a.Store.Channels()
	}
	if e != nil {
		failure(w, 500, "Channels unavailable")
		return
	}
	response(w, v)
}
func (a *API) saveChannel(w http.ResponseWriter, r *http.Request) {
	var c catalog.Channel
	if !decode(w, r, &c) {
		return
	}
	id, e := a.Store.SaveChannel(c)
	if e != nil {
		failure(w, 400, e.Error())
		return
	}
	response(w, map[string]string{"id": id})
}
func (a *API) deleteChannel(w http.ResponseWriter, r *http.Request) {
	if a.Store.DeleteChannel(r.PathValue("id")) != nil {
		failure(w, 400, "Channel removal failed")
		return
	}
	response(w, map[string]bool{"saved": true})
}
func (a *API) mapping(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Channel string `json:"channel_id"`
		Stream  int64  `json:"stream_id"`
		Enabled bool   `json:"enabled"`
	}
	if !decode(w, r, &v) {
		return
	}
	if e := a.Store.Map(v.Channel, v.Stream, v.Enabled); e != nil {
		message := "Could not change source assignment"
		if e.Error() == "stream excluded by channel rules; edit the channel rules first" {
			message = e.Error()
		}
		failure(w, 400, message)
		return
	}
	if !v.Enabled {
		a.Store.DB.Exec("DELETE FROM roster WHERE channel_id=? AND stream_id=?", v.Channel, v.Stream)
	}
	response(w, map[string]bool{"saved": true})
}

func (a *API) settings(w http.ResponseWriter, r *http.Request) {
	if a.ReadSettings == nil {
		failure(w, 503, "Settings unavailable")
		return
	}
	v, e := a.ReadSettings()
	if e != nil {
		failure(w, 500, "Settings read failed")
		return
	}
	response(w, v)
}
func (a *API) saveSettings(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	var old config.Settings
	old.Defaults()
	if a.ReadSettings != nil {
		var e error
		old, e = a.ReadSettings()
		if e != nil {
			failure(w, 500, "Settings read failed; nothing was saved")
			return
		}
	}
	v := old
	if !decode(w, r, &v) {
		return
	}
	v = config.AdjustViewerCapacity(v, old)
	if v.Validate() != nil {
		failure(w, 400, "Settings outside supported ranges; provider limits must fit total upstream capacity")
		return
	}
	if a.SaveSettings == nil || a.SaveSettings(v) != nil {
		failure(w, 500, "Settings save failed")
		return
	}
	response(w, map[string]any{"saved": true, "restart_required": v.BufferSeconds != old.BufferSeconds || v.StallTimeoutMS != old.StallTimeoutMS, "settings": v})
}
func (a *API) Page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}

func (a *API) removalPolicy(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.RemovalPolicy()
	if e != nil {
		failure(w, 500, "Policy unavailable")
		return
	}
	response(w, v)
}
func (a *API) saveRemovalPolicy(w http.ResponseWriter, r *http.Request) {
	var v catalog.RemovalPolicy
	if !decode(w, r, &v) {
		return
	}
	if e := a.Store.SaveRemovalPolicy(v); e != nil {
		failure(w, 400, e.Error())
		return
	}
	response(w, map[string]bool{"saved": true})
}

func (a *API) stopOutgoing(w http.ResponseWriter, r *http.Request) {
	if a.StopStream == nil {
		failure(w, 503, "Stream control unavailable")
		return
	}
	response(w, map[string]bool{"stopped": a.StopStream(r.PathValue("id"))})
}

func (a *API) monitorStatus(w http.ResponseWriter, r *http.Request) {
	if a.Monitor == nil {
		failure(w, 503, "Status collector unavailable")
		return
	}
	v, e := a.Monitor(r.Context())
	if e != nil {
		failure(w, 503, "Status data unavailable; retry shortly")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response(w, v)
}

func (a *API) systemStatus(w http.ResponseWriter, r *http.Request) {
	if a.SystemInfo == nil {
		failure(w, 503, "System information unavailable")
		return
	}
	response(w, a.SystemInfo())
}
