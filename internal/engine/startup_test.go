package engine

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/media"
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupWaitsForDecodableKeyframe(t *testing.T) {
	var inspections atomic.Int32
	cat := &fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 2, URL: "fixture"}}}
	e := &Engine{Catalogue: cat, Budget: budget.New(10, 10), Copy: func(ctx context.Context, _ string, emit func([]byte)) error {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var stamp byte
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-tick.C:
				stamp++
				b := make([]byte, 376)
				for i, pid := range []byte{0, 1} {
					p := b[i*188 : (i+1)*188]
					p[0] = 0x47
					p[1] = 0x41
					p[2] = pid
					p[3] = 0x10
					p[6] = 1
					p[11] = 0x80
					p[12] = 5
					if pid == 1 {
						p[18], p[19] = 0xff, 0xf1
					}
					p[17] = stamp
				}
				emit(b)
			}
		}
	}, Inspect: func(context.Context, []byte) (media.Info, error) {
		if inspections.Add(1) < 2 {
			return media.Info{}, errors.New("waiting for keyframe")
		}
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
	for ctx.Err() == nil {
		if inspections.Load() >= 2 {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a temporary inspection failure discarded the source")
}

type expandingCatalogue struct{ fixtureCatalogue }

func (c *expandingCatalogue) Candidates(_ string, healthy bool) ([]catalog.Candidate, error) {
	if healthy {
		return c.candidates[:1], nil
	}
	return c.candidates, nil
}
func TestExhaustedHealthyPoolExpands(t *testing.T) {
	cat := &expandingCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1, URL: "bad"}, {StreamID: 2, AccountID: 2, Limit: 1, URL: "new"}}}}
	launched := make(chan struct{}, 1)
	e := &Engine{Catalogue: cat, Budget: budget.New(10, 10), RaceSize: 1, Copy: func(ctx context.Context, u string, _ func([]byte)) error {
		if u == "bad" {
			return errors.New("unavailable")
		}
		select {
		case launched <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
	select {
	case <-launched:
		cancel()
		<-done
	case <-ctx.Done():
		t.Fatal("unverified fallback was never attempted")
	}
}

func TestFailedPoolCyclesWithoutPerSourceCooldown(t *testing.T) {
	cat := &fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1, URL: "retry"}}}
	attempts := 0
	again := make(chan struct{}, 1)
	e := &Engine{Catalogue: cat, Budget: budget.New(10, 10), RaceSize: 1, Copy: func(ctx context.Context, _ string, _ func([]byte)) error {
		attempts++
		if attempts == 1 {
			return errors.New("fixture temporary failure")
		}
		again <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "retry", func([]byte) {}) }()
	select {
	case <-again:
		cancel()
		<-done
	case <-ctx.Done():
		t.Fatal("source still had a timed cooldown")
	}
}

type startupCatalogue struct {
	fixtureCatalogue
	history chan string
}

func (c *startupCatalogue) Record(_ int64, r catalog.Result) error {
	if r.Success {
		c.history <- "decoded"
	}
	return nil
}

func (c *startupCatalogue) Event(kind, _ string, _ int64, _ string) {
	if kind == "source_pool" {
		c.history <- kind
	}
}

func TestStartupInspectsFreshMediaAfterFullUndecodableWindow(t *testing.T) {
	cat := &startupCatalogue{
		fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1}}},
		history:          make(chan string, 2),
	}
	var active, closed atomic.Int32
	var fullWindowRejected atomic.Bool
	const keyframe = "late keyframe"
	source := progressingFixture(&active, &closed)
	e := &Engine{Catalogue: cat, Budget: budget.New(1, 1), Copy: func(ctx context.Context, url string, emit func([]byte)) error {
		return source(ctx, url, func(b []byte) {
			if fullWindowRejected.Load() {
				copy(b[32:], keyframe)
				emit(b)
				return
			}
			emit(bytes.Repeat(b, 1024))
		})
	}, Inspect: func(_ context.Context, sample []byte) (media.Info, error) {
		if len(sample) >= 4<<20 {
			fullWindowRejected.Store(true)
		}
		if !bytes.Contains(sample, []byte(keyframe)) {
			return media.Info{}, errors.New("waiting for keyframe")
		}
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}, NewOutput: func(_ context.Context, emit func([]byte), _ int) (Output, error) {
		return &testOutput{emit: emit}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "late-keyframe", func([]byte) {}) }()
	defer func() {
		cancel()
		<-done
		if err := e.FlushMetrics(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	// The controller queues history in order. With just one source it must
	// report successful startup before reporting the missing standby.
	for _, want := range []string{"decoded", "source_pool"} {
		select {
		case got := <-cat.history:
			if got != want {
				t.Fatalf("got %q while waiting for %q", got, want)
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}
