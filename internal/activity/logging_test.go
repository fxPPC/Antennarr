package activity

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLogsExcludeFreeFormMessagesAndKeepRoutineWorkQuiet(t *testing.T) {
	var b bytes.Buffer
	old := slog.Default()
	defer slog.SetDefault(old)
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, nil)))
	tr := New()
	tr.Begin("stream_check", "https://secret.example/user/password", 1, 2).Finish("healthy")
	if b.Len() != 0 {
		t.Fatal("routine check flooded INFO log")
	}
	tr.Begin("manual_import", "password=fixture-secret", 1, 2).Finish("imported")
	if !strings.Contains(b.String(), "task finished") || strings.Contains(b.String(), "fixture-secret") {
		t.Fatal("missing or unsafe task log")
	}
	b.Reset()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug})))
	tr.Begin("stream_check", "password=fixture-secret", 1, 2).Finish("deferred")
	if !strings.Contains(b.String(), "DEBUG") || strings.Contains(b.String(), "fixture-secret") {
		t.Fatal("debug task log unsafe or absent")
	}
}

func TestTimeoutWarnsWithoutLoggingCredentials(t *testing.T) {
	var b bytes.Buffer
	old := slog.Default()
	defer slog.SetDefault(old)
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, nil)))
	tr := New()
	tr.Begin("account_discovery", "https://secret.example/?password=fixture-secret", 42, 0).Finish("timeout")
	if !strings.Contains(b.String(), `"level":"WARN"`) || !strings.Contains(b.String(), `"msg":"task timed out"`) || !strings.Contains(b.String(), `"account_id":42`) || strings.Contains(b.String(), "fixture-secret") || strings.Contains(b.String(), "secret.example") {
		t.Fatal("missing or unsafe timeout warning", b.String())
	}
	if tr.Snapshot().Recent[0].Outcome != "timeout" {
		t.Fatal("status snapshot lost timeout")
	}
}
