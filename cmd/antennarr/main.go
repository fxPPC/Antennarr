package main

import (
	"antennarr/internal/access"
	"antennarr/internal/activity"
	"antennarr/internal/admin"
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/desktopstats"
	"antennarr/internal/engine"
	"antennarr/internal/guide"
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
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

func serverMain() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var output io.Writer = os.Stdout
	var counts func(desktopstats.Counters) error
	if os.Getenv("ANTENNARR_DESKTOP_CHILD") == "1" {
		output = os.Stderr
		encoder := json.NewEncoder(os.Stdout)
		counts = func(v desktopstats.Counters) error { return encoder.Encode(v) }

		// Only the desktop launcher owns the pipe's write end. EOF also covers
		// an unexpected launcher exit, including during a maintenance restart.
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			stop()
		}()
	}
	if err := serve(ctx, output, counts); err != nil {
		os.Exit(1)
	}
}

// serve owns the complete lifetime of the server, including requested desktop maintenance restarts.
func serve(ctx context.Context, logOutput io.Writer, report func(desktopstats.Counters) error) error {
	for ctx.Err() == nil {
		restart, err := run(ctx, logOutput, report)
		if err != nil || !restart {
			return err
		}
	}
	return nil
}

var errServerStopped = errors.New("Antennarr could not run. See the server log for details.")

