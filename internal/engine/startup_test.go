package engine

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/media"
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
