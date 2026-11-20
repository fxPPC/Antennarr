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

func main() {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if os.Getenv("ANTENNARR_LISTEN") == "" {
			os.Setenv("ANTENNARR_LISTEN", config.DesktopTunerListen())
		}
		if os.Getenv("ANTENNARR_ADMIN_LISTEN") == "" {
			os.Setenv("ANTENNARR_ADMIN_LISTEN", "127.0.0.1:39192")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Getenv("ANTENNARR_DESKTOP_CHILD") == "1" {
		// Only the desktop launcher owns the pipe's write end. EOF also covers
		// an unexpected launcher exit, including during a maintenance restart.
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			stop()
		}()
	}
	for ctx.Err() == nil && run(ctx) {
	}
}

func run(parent context.Context) bool {
	var initialLevel slog.Level
	levelErr := initialLevel.UnmarshalText([]byte(env("ANTENNARR_LOG_LEVEL", "DEBUG")))
	var level slog.LevelVar
	level.Set(initialLevel)
	logOutput := os.Stdout
	desktopChild := os.Getenv("ANTENNARR_DESKTOP_CHILD") == "1"
	if desktopChild {
		logOutput = os.Stderr // stdout carries only the two menu counters.
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: &level}))
	slog.SetDefault(logger)
	if levelErr != nil {
		slog.Error("invalid logging level")
		os.Exit(1)
	}
	// Storage/tool locations are trusted deployment configuration, never values
	// taken from HTTP requests, provider catalogues, or imported lists.
	root, err := dataDirectory()
	if err != nil {
		slog.Error("cannot determine the appdata directory", "error", err)
		os.Exit(1)
	}
	binary, err := ffmpegBinary()
	if err != nil {
		slog.Error("media tools unavailable", "error", err)
		os.Exit(1)
	}
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
	ports, err := config.ParseTunerPorts(os.Getenv("ANTENNARR_TUNER_PORTS"))
	if err == nil {
		err = ports.ValidateListeners(env("ANTENNARR_LISTEN", ":9192"), os.Getenv("ANTENNARR_ADMIN_LISTEN"), os.Getenv("ANTENNARR_COMPAT_LISTEN"))
	}
	if err != nil {
		slog.Error("invalid tuner listener configuration", "error", err)
		os.Exit(1)
	}
	login, err := config.OpenAdminLogin(root)
	if err != nil {
		startupError("admin credential unavailable", root, err)
		os.Exit(1)
	}
	store, err := catalog.OpenWithDefaults(root)
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
	if err = store.MigratePlaybackQuality(c.Settings.AllowUHD); err != nil {
		slog.Error("quality policy setup failed")
		os.Exit(1)
	}
	if err = store.RefreshMatching(); err != nil {
		slog.Error("channel matching unavailable")
		os.Exit(1)
	}
	base := os.Getenv("ANTENNARR_BASE_URL")
	if base == "" {
		ip := config.PrivateIPv4()
		if ip == "" && (runtime.GOOS == "darwin" || runtime.GOOS == "windows") {
			ip = "127.0.0.1"
		}
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
	checks := &roster.Service{Settings: readChecks, Activity: work, Store: store, Budget: limits, Gateway: checkGateway, Client: client, Binary: binary, Live: func() bool { return m != nil && m.Status().Viewers > 0 }}
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
		os.Exit(1)
	}
	system := systeminfo.New(root, func() (bool, bool) {
		state := checks.CheckerState().State
		return m.Status().Viewers > 0, state == "checking" || state == "discovering"
	})
	statusCollector := &monitor.Collector{Store: store, Playback: m.Status, Live: eng.Status, Work: work.Snapshot, Checker: checks.CheckerState}
	s := tuner.Server{Guard: guard, ChannelList: channelList, BeforeTune: checks.Preempt, Admin: &admin.API{TunerPorts: ports, PrimaryListen: env("ANTENNARR_LISTEN", ":9192"), WithIdle: m.WithIdle, SystemInfo: system.Snapshot, Monitor: statusCollector.Snapshot, StopStream: m.Stop, WithChecksPaused: checks.WithPaused, WithProtectedAccounts: limits.WithProtectedAccounts, ReadLogin: login.Current, ChangeLogin: login.Change, DataRoot: root, BaseURL: base, Idle: func() bool { return m.Status().Viewers == 0 }, Restart: func() {
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
		os.Exit(1)
	}
	for _, profile := range profiles {
		if profile.Enabled {
			if _, err = ports.URL(base, profile.PortOffset); err != nil {
				slog.Error("enabled tuner has no configured address", "profile_id", profile.ID, "error", err)
				os.Exit(1)
			}
		}
	}
	primaryHandler := fleet.PrimaryHandler()
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		// Desktop firewalls allow Plex to reach tuner ports. Administration
		// has its own local listener, even when the primary tuner is absent.
		primaryHandler = fleet.Handler(0)
	}
	srv := http.Server{Addr: env("ANTENNARR_LISTEN", ":9192"), Handler: primaryHandler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
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
			slog.Error("additional tuner listener unavailable", "port", port)
			os.Exit(1)
		}
		extra = append(extra, deviceListener{&http.Server{Handler: fleet.Handler(port - ports.First + 1), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}, listener})
	}
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
		compat = &http.Server{Handler: fleet.FallbackHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	}
	primaryListener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		slog.Error("primary tuner listener unavailable")
		os.Exit(1)
	}
	if desktopChild {
		// An initial count also confirms this child owns every requested port.
		go writeDesktopStats(ctx, os.Stdout, store, m.Status)
	}
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
	slog.Info("tuner starting", "version", "v1.1.0-beta.1", "schema", 9, "replenish_while_playing", c.Settings.ReplenishWhilePlaying, "stage", "ready", "maximum_viewers", c.Settings.MaxOutgoingStreams, "profiles", len(profiles))
	if err = srv.Serve(primaryListener); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		os.Exit(1)
	}
	<-shutdownDone
	// Container restarts stay with its supervisor. Standalone applications
	// close the old store and listeners before applying the staged operation.
	return restartRequested.Load() && (runtime.GOOS == "darwin" || runtime.GOOS == "windows")
}
func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
