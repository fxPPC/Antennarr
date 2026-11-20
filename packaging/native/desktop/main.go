//go:build darwin || windows

// The desktop launcher and headless mode start exactly the same server.
package main

import (
	"antennarr/internal/config"
	"antennarr/internal/desktopstats"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const (
	starting = iota
	ready
	failed
	stopped
)

type session struct {
	counters  desktopstats.Counters
	countedAt time.Time
	mu        sync.Mutex
	state     int
	detail    string
	adminURL  string
	data      string
	stopPipe  *os.File
	done      chan struct{}
}

func (s *session) status() (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.detail
}
func (s *session) update(status int, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state, s.detail = status, detail
}
func (s *session) stop() {
	if s.stopPipe != nil {
		_ = s.stopPipe.Close()
	}
}

func defaultEnv(name, value string) string {
	if existing := os.Getenv(name); existing != "" {
		return existing
	}
	os.Setenv(name, value)
	return value
}

func privateAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("check the listener address %q: %w", listen, err)
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}).String(), nil
}

func (s *session) start(headless bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(self)
	media := filepath.Join(dir, "media", "bin")
	ext := ".exe"
	if runtime.GOOS == "darwin" {
		media = filepath.Join(dir, "..", "Resources", "media", "bin")
		ext = ""
	}
	// Give the packaged DLLs and tools precedence over unrelated installations.
	if err := os.Setenv("PATH", media+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return err
	}
	ffmpeg := defaultEnv("ANTENNARR_FFMPEG", filepath.Join(media, "ffmpeg"+ext))
	ffmpeg, err = exec.LookPath(ffmpeg)
	if err != nil {
		return fmt.Errorf("FFmpeg could not be found. Extract the complete Antennarr download or correct ANTENNARR_FFMPEG: %w", err)
	}
	probe := filepath.Join(filepath.Dir(ffmpeg), "ffprobe"+ext)
	for _, tool := range []string{ffmpeg, probe} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, tool, "-version")
		hideConsole(cmd)
		err := cmd.Run()
		cancel()
		if err != nil {
			return fmt.Errorf("%s could not start. Extract the complete download, including its media tools and libraries: %w", filepath.Base(tool), err)
		}
	}
	if err := os.Setenv("ANTENNARR_FFMPEG", ffmpeg); err != nil {
		return err
	}
	if s.data = os.Getenv("ANTENNARR_DATA"); s.data == "" {
		var base string
		if runtime.GOOS == "windows" {
			base = os.Getenv("LOCALAPPDATA")
		} else {
			base, err = os.UserConfigDir()
		}
		if err != nil || base == "" {
			return errors.New("choose a writable local folder with ANTENNARR_DATA")
		}
		s.data = filepath.Join(base, "Antennarr Beta")
	}
	s.data, err = filepath.Abs(s.data)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(s.data, 0700); err != nil {
		return fmt.Errorf("cannot create app data: %w", err)
	}
	os.Setenv("ANTENNARR_DATA", s.data)
	primary, err := privateAddress(defaultEnv("ANTENNARR_LISTEN", config.DesktopTunerListen()))
	if err != nil {
		return err
	}
	defaultEnv("ANTENNARR_BASE_URL", primary)
	s.adminURL, err = privateAddress(defaultEnv("ANTENNARR_ADMIN_LISTEN", "127.0.0.1:39192"))
	if err != nil {
		return err
	}
	logPath := filepath.Join(s.data, "desktop.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 2<<20 {
		if err = os.Rename(logPath, logPath+".old"); err != nil {
			return fmt.Errorf("rotate desktop log: %w", err)
		}
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot write the desktop log: %w", err)
	}

	server := exec.Command(filepath.Join(dir, "antennarr-server"+ext))
	hideConsole(server)
	server.Env = append(os.Environ(), "ANTENNARR_DESKTOP_CHILD=1")
	server.Stderr = log
	if headless {
		server.Stderr = io.MultiWriter(log, os.Stderr)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		log.Close()
		return err
	}
	defer reader.Close()
	server.Stdin = reader
	statsReader, statsWriter, err := os.Pipe()
	if err != nil {
		writer.Close()
		log.Close()
		return err
	}
	defer statsWriter.Close()
	server.Stdout = statsWriter
	if err = server.Start(); err != nil {
		statsReader.Close()
		log.Close()
		writer.Close()
		return fmt.Errorf("could not start the Antennarr server: %w", err)
	}
	s.stopPipe, s.done = writer, make(chan struct{})
	go func() {
		err := server.Wait()
		log.Close()
		if err != nil {
			s.update(failed, "Server stopped. Check that the private ports are available and see "+logPath+". "+err.Error())
		} else {
			s.update(stopped, "Server stopped.")
		}
		close(s.done)
	}()
	go s.readStats(statsReader)
	go s.watchReady()
	return nil
}

func (s *session) watchReady() {
	client := http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-tick.C:
			r, err := client.Get(s.adminURL + "/healthz")
			if err != nil {
				continue
			}
			var health struct{ Status, Stage string }
			err = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&health)
			r.Body.Close()
			if r.StatusCode == http.StatusOK && err == nil && health.Status == "ok" && health.Stage == "ready" {
				s.mu.Lock()
				// A different local service may already own this port. Open the
				// dashboard only after our child also sends its own status.
				confirmed := !s.countedAt.IsZero()
				if s.state == starting && confirmed {
					s.state, s.detail = ready, "Antennarr is running."
				}
				s.mu.Unlock()
				if confirmed {
					return
				}
			}
		}
	}
}

func (s *session) settingsURL() string { return s.adminURL + "/admin#admin" }

func (s *session) menuReport(enabled, disabled, quit bool) error {
	s.mu.Lock()
	viewers, channels := s.counters.Viewers, s.counters.WithFeeds
	observed := !s.countedAt.IsZero()
	s.mu.Unlock()
	if !observed {
		return errors.New("desktop counters unavailable")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"viewers": viewers, "channels": channels, "settings_url": s.settingsURL(),
		"login_enabled": enabled, "login_disabled": disabled, "quit": quit,
	})
}

func (s *session) checkMenu() error {
	timeout := time.NewTimer(45 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.done:
			return errors.New("server stopped before menu test")
		case <-timeout.C:
			return errors.New("timed out waiting for desktop status")
		case <-tick.C:
			if _, _, ok := s.stats(); ok {
				return menuSmoke(s)
			}
		}
	}
}

func run() error {
	testMenu := os.Getenv("ANTENNARR_MENU_TEST") == "1"
	headless := testMenu || os.Getenv("ANTENNARR_HEADLESS") == "1"
	if !headless {
		release, previous, err := beginDesktop()
		if err != nil {
			return err
		}
		if previous {
			return nil
		}
		defer release()
	}
	s := &session{detail: "Starting Antennarr…"}
	if err := s.start(headless); err != nil {
		return err
	}
	defer s.stop()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			s.stop()
		case <-s.done:
		}
	}()
	var err error
	if testMenu {
		err = s.checkMenu()
		s.stop()
	} else if !headless {
		desktop(s)
		s.stop()
	}
	<-s.done
	if state, detail := s.status(); state == failed {
		return errors.New(detail)
	}
	return err
}

func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if os.Getenv("ANTENNARR_HEADLESS") != "1" && os.Getenv("ANTENNARR_MENU_TEST") != "1" {
			showError(err.Error())
		}
		os.Exit(1)
	}
}
