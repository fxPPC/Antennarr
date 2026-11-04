package tuner

import (
	"antennarr/internal/access"
	"antennarr/internal/admin"
	"antennarr/internal/config"
	"antennarr/internal/stream"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Guard             *access.Guard
	ChannelList       func() []config.Channel
	BeforeTune        func()
	Admin             *admin.API
	Config            config.Config
	Streams           *stream.Manager
	BaseURL, Password string
	ReadLogin         func() config.AdminCredentials
}

// StreamHandler supplies the HDHomeRun default-port fallback without adding
// another administration surface. It shares the same viewer/upstream manager.
func (s *Server) StreamHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auto/{channel}", s.watch)
	return mux
}

func (s *Server) Handler() http.Handler {
	if s.Guard == nil {
		s.Guard, _ = access.New("")
	}
	mux := http.NewServeMux()
	if s.Admin != nil {
		s.Admin.Register(mux, s.auth)
	}
	mux.HandleFunc("GET /discover.json", s.discover)
	mux.HandleFunc("GET /lineup.json", s.lineup)
	mux.HandleFunc("GET /lineup_status.json", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"ScanInProgress": 0, "ScanPossible": 0, "Source": "Cable", "SourceList": []string{"Cable"}})
	})
	mux.HandleFunc("POST /lineup.post", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /device.xml", s.device)
	mux.HandleFunc("GET /auto/{channel}", s.watch)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"status": "ok", "stage": "ready"})
	})
	mux.HandleFunc("GET /api/status", s.auth(func(w http.ResponseWriter, r *http.Request) { reply(w, s.Streams.Status()) }))
	mux.HandleFunc("GET /{$}", s.auth(s.page))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		login := config.AdminCredentials{Username: "admin", Password: s.Password}
		if s.ReadLogin != nil {
			login = s.ReadLogin()
		}
		got := sha256.Sum256([]byte(p))
		want := sha256.Sum256([]byte(login.Password))
		if !ok || u != login.Username || login.Password == "" || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Antennarr", charset="UTF-8"`)
			// Missing credentials are a normal HTTP authentication challenge, not a
			// failed password. Parallel module/image requests must retain their 401
			// challenge so browsers can retry them with the saved login.
			if s.Guard != nil && r.Header.Get("Authorization") != "" {
				if wait := s.Guard.Failure(s.Guard.Key(r)); wait > 0 {
					access.Limited(w, wait)
					return
				}
			}
			http.Error(w, "Authentication required", 401)
			return
		}
		if s.Guard != nil {
			key := s.Guard.Key(r)
			s.Guard.Success(key)
			if wait := s.Guard.Allow(key); wait > 0 {
				access.Limited(w, wait)
				return
			}
			release, wait := s.Guard.Cost(key, r.URL.Path, r.Method)
			if wait > 0 {
				access.Limited(w, wait)
				return
			}
			defer release()
		}
		next(w, r)
	}
}
func (s *Server) tunerCount() int {
	if s.Streams != nil {
		return s.Streams.Status().Limit
	}
	return s.Config.Tuners
}
func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]any{
		"FriendlyName": "Antennarr", "Manufacturer": "Antennarr", "ModelNumber": "HDHR3-US", "FirmwareName": "hdhomerun3_atsc", "FirmwareVersion": "20261026", "DeviceID": strings.ToUpper(s.Config.DeviceID), "DeviceAuth": "antennarr", "TunerCount": s.tunerCount(), "BaseURL": s.BaseURL, "LineupURL": s.BaseURL + "/lineup.json",
	})
}
func (s *Server) lineup(w http.ResponseWriter, r *http.Request) {
	rows := []map[string]string{}
	for _, c := range s.channels() {
		rows = append(rows, map[string]string{"GuideNumber": c.Number, "GuideName": c.Name, "URL": s.BaseURL + "/auto/v" + c.Number})
	}
	reply(w, rows)
}
func (s *Server) watch(w http.ResponseWriter, r *http.Request) {
	var ch *config.Channel
	list := s.channels()
	for i := range list {
		if "v"+list[i].Number == r.PathValue("channel") {
			ch = &list[i]
			break
		}
	}
	if ch == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == "HEAD" {
		w.Header().Set("Content-Type", "video/mpeg")
		return
	}
	if s.BeforeTune != nil {
		s.BeforeTune()
	}
	descriptor := ch.Fixture
	if descriptor == "" {
		descriptor = ch.ID
	}
	unlock := func() {}
	if s.Admin != nil {
		var ok bool
		unlock, ok = s.Admin.BeginTune()
		if !ok {
			http.Error(w, "Antennarr is restarting", 503)
			return
		}
	}
	data, release, err := s.Streams.Subscribe(ch.ID, descriptor)
	unlock()
	if err != nil {
		http.Error(w, "No tuner available", 503)
		return
	}
	defer release()
	// Do not return HTTP 200 before receiving media.
	startup := time.NewTimer(30 * time.Second)
	defer startup.Stop()
	var first []byte
	select {
	case first = <-data:
		if len(first) == 0 {
			http.Error(w, "Media unavailable", 502)
			return
		}
	case <-r.Context().Done():
		return
	case <-startup.C:
		http.Error(w, "Media startup timeout", 504)
		return
	}
	w.Header().Set("Content-Type", "video/mpeg")
	rc := http.NewResponseController(w)
	send := func(b []byte) error {
		_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := w.Write(b); err != nil {
			return err
		}
		return rc.Flush()
	}
	if send(first) != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-data:
			if !ok {
				return
			}
			if send(b) != nil {
				return
			}
		}
	}
}
func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	_ = deviceTemplate.Execute(w, s)
}

