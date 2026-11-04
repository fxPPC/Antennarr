package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLogMenuLevels(t *testing.T) {
	for _, mode := range []string{"none", "normal", "debug"} {
		var b bytes.Buffer
		var level slog.LevelVar
		s := FreshSettings()
		s.LoggingLevel = mode
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		level.Set(s.LogLevel())
		l := slog.New(slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: &level}))
		l.Debug("detail")
		l.Warn("warning")
		l.Error("error")
		if strings.Contains(b.String(), "detail") != (mode == "debug") || strings.Contains(b.String(), "warning") != (mode != "none") || strings.Contains(b.String(), "error") != (mode != "none") {
			t.Fatal(mode, b.String())
		}
	}
}