func run(parent context.Context, logOutput io.Writer, report func(desktopstats.Counters) error) (bool, error) {
	var initialLevel slog.Level
	levelErr := initialLevel.UnmarshalText([]byte(env("ANTENNARR_LOG_LEVEL", "DEBUG")))
	var level slog.LevelVar
	level.Set(initialLevel)
	logger := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: &level}))
	slog.SetDefault(logger)
	if levelErr != nil {
		slog.Error("invalid logging level")
		return false, errServerStopped
	}
	// Storage/tool locations are trusted deployment configuration, never values
	// taken from HTTP requests, provider catalogues, or imported lists.
	root, err := dataDirectory()
	if err != nil {
		slog.Error("cannot determine the appdata directory", "error", err)
		return false, errServerStopped
	}
	binary, err := ffmpegBinary()
	if err != nil {
		slog.Error("media tools unavailable", "error", err)
		return false, errServerStopped
	}
	if err := recovery.ApplyReset(root); err != nil {
		startupError("reset failed", root, err)
		return false, errServerStopped
	}
	if err := recovery.ApplyPending(root); err != nil {
		startupError("pending restore could not be applied", root, err)
		return false, errServerStopped
	}
	c, err := config.Load(root)
	if err != nil {
		startupError("configuration unavailable", root, err)
		return false, errServerStopped
	}
	level.Set(c.Settings.LogLevel())
	desktop := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	listeners, err := config.ResolveListeners(root, c.PortRange, desktop)
	ports := listeners.Extra
	if err != nil {
		slog.Error("invalid tuner listener configuration", "error", err)
		return false, errServerStopped
	}
	login, err := config.OpenAdminLogin(root)
	if err != nil {
		startupError("admin credential unavailable", root, err)
		return false, errServerStopped
	}
	store, err := catalog.OpenWithDefaults(root)
	if err != nil {
		startupError("catalogue unavailable", root, err)
		return false, errServerStopped
	}
	defer store.Close()
	if recovered, e := store.RecoverImportJobs(); e != nil {
		startupError("import recovery failed", root, e)
		return false, errServerStopped
	} else if recovered > 0 {
		slog.Info("interrupted imports recovered", "count", recovered)
	}
	if err = store.MigratePlaybackQuality(c.Settings.AllowUHD); err != nil {
		slog.Error("quality policy setup failed")
		return false, errServerStopped
	}
	if err = store.RefreshMatching(); err != nil {
		slog.Error("channel matching unavailable")
		return false, errServerStopped
	}
	base, err := listeners.BaseURL(os.Getenv("ANTENNARR_BASE_URL"), desktop)
	if err != nil {
		slog.Error("cannot determine tuner address", "error", err)
		return false, errServerStopped
	}
	var m *stream.Manager
	readChecks := func() config.Settings {
		saved, e := config.Load(root)
		if e != nil {
			fallback := c.Settings
			fallback.ScansPaused = true
			return fallback
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
		return false, errServerStopped
	}
	defer checkGateway.Close()
	gateway, err := media.NewGateway(nil)
	if err != nil {
		slog.Error("media gateway unavailable")
		return false, errServerStopped
	}
	defer gateway.Close()
	limits := budget.New(c.Settings.GlobalConnections, c.Settings.ProviderConnections)
	work := activity.New()
	if err = store.EnableRosters(catalog.RosterPolicy{Target: c.Settings.RosterTarget, Preference: c.Settings.SelectionPreference, Race: c.Settings.StartupRace}); err != nil {
		slog.Error("roster initialisation failed")
		return false, errServerStopped
	}
	if err = store.ReconcileRosters(); err != nil {
		slog.Error("roster eligibility refresh failed")
		return false, errServerStopped
	}
	checks := &roster.Service{Settings: readChecks, Activity: work, Store: store, Budget: limits, Gateway: checkGateway, Client: client, Binary: binary, Live: func() bool { return m != nil && m.Status().Viewers > 0 }}
	guides, err := guide.New(store, client)
	if err != nil {
		startupError("programme guide unavailable", root, err)
		return false, errServerStopped
	}
	guides.Budget = limits
	portalWork := &admin.PortalWork{}
	preempt := func() {
		portalWork.Preempt()
		guides.Preempt()
		checks.Preempt()
	}
	eng := &engine.Engine{Activity: work, RaceSize: c.Settings.StartupRace, Buffer: time.Duration(c.Settings.BufferSeconds) * time.Second, StallTimeout: time.Duration(c.Settings.StallTimeoutMS) * time.Millisecond, Catalogue: checks, Budget: limits, Binary: binary, Preempt: preempt, Copy: func(ctx context.Context, u string, emit func([]byte)) error {
		url, err := store.ResolvePortalStream(ctx, probeClient, u)
		if err != nil {
			return err
		}
		return gateway.Copy(ctx, binary, url, emit)
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
		selected, e := store.ChannelsForProfile("primary")
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
	ctx, stop := context.WithCancel(parent)
	defer stop()
	var restartRequested atomic.Bool
	guard, guardErr := access.New(os.Getenv("ANTENNARR_TRUSTED_PROXIES"))
	if guardErr != nil {
		slog.Error("trusted proxy configuration invalid")
		return false, errServerStopped
	}
	system := systeminfo.New(root, func() (bool, bool) {
		state := checks.CheckerState().State
		return m.Status().Viewers > 0, state == "checking" || state == "discovering"
	})
	statusCollector := &monitor.Collector{Store: store, Playback: m.Status, Live: eng.Status, Work: work.Snapshot, Checker: checks.CheckerState}
	s := tuner.Server{Guide: guides, Guard: guard, ChannelList: channelList, BeforeTune: preempt, Admin: &admin.API{GuideAdmin: guides.Admin, PortalBudget: limits.Reserve, PortalWork: portalWork, TunerPorts: ports, PrimaryListen: listeners.Primary, AdminListen: listeners.Admin, CompatListen: listeners.Compat, PortRange: listeners.Range, WithIdle: m.WithIdle, SystemInfo: system.Snapshot, Monitor: statusCollector.Snapshot, StopStream: m.Stop, WithChecksPaused: checks.WithPaused, WithProtectedAccounts: limits.WithProtectedAccounts, ReadLogin: login.Current, ChangeLogin: login.Change, DataRoot: root, BaseURL: base, Idle: func() bool { return m.Status().Viewers == 0 }, Restart: func() {
		restartRequested.Store(true)
		stop()
	}, Activity: work, ReadSettings: func() (config.Settings, error) { v, e := config.Load(root); return v.Settings, e }, SaveSettings: func(v config.Settings) error {
		copy := c
		copy.Settings = v
		save := func() error { return config.Save(root, copy) }
		var e error
		if v.ScansPaused {
			e = checks.WithPaused(save)
		} else {
			e = save()
		}
		if e == nil {
			checks.Preempt()
			if e = store.EnableRosters(catalog.RosterPolicy{Target: v.RosterTarget, Preference: v.SelectionPreference, Race: v.StartupRace}); e != nil {
				return e
			}
			level.Set(v.LogLevel())
			m.SetLimit(v.MaxOutgoingStreams)
			limits.SetLimits(v.GlobalConnections, v.ProviderConnections)
		}
		return e
	}, Store: store, Client: client}, Config: c, Streams: m, BaseURL: base, ReadLogin: login.Current}
	fleet := tuner.Fleet{Store: store, Template: &s, Ports: ports}
	profiles, err := store.Profiles()
	if err != nil {
		slog.Error("tuner profiles unavailable")
		return false, errServerStopped
	}
	for _, profile := range profiles {
		if profile.Enabled {
			if _, err = ports.URL(base, profile.PortOffset); err != nil {
				slog.Error("enabled tuner has no configured address", "profile_id", profile.ID, "error", err)
				return false, errServerStopped
			}
		}
	}
	primaryHandler := fleet.PrimaryHandler()
	if desktop || listeners.Range != "" {
		// Standalone apps and seven-port installations keep administration off every tuner port.
		primaryHandler = fleet.Handler(0)
	}
	srv := http.Server{Addr: listeners.Primary, Handler: primaryHandler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	// Bind the bounded private range once. Adding/enabling a profile then needs
	// no restart, and an unassigned port returns 404 rather than another tuner.
	bindHost, _, _ := net.SplitHostPort(srv.Addr)
	type deviceListener struct {
		server   *http.Server
		listener net.Listener
	}
	var extra []deviceListener
	for port := ports.First; port <= ports.Last; port++ {
		listener, e := net.Listen("tcp", net.JoinHostPort(bindHost, strconv.Itoa(port)))
		if e != nil {
			slog.Error("additional tuner listener unavailable", "port", port, "error", e)
			return false, errServerStopped
		}
		defer listener.Close()
		extra = append(extra, deviceListener{&http.Server{Handler: fleet.Handler(port - ports.First + 1), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}, listener})
	}
	var adminServer *http.Server
	var adminListener net.Listener
	if addr := listeners.Admin; addr != "" {
		adminListener, err = net.Listen("tcp", addr)
		if err != nil {
			slog.Error("administration listener unavailable", "error", err)
			return false, errServerStopped
		}
		defer adminListener.Close()
		adminServer = &http.Server{Handler: s.AdminHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	}
	var compat *http.Server
	var compatListener net.Listener
	if addr := listeners.Compat; addr != "" {
		compatListener, err = net.Listen("tcp", addr)
		if err != nil {
			slog.Error("compatibility listener unavailable", "error", err)
			return false, errServerStopped
		}
		defer compatListener.Close()
		compat = &http.Server{Handler: fleet.FallbackHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	}
	primaryListener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		slog.Error("primary tuner listener unavailable", "error", err)
		return false, errServerStopped
	}
	defer primaryListener.Close()
	if report != nil {
		// The first count confirms this server owns every requested listener.
		done := make(chan struct{})
		go func() { defer close(done); writeDesktopStats(ctx, report, store, m.Status) }()
		defer func() { stop(); <-done }()
	}
	guideDone := make(chan struct{})
	go func() {
		defer close(guideDone)
		guides.Run(ctx)
	}()
	defer func() { stop(); <-guideDone }()
	go system.Run(ctx)
	for _, device := range extra {
		go func() {
			if e := device.server.Serve(device.listener); e != nil && e != http.ErrServerClosed {
				slog.Error("additional tuner listener stopped")
				stop()
			}
		}()
	}
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
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		checks.Run(ctx)
	}()
	defer func() { stop(); <-jobsDone }()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
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
		for _, device := range extra {
			_ = device.server.Shutdown(shutdown)
		}
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("tuner starting", "version", "v1.2.0-alpha.1", "schema", catalog.SchemaVersion, "replenish_while_playing", c.Settings.ReplenishWhilePlaying, "stage", "ready", "maximum_viewers", c.Settings.MaxOutgoingStreams, "profiles", len(profiles))
	if err = srv.Serve(primaryListener); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		return false, errServerStopped
	}
	<-shutdownDone
	// Container restarts stay with its supervisor. Standalone applications
	// close the old store and listeners before applying the staged operation.
	return restartRequested.Load() && (runtime.GOOS == "darwin" || runtime.GOOS == "windows"), nil
}
func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