var deviceTemplate = template.Must(template.New("device").Parse(`<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><URLBase>{{.BaseURL}}/</URLBase><device><deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType><friendlyName>Antennarr</friendlyName><manufacturer>Antennarr</manufacturer><modelName>HDHomeRun</modelName><modelNumber>HDHR3-US</modelNumber><serialNumber>{{.Config.DeviceID}}</serialNumber><UDN>uuid:{{.Config.DeviceID}}</UDN></device></root>`))

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if s.Admin != nil {
		s.Admin.Page(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	st := s.Streams.Status()
	fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Antennarr</title><style>body{font:17px system-ui;max-width:800px;margin:60px auto;padding:20px;background:#101923;color:#edf2f5}section{background:#1b2a37;padding:24px;border-radius:12px;margin:20px 0}a{color:#8dd9cf}h1{font-size:40px}code{word-break:break-all}</style><h1>Antennarr</h1><p>Fixture tuner · <a href="/admin">Manage sources and channels</a></p><section><h2>Live status</h2><p>%d / %d viewers · %d active channels</p></section><section><h2>Three controlled test channels</h2><p>901 · H.264 / AAC, 720p<br>902 · H.264 / AAC, 480p<br>903 · MPEG-2 / MP2</p><p>Generated test pictures, no external streams.</p></section><section><h2>Setup</h2><p>The tuner address and channel lineup are available through the private Docker network. Manage selected channels, source imports, checks, and live primary/ghost status in the administration page.</p></section></html>`, st.Viewers, s.Config.Tuners, st.Channels)
}

func (s *Server) channels() []config.Channel {
	if s.ChannelList != nil {
		return s.ChannelList()
	}
	return s.Config.Channels
}

// AdminHandler is used by the NPM-facing listener. Headers, Host values, and
// credentials cannot turn this listener into an unauthenticated public tuner.
func (s *Server) AdminHandler() http.Handler {
	private := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" && path != "/healthz" && path != "/branding/logo.png" && path != "/admin" && !strings.HasPrefix(path, "/admin/") && !strings.HasPrefix(path, "/api/") {
			http.NotFound(w, r)
			return
		}
		private.ServeHTTP(w, r)
	})
}
