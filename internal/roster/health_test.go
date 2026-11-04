package roster

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestWaitDiagnosticsDoNotRepeatEveryTick(t *testing.T) {
	var b bytes.Buffer
	old := slog.Default()
	defer slog.SetDefault(old)
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug})))
	s := Service{}
	for range 5 {
		s.state("selecting", 0)
		s.state("checks_waiting", 2000000000)
	}
	if strings.Count(b.String(), "checker waiting") != 1 {
		t.Fatal("idle log flood", b.String())
	}
	s.state("checks_waiting", 2000000001)
	if strings.Count(b.String(), "checker waiting") != 2 {
		t.Fatal("new retry time not logged")
	}
}
