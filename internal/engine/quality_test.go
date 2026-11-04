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

type qualityCatalogue struct {
	fixtureCatalogue
	enabled atomic.Bool
}

func (c *qualityCatalogue) UHDAllowed() bool            { return c.enabled.Load() }
func (c *qualityCatalogue) PreferDiversity(string) bool { return true }
func TestUHDRaceAndLastResortFallback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "UHD_before_HD", true: "HD_last_resort"}[fail], func(t *testing.T) {
			cat := &qualityCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Provider: "https://a.example", Limit: 1, URL: "uhd", UHD: true}, {StreamID: 2, AccountID: 2, Provider: "https://b.example", Limit: 1, URL: "hd"}}}}
			cat.enabled.Store(true)
			started := make(chan string, 20)
			ctx, cancel := context.WithCancel(context.Background())
			e := &Engine{Catalogue: cat, Budget: budget.New(10, 5), RaceSize: 5, Copy: func(ctx context.Context, u string, _ func([]byte)) error {
				started <- u
				if fail && u == "uhd" {
					return errors.New("fixture failure")
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			done := make(chan error, 1)
			go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
			defer func() { cancel(); <-done }()
			select {
			case u := <-started:
				if u != "uhd" {
					t.Fatal("did not prefer UHD")
				}
			case <-time.After(time.Second):
				t.Fatal("no startup")
			}
			if fail {
				select {
				case u := <-started:
					if u != "hd" {
						t.Fatal(u)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("did not fall back")
				}
			} else {
				select {
				case u := <-started:
					t.Fatal("cross-class race launched prematurely", u)
				case <-time.After(200 * time.Millisecond):
				}
			}
		})
	}
}
func TestDisableUHDKeepsOutputAndMovesToHD(t *testing.T) {
	cat := &qualityCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1, URL: "uhd", UHD: true}, {StreamID: 2, AccountID: 2, Limit: 1, URL: "hd"}}}}
	cat.enabled.Store(true)
	var active, closed atomic.Int32
	copy := progressingFixture(&active, &closed)
	e := &Engine{Catalogue: cat, Budget: budget.New(10, 5), Copy: copy, Inspect: func(context.Context, []byte) (media.Info, error) {
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}, NewOutput: func(_ context.Context, emit func([]byte), _ int) (Output, error) { return &testOutput{emit: emit}, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
	defer func() { cancel(); <-done }()
	end := time.Now().Add(2 * time.Second)
	for time.Now().Before(end) {
		st := e.Status()
		if len(st) > 0 && st[0].Primary == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cat.enabled.Store(false)
	for ctx.Err() == nil {
		st := e.Status()
		if len(st) > 0 && st[0].Primary == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("did not replace disabled UHD primary")
}

type growingQualityCatalogue struct {
	qualityCatalogue
	available atomic.Int32
}

func (c *growingQualityCatalogue) Candidates(string, bool) ([]catalog.Candidate, error) {
	return c.candidates[:int(c.available.Load())], nil
}
func TestMatchingQualityGhostReplacesLastResortGhost(t *testing.T) {
	cat := &growingQualityCatalogue{qualityCatalogue: qualityCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1, URL: "uhd1", UHD: true}, {StreamID: 2, AccountID: 2, Limit: 1, URL: "hd"}, {StreamID: 3, AccountID: 3, Limit: 1, URL: "uhd2", UHD: true}}}}}
	cat.enabled.Store(true)
	cat.available.Store(2)
	var active, closed atomic.Int32
	e := &Engine{Catalogue: cat, Budget: budget.New(10, 5), Copy: progressingFixture(&active, &closed), Inspect: func(context.Context, []byte) (media.Info, error) {
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}, NewOutput: func(_ context.Context, emit func([]byte), _ int) (Output, error) { return &testOutput{emit: emit}, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
	defer func() { cancel(); <-done }()
	ready := false
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		st := e.Status()
		if len(st) > 0 && st[0].Primary == 1 && st[0].Ghost == 2 {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("last-resort hot standby was not established")
	}
	cat.available.Store(3)
	for ctx.Err() == nil {
		st := e.Status()
		if len(st) > 0 && st[0].Primary == 1 && st[0].Ghost == 3 && st[0].Candidates == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("matching-quality standby did not replace fallback")
}
