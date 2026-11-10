package media

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestProbeDistinguishesDeadlineFromCancellation(t *testing.T) {
	g, e := NewGateway(&http.Client{})
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	expired, done := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer done()
	r := g.Probe(expired, "/nonexistent/ffmpeg", "https://fixture.example/1.ts", time.Second)
	if !r.TimedOut || r.Success || r.Reason != "media check timed out" {
		t.Fatal(r)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r = g.Probe(cancelled, "/nonexistent/ffmpeg", "https://fixture.example/1.ts", time.Second)
	if r.TimedOut {
		t.Fatal("playback cancellation reported as timeout", r)
	}
	_, e = Inspect(expired, "/nonexistent/ffmpeg", nil)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}

func TestProbeFailureDiagnosticsAreSafe(t *testing.T) {
	for _, tc := range []struct {
		err     error
		invalid bool
		want    string
	}{
		{nil, false, "source ended before sample completed"},
		{errors.New("partial transport packet"), false, "partial transport packet"},
		{errors.New("https://example.test/private-password"), false, "source adapter failed"},
		{context.Canceled, true, "invalid transport packet"},
	} {
		if got := probeEndReason(tc.err, tc.invalid); got != tc.want {
			t.Fatal(got)
		}
	}
	g, e := NewGateway(&http.Client{})
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	r := g.Probe(context.Background(), "/nonexistent/ffmpeg", "https://fixture.example/1.ts", time.Millisecond)
	if r.Success || r.Reason != "media process failed to start" {
		t.Fatal(r)
	}
}
