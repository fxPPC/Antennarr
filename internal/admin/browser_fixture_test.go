package admin

// This loopback-only, disposable fixture is compiled only into the test binary.
// It runs no checker and makes no external requests; it never uses live data.
import (
	"antennarr/internal/activity"
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/engine"
	"antennarr/internal/ingest"
	"antennarr/internal/monitor"
	"antennarr/internal/recovery"
	"antennarr/internal/roster"
	"antennarr/internal/stream"
	"antennarr/internal/systeminfo"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserFixture(t *testing.T) {
	if os.Getenv("ANTENNARR_BROWSER_FIXTURE") != "1" {
		t.Skip("disposable browser fixture not requested")
	}
	root := os.Getenv("ANTENNARR_BROWSER_DATA")
	if root == "" {
		t.Fatal("choose a disposable data directory")
	}
	if _, e := os.Stat(root); e == nil {
		t.Fatal("fixture directory must not already exist")
	}
	os.MkdirAll(root, 0700)
	c, e := config.Load(root)
	if e != nil {
		t.Fatal(e)
	}
	config.AdminPassword(root)
	login, _ := config.OpenAdminLogin(root)
	login.Change(login.Current().Password, "fixture-admin", "fixture-password")
	c.Settings.RosterTarget = 2
	config.Save(root, c)
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	s.SetUHDAllowed(false)
	s.EnableRosters(catalog.RosterPolicy{Target: 2, Preference: "stability", Race: 5})
	for n, name := range []string{"CNN", "MTV", "MS NOW", "FOX News", "Discovery", "VH1", "TLC", "Animal Planet"} {
		id, e := s.SaveChannel(catalog.Channel{Number: fmt.Sprint(101 + n), Name: name, Region: "US", Enabled: true, Aliases: []string{"-international"}})
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 3; i++ {
			entry, _ := ingest.FromURL(fmt.Sprintf("https://fixture%d.example/%d.ts", i, n), name, " ")
			if _, e = s.Import([]ingest.Entry{entry}, "Browser fixture"); e != nil {
				t.Fatal(e)
			}
		}
		cs, _ := s.Candidates(id, false)
		count := 0
		if n == 0 {
			count = 2
		} else if n == 1 {
			count = 1
		}
		for _, v := range cs[:count] {
			s.Record(v.StreamID, catalog.Result{Success: true})
		}
	}
	for i := 0; i < 125; i++ {
		entry, _ := ingest.FromURL(fmt.Sprintf("https://page-%d.fixture.invalid/get.php?username=fixture-%03d&password=fixture", i%4, i), "", "")
		if _, e = s.Import([]ingest.Entry{entry}, "pagination fixture"); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	for {
		if e = recovery.ApplyReset(root); e != nil {
			t.Fatal(e)
		}
		if e = recovery.ApplyPending(root); e != nil {
			t.Fatal(e)
		}
		c, e = config.Load(root)
		if e != nil {
			t.Fatal(e)
		}
		login, e = config.OpenAdminLogin(root)
		if e != nil {
			t.Fatal(e)
		}
		s, e = catalog.OpenWithUSDefaults(root)
		if e != nil {
			t.Fatal(e)
		}
		s.SetUHDAllowed(c.Settings.AllowUHD)
		s.EnableRosters(catalog.RosterPolicy{Target: c.Settings.RosterTarget, Preference: c.Settings.SelectionPreference, Race: c.Settings.StartupRace})
		s.EnsureRosterPass(time.Now().Unix())
		work := activity.New()
		if os.Getenv("ANTENNARR_BROWSER_REMOVAL_FIXTURE") == "1" {
			s.Event("account_removed", "", 0, "Deleted account #999: 2 consecutive failed account checks; 8 streams removed")
		}
		if os.Getenv("ANTENNARR_BROWSER_TIMEOUT_FIXTURE") == "1" {
			work.Begin("account_discovery", "Fetching channel catalogue on account #42", 42, 0).Finish("timeout")
		}
		if os.Getenv("ANTENNARR_BROWSER_DIAGNOSTICS_FIXTURE") == "1" {
			j := work.Begin("account_discovery", "Fetching channel catalogue on account #42", 42, 0)
			j.Detail("catalogue", "http_error", 503)
			j.Finish("failed")
			s.DB.Exec("INSERT OR REPLACE INTO settings(name,value) VALUES(?, '2')", fmt.Sprintf("accounts_retired_minute_%d", time.Now().Unix()/60))
		}
		budget := budget.New(c.Settings.GlobalConnections, c.Settings.ProviderConnections)
		m := stream.New(c.Settings.MaxOutgoingStreams, time.Second, func(ctx context.Context, _ string, emit func([]byte)) error {
			tick := time.NewTicker(50 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-tick.C:
					emit([]byte{0x47, 0, 0, 0})
				}
			}
		})
		collector := &monitor.Collector{Store: s, Playback: m.Status, Live: func() []engine.Status { return nil }, Work: work.Snapshot, Checker: func() roster.CheckerState { return roster.CheckerState{State: "waiting_candidates"} }}
		if os.Getenv("ANTENNARR_BROWSER_STREAMS") == "1" {
			chs, _ := s.Channels()
			m.Subscribe(chs[0].ID, "fixture")
			m.Subscribe(chs[0].ID, "fixture")
			m.Subscribe(chs[1].ID, "fixture")
		}

		var server *http.Server
		restart := false
		shutdownDone := make(chan struct{})
		sys := systeminfo.New(root, func() (bool, bool) { return false, false })
		a := &API{SystemInfo: sys.Snapshot, Monitor: collector.Snapshot, StopStream: m.Stop, DataRoot: root, BaseURL: "http://antennarr:9192", Store: s, Activity: work, ReadLogin: login.Current, ChangeLogin: login.Change, Idle: func() bool { return m.Status().Viewers == 0 }, WithChecksPaused: func(fn func() error) error { return fn() }, WithProtectedAccounts: budget.WithProtectedAccounts, ReadSettings: func() (config.Settings, error) { cfg, e := config.Load(root); return cfg.Settings, e }, SaveSettings: func(v config.Settings) error {
			cfg, e := config.Load(root)
			if e != nil {
				return e
			}
			cfg.Settings = v
			if e = config.Save(root, cfg); e != nil {
				return e
			}
			s.EnableRosters(catalog.RosterPolicy{Target: v.RosterTarget, Preference: v.SelectionPreference, Race: v.StartupRace})
			s.SetUHDAllowed(v.AllowUHD)
			m.SetLimit(v.MaxOutgoingStreams)
			budget.SetLimits(v.GlobalConnections, v.ProviderConnections)
			return nil
		}, Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			time.Sleep(100 * time.Millisecond)
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("Host: https://new.fixture.example\nUser: fixture-user\nPass: fixture-pass\n"))}, nil
		})}}
		a.Restart = func() {
			restart = true
			go func() { time.Sleep(50 * time.Millisecond); server.Shutdown(context.Background()); close(shutdownDone) }()
		}
		mux := http.NewServeMux()
		a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
		mux.HandleFunc("GET /{$}", a.Page)
		// Test-only iframe surface for a real 390 px CSS viewport. Production
		// retains frame-ancestors none and never exposes these routes.
		mux.HandleFunc("GET /preview", func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			a.Page(rec, r)
			w.Header().Set("Content-Type", "text/html")
			w.Write(rec.Body.Bytes())
		})
		mux.HandleFunc("GET /mobile", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<!doctype html><title>Antennarr mobile fixture</title><body style="margin:0;background:#080f15"><iframe title="390 pixel mobile preview" src="/preview#status" style="width:390px;height:1100px;border:0"></iframe></body>`)
		})
		mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { response(w, m.Status()) })
		server = &http.Server{Addr: "127.0.0.1:39201", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		t.Log("Disposable browser fixture ready at http://127.0.0.1:39201")
		e = server.ListenAndServe()
		if e != http.ErrServerClosed {
			t.Fatal(e)
		}
		<-shutdownDone
		m.Close()
		s.Close()
		if !restart {
			return
		}
	}
}
