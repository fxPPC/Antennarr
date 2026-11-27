package main

import (
	"antennarr/internal/catalog"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestStartupReason(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{&os.PathError{Op: "mkdir", Path: "/data/config", Err: syscall.EACCES}, "permission denied"}, {syscall.EROFS, "read-only"}, {syscall.ENOSPC, "full"}, {&json.SyntaxError{}, "JSON"}} {
		if got := startupReason(tc.err); !strings.Contains(got, tc.want) {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}

func TestDataDirectoryHonoursOverridesAndAppDefaults(t *testing.T) {
	t.Setenv("ANTENNARR_DATA", "relative appdata")
	expected, err := filepath.Abs("relative appdata")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := dataDirectory(); err != nil || got != expected {
		t.Fatal(got, err)
	}
	t.Setenv("ANTENNARR_DATA", "")
	home := t.TempDir()
	expected = "/data"
	switch runtime.GOOS {
	case "darwin":
		t.Setenv("HOME", home)
		expected = filepath.Join(home, "Library", "Application Support", "Antennarr Beta")
	case "windows":
		t.Setenv("LOCALAPPDATA", home)
		expected = filepath.Join(home, "Antennarr Beta")
	}
	if got, err := dataDirectory(); err != nil || got != expected {
		t.Fatal(got, err)
	}
}

func TestNativeMediaRequiresCompanionProbe(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("native desktop media lookup")
	}
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	binary := filepath.Join(dir, "ffmpeg"+ext)
	t.Setenv("ANTENNARR_FFMPEG", binary)
	if _, err := ffmpegBinary(); err == nil || !strings.Contains(err.Error(), "FFmpeg unavailable") {
		t.Fatal("missing FFmpeg", err)
	}
	if err := os.WriteFile(binary, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ffmpegBinary(); err == nil || !strings.Contains(err.Error(), "FFprobe unavailable") {
		t.Fatal("missing FFprobe", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ffprobe"+ext), nil, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := ffmpegBinary(); err != nil || got != binary {
		t.Fatal(got, err)
	}
}

func TestCatalogueStartupLog(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "database")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antennarr.db"), bytes.Repeat([]byte("private database contents"), 32), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.OpenWithDefaults(root)
	if err == nil {
		store.Close()
		t.Fatal("opened invalid SQLite database")
	}
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(previous)
	startupError("catalogue unavailable", root, errors.Join(err, &os.PathError{Op: "open", Path: "private filename", Err: syscall.EACCES}))
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["stage"] != "enable WAL" || record["sqlite_code"] != float64(26) || record["errno"] != float64(syscall.EACCES) || strings.Contains(buf.String(), "private filename") || strings.Contains(buf.String(), "private database contents") {
		t.Fatalf("unexpected startup log: %s", buf.String())
	}
}
