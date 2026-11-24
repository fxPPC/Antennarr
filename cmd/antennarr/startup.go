package main

import (
	"antennarr/internal/catalog"
	"antennarr/internal/media"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

func ffmpegBinary() (string, error) {
	configured := os.Getenv("ANTENNARR_FFMPEG")
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return env("ANTENNARR_FFMPEG", "ffmpeg"), nil
	}
	if configured == "" {
		self, err := os.Executable()
		if err != nil {
			return "", err
		}
		configured = bundledFFmpeg(self)
		if _, err := os.Stat(filepath.Dir(configured)); os.IsNotExist(err) {
			configured = "ffmpeg"
		}
	}
	binary, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("FFmpeg unavailable: %w", err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(media.ProbeBinary(binary)); err != nil {
		return "", fmt.Errorf("FFprobe unavailable beside FFmpeg: %w", err)
	}
	return binary, nil
}

func bundledFFmpeg(server string) string {
	dir := filepath.Dir(server)
	if runtime.GOOS == "darwin" {
		return filepath.Join(dir, "..", "Resources", "media", "bin", "ffmpeg")
	}
	return filepath.Join(dir, "media", "bin", "ffmpeg.exe")
}

func dataDirectory() (string, error) {
	if path := os.Getenv("ANTENNARR_DATA"); path != "" {
		return filepath.Abs(path)
	}
	var dir string
	var err error
	switch runtime.GOOS {
	case "darwin":
		dir, err = os.UserConfigDir()
	case "windows":
		dir, err = os.UserCacheDir() // LOCALAPPDATA, not a roaming profile
	default:
		return "/data", nil
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Antennarr Beta"), nil
}

// Explain common startup failures without dumping configuration or credentials.
func startupReason(err error) string {
	var se *sqlite.Error
	var syntax *json.SyntaxError
	var value *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_BUSY:
		return "database is busy; check for another process using the catalogue"
	case errors.Is(err, context.DeadlineExceeded):
		return "startup operation timed out"
	case errors.Is(err, os.ErrPermission):
		return "permission denied: the account running Antennarr needs read/write access to the appdata directory and its existing contents"
	case errors.Is(err, syscall.EROFS):
		return "read-only filesystem: choose a writable appdata directory"
	case errors.Is(err, syscall.ENOSPC):
		return "appdata filesystem is full"
	case errors.Is(err, syscall.ENOTDIR):
		return "an appdata path is a file instead of a directory"
	case errors.As(err, &syntax), errors.As(err, &value):
		return "invalid config/config.json: check JSON syntax and value types"
	default:
		return "configuration or storage could not be initialised; check appdata access and configuration values"
	}
}
func startupError(message, root string, err error) {
	attrs := []any{"reason", startupReason(err), "data_directory", root}
	var stage *catalog.StartupError
	if errors.As(err, &stage) {
		attrs = append(attrs, "stage", stage.Stage)
	}
	var se *sqlite.Error
	var errno syscall.Errno
	if errors.As(err, &se) {
		attrs = append(attrs, "sqlite_code", se.Code())
	}
	if errors.As(err, &errno) {
		attrs = append(attrs, "errno", uint64(errno))
	}
	if runtime.GOOS != "windows" {
		attrs = append(attrs, "uid", os.Getuid(), "gid", os.Getgid())
	}
	slog.Error(message, attrs...)
}
