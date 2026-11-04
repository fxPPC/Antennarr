package catalog

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestPlaybackEventsLogWithoutSourceCredentials(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var b bytes.Buffer
	old := slog.Default()
	defer slog.SetDefault(old)
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, nil)))
	s.Event("failover", "channel-1", 42, "https://provider.example/live/user/fixture-password/1.ts")
	s.Event("source_failure", "channel-1", 42, "fixture-password")
	log := b.String()
	if !strings.Contains(log, "live backup promoted") || !strings.Contains(log, "playback source failed") || strings.Contains(log, "fixture-password") || strings.Contains(log, "provider.example") {
		t.Fatal("missing or unsafe event log")
	}
}
