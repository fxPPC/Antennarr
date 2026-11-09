package main

import (
	"antennarr/internal/access"
	"antennarr/internal/activity"
	"antennarr/internal/admin"
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/engine"
	"antennarr/internal/media"
	"antennarr/internal/moderation"
	"antennarr/internal/monitor"
	"antennarr/internal/netguard"
	"antennarr/internal/recovery"
	"antennarr/internal/roster"
	"antennarr/internal/stream"
	"antennarr/internal/systeminfo"
	"antennarr/internal/tuner"
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// stdout is the operational log sink; Docker owns rotation and retention.
	var initialLevel slog.Level
	levelErr := initialLevel.UnmarshalText([]byte(env("ANTENNARR_LOG_LEVEL", "DEBUG")))
	var level slog.LevelVar
	level.Set(initialLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &level}))
	slog.SetDefault(logger)
	if levelErr != nil {
		slog.Error("invalid logging level")
		os.Exit(1)
	}
	// Storage/tool locations are trusted deployment configuration, never values
	// taken from HTTP requests, provider catalogues, or imported lists.
	root := env("ANTENNARR_DATA", "/data")
	if err := recovery.ApplyReset(root); err != nil {
		startupError("reset failed", root, err)
		os.Exit(1)
	}
	if err := recovery.ApplyPending(root); err != nil {
		startupError("pending restore could not be applied", root, err)
		os.Exit(1)
	}
	c, err := config.Load(root)
	if err != nil {
		startupError("configuration unavailable", root, err)
		os.Exit(1)
	}
	level.Set(c.Settings.LogLevel())
	login, err := config.OpenAdminLogin(root)
	if err != nil {
		startupError("admin credential unavailable", root, err)
		os.Exit(1)
	}
	store, err := catalog.OpenWithUSDefaults(root)
	if err != nil {
		startupError("catalogue unavailable", root, err)
		os.Exit(1)
	}
	defer store.Close()
	if recovered, e := store.RecoverImportJobs(); e != nil {
		startupError("import recovery failed", root, e)
		os.Exit(1)
	} else if recovered > 0 {
		slog.Info("interrupted imports recovered", "count", recovered)
	}
	if err = store.SetUHDAllowed(c.Settings.AllowUHD); err != nil {
		slog.Error("quality policy setup failed")
		os.Exit(1)
	}
	if err = store.RefreshMatching(); err != nil {
		slog.Error("channel matching unavailable")
		os.Exit(1)
	}
	base := os.Getenv("ANTENNARR_BASE_URL")
	if base == "" {
		ip := privateIP()
		if ip == "" {
			slog.Error("no private interface address")
			os.Exit(1)
		}
		base = "http://" + net.JoinHostPort(ip, "9192")
	}
	var m *stream.Manager
	readChecks := func() config.Settings {
		saved, e := config.Load(root)
		if e != nil {
			return c.Settings
		}
		return saved.Settings
	}
	client := netguard.Client(0) // Each check/import supplies a deadline; pacing waits must not consume an HTTP-client timeout.
	client.Transport = &moderation.Transport{Store: store, Base: client.Transport, Settings: readChecks}
	probeClient := netguard.Client(30 * time.Second)
	probeClient.Transport = &moderation.Transport{Store: store, Base: probeClient.Transport, Settings: readChecks, ObserveOnly: true}
	checkGateway, err := media.NewGateway(probeClient)
	if err != nil {
		slog.Error("check gateway unavailable")
		os.Exit(1)
	}
	defer checkGateway.Close()
	gateway, err := media.NewGateway(nil)
	if err != nil {
		slog.Error("media gateway unavailable")
		os.Exit(1)
	}
	defer gateway.Close()
	limits := budget.New(c.Settings.GlobalConnections, c.Settings.ProviderConnections)
	work := activity.New()
	if err = store.EnableRosters(catalog.RosterPolicy{Target: c.Settings.RosterTarget, Preference: c.Settings.SelectionPreference, Race: c.Settings.StartupRace}); err != nil {
		slog.Error("roster initialisation failed")
		os.Exit(1)
	}
	if err = store.ReconcileRosters(); err != nil {
		slog.Error("roster eligibility refresh failed")
		os.Exit(1)
	}
	checks := &roster.Service{Settings: readChecks, Activity: work, Store: store, Budget: limits, Gateway: checkGateway, Client: client, Binary: env("ANTENNARR_FFMPEG", "ffmpeg"), Live: func() bool { return m != nil && m.Status().Viewers > 0 }}
	binary := env("ANTENNARR_FFMPEG", "ffmpeg")
	eng := &engine.Engine{Activity: work, RaceSize: c.Settings.StartupRace, Buffer: time.Duration(c.Settings.BufferSeconds) * time.Second, StallTimeout: time.Duration(c.Settings.StallTimeoutMS) * time.Millisecond, Catalogue: checks, Budget: limits, Binary: binary, Preempt: checks.Preempt, Copy: func(ctx context.Context, u string, emit func([]byte)) error {
		return gateway.Copy(ctx, binary, u, emit)
	}, Inspect: func(ctx context.Context, b []byte) (media.Info, error) { return media.Inspect(ctx, binary, b) }}
	if os.Getenv("ANTENNARR_TEST_FAILOVER") == "1" {
		eng.Catalogue = engine.FixtureCatalogue{Catalogue: store}
		eng.Copy = engine.FixtureCopy(binary, env("ANTENNARR_FIXTURES", "/opt/antennarr/fixtures"), eng.Copy)
	}
	fixture := stream.FixtureRunner(binary, env("ANTENNARR_FIXTURES", "/opt/antennarr/fixtures"))
	m = stream.New(c.Settings.MaxOutgoingStreams, time.Duration(c.GraceSeconds)*time.Second, func(ctx context.Context, id string, emit func([]byte)) error {
		if id == "720" && os.Getenv("ANTENNARR_TEST_FAILOVER") == "1" {
			return eng.Run(ctx, "test-1", emit)
		}
		if id == "720" || id == "480" || id == "mpeg2" {
			return fixture(ctx, id, emit)
		}
		return eng.Run(ctx, id, emit)
	})
	checks.ActiveChannels = func() map[string]bool {
		out := map[string]bool{}
		for _, v := range m.Status().Outgoing {
			out[v.Channel] = true
		}
		return out
	}

	checks.LivePairReady = func(ch string) bool {
		for _, v := range eng.Status() {
			if v.Channel == ch && v.Primary != 0 && v.Ghost != 0 && v.Standby == "receiving" {
				return true
			}
		}
		return false
	}

	channelList := func() []config.Channel {
		selected, e := store.Channels()
		if e != nil {
			return nil
		}
		out := []config.Channel{}
		for _, v := range selected {
			if v.Enabled {
				out = append(out, config.Channel{ID: v.ID, Number: v.Number, Name: v.Name})
			}
		}
		return out
	}
	restart := make(chan struct{}, 1)
	guard, guardErr := access.New(os.Getenv("ANTENNARR_TRUSTED_PROXIES"))
	if guardErr != nil {
		slog.Error("trusted proxy configuration invalid")
		os.Exit(1)
	}
	system := systeminfo.New(root, func() (bool, bool) {
		state := checks.CheckerState().State
		return m.Status().Viewers > 0, state == "checking" || state == "discovering"
	})
	statusCollector := &monitor.Collector{Store: store, Playback: m.Status, Live: eng.Status, Work: work.Snapshot, Checker: checks.CheckerState}
	s := tuner.Server{Guard: guard, ChannelList: channelList, BeforeTune: checks.Preempt, Admin: &admin.API{WithIdle: m.WithIdle, SystemInfo: system.Snapshot, Monitor: statusCollector.Snapshot, StopStream: m.Stop, WithChecksPaused: checks.WithPaused, WithProtectedAccounts: limits.WithProtectedAccounts, ReadLogin: login.Current, ChangeLogin: login.Change, DataRoot: root, BaseURL: base, Idle: func() bool { return m.Status().Viewers == 0 }, Restart: func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	}, Activity: work, ReadSettings: func() (config.Settings, error) { v, e := config.Load(root); return v.Settings, e }, SaveSettings: func(v config.Settings) error {
		copy := c
		copy.Settings = v
		e := config.Save(root, copy)
		if e == nil {
			checks.Preempt()
			if e = store.EnableRosters(catalog.RosterPolicy{Target: v.RosterTarget, Preference: v.SelectionPreference, Race: v.StartupRace}); e != nil {
				return e
			}
			if v.AllowUHD != store.UHDAllowed() {
				if e = store.SetUHDAllowed(v.AllowUHD); e != nil {
					return e
				}
			}
			level.Set(v.LogLevel())
			m.SetLimit(v.MaxOutgoingStreams)
			limits.SetLimits(v.GlobalConnections, v.ProviderConnections)
		}
		return e
	}, Store: store, Client: client}, Config: c, Streams: m, BaseURL: base, ReadLogin: login.Current}
	srv := http.Server{Addr: env("ANTENNARR_LISTEN", ":9192"), Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	var adminServer *http.Server
	var adminListener net.Listener
	if addr := os.Getenv("ANTENNARR_ADMIN_LISTEN"); addr != "" {
		adminListener, err = net.Listen("tcp", addr)
		if err != nil {
			slog.Error("administration listener unavailable")
			os.Exit(1)
		}
		adminServer = &http.Server{Handler: s.AdminHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	}
	var compat *http.Server
	var compatListener net.Listener
	if addr := os.Getenv("ANTENNARR_COMPAT_LISTEN"); addr != "" {
		compatListener, err = net.Listen("tcp", addr)
		if err != nil {
			slog.Error("compatibility listener unavailable")
			os.Exit(1)
		}
		compat = &http.Server{Handler: s.StreamHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go system.Run(ctx)
	defer stop()
	if adminServer != nil {
		go func() {
			if err := adminServer.Serve(adminListener); err != nil && err != http.ErrServerClosed {
				slog.Error("administration listener stopped")
				stop()
			}
		}()
	}
	if compat != nil {
		go func() {
			if err := compat.Serve(compatListener); err != nil && err != http.ErrServerClosed {
				slog.Error("compatibility listener stopped")
				stop()
			}
		}()
	}
	go func() {
		select {
		case <-restart:
			stop()
		case <-ctx.Done():
		}
	}()
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		checks.Run(ctx)
	}()
	defer func() { stop(); <-jobsDone }()
	go func() {
		<-ctx.Done()
		m.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if eng.FlushMetrics(shutdown) != nil {
			slog.Warn("history flush exceeded shutdown deadline")
		}
		if adminServer != nil {
			_ = adminServer.Shutdown(shutdown)
		}
		if compat != nil {
			_ = compat.Shutdown(shutdown)
		}
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("tuner starting", "version", "v1.0.1-dev", "schema", 8, "replenish_while_playing", c.Settings.ReplenishWhilePlaying, "stage", "ready", "tuners", c.Settings.MaxOutgoingStreams)
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		os.Exit(1)
	}
}
func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
func privateIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ip, _, e := net.ParseCIDR(a.String())
		if e == nil && ip.To4() != nil && !ip.IsLoopback() && ip.IsPrivate() {
			return ip.String()
		}
	}
	return ""
}
