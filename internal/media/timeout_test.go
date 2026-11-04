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
