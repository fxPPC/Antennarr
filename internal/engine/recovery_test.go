package engine

import (
	"antennarr/internal/activity"
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/media"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testOutput struct {
	closed atomic.Bool
	silent atomic.Bool
	last   atomic.Int64
	emit   func([]byte)
}

func (o *testOutput) Send(b []byte) bool {
	if o.closed.Load() {
		return false
	}
	if !o.silent.Load() {
		o.last.Store(time.Now().UnixNano())
		o.emit(b)
	}
	return true
}
func (o *testOutput) Close()        { o.closed.Store(true) }
func (o *testOutput) Stopped() bool { return o.closed.Load() }
func (o *testOutput) LastOutput() time.Time {
	v := o.last.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

type recoveryCatalogue struct {
	fixtureCatalogue
	failures atomic.Int32
}

func (c *recoveryCatalogue) Record(_ int64, r catalog.Result) error {
	if !r.Success {
		c.failures.Add(1)
	}
	return nil
}
func progressingFixture(active, closed *atomic.Int32) CopyFunc {
	return func(ctx context.Context, _ string, emit func([]byte)) error {
		active.Add(1)
		defer closed.Add(1)
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
	}
}
func TestOutputRecoveryKeepsHealthyPrimaryAndGhost(t *testing.T) {
	for _, mode := range []string{"stopped", "silent", "start_failure"} {
		t.Run(mode, func(t *testing.T) {
			cat := &recoveryCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1, URL: "one"}, {StreamID: 2, AccountID: 2, Limit: 1, URL: "two"}}}}
			var active, closed, attempts atomic.Int32
			var delivered atomic.Int64
			var mu sync.Mutex
			var first *testOutput
			e := &Engine{Catalogue: cat, Activity: activity.New(), RaceSize: 2, Budget: budget.New(10, 10), Copy: progressingFixture(&active, &closed), Inspect: func(context.Context, []byte) (media.Info, error) {
				return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
			}}
			e.NewOutput = func(_ context.Context, emit func([]byte), _ int) (Output, error) {
				n := attempts.Add(1)
				if mode == "start_failure" && n == 1 {
					return (*testOutput)(nil), errors.New("temporary process shortage")
				}
				out := &testOutput{emit: emit}
				mu.Lock()
				if first == nil {
					first = out
				}
				mu.Unlock()
				return out, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- e.Run(ctx, "test", func(b []byte) { delivered.Add(int64(len(b))) }) }()
			for ctx.Err() == nil {
				state := e.Status()
				if len(state) > 0 && state[0].Primary != 0 && state[0].Ghost != 0 && delivered.Load() > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if ctx.Err() != nil {
				t.Fatal("pair never became ready")
			}
			before := delivered.Load()
			if mode != "start_failure" {
				mu.Lock()
				out := first
				mu.Unlock()
				if mode == "stopped" {
					out.Close()
				} else {
					out.silent.Store(true)
					out.last.Store(time.Now().Add(-10 * time.Second).UnixNano())
				}
			}
			for ctx.Err() == nil {
				state := e.Status()
				if attempts.Load() >= 2 && delivered.Load() > before && len(state) > 0 && state[0].OutputRestarts >= 1 && state[0].Recovery == "" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if ctx.Err() != nil {
				t.Fatal("output did not recover")
			}
			if active.Load() != 2 || closed.Load() != 0 || cat.failures.Load() != 0 {
				t.Fatalf("healthy feeds disrupted: starts=%d closes=%d failures=%d", active.Load(), closed.Load(), cat.failures.Load())
			}
			if e.Activity.Snapshot().Completed["output_recovery"] == 0 {
				t.Fatal("recovery not reported in Activity")
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

type providerLimitedCatalogue struct{ fixtureCatalogue }

func (c *providerLimitedCatalogue) Candidates(_ string, healthy bool) ([]catalog.Candidate, error) {
	if healthy {
		return c.candidates[:3], nil
	}
	return c.candidates, nil
}
func TestRecruitmentExpandsPastProviderBudget(t *testing.T) {
	cat := &providerLimitedCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{
		{StreamID: 1, AccountID: 1, Provider: "busy", Limit: 1, URL: "one"},
		{StreamID: 2, AccountID: 2, Provider: "busy", Limit: 1, URL: "two"},
		{StreamID: 3, AccountID: 3, Provider: "busy", Limit: 1, URL: "three"},
		{StreamID: 4, AccountID: 4, Provider: "other", Limit: 1, URL: "fallback"},
	}}}
	var active, closed atomic.Int32
	e := &Engine{Catalogue: cat, RaceSize: 2, Budget: budget.New(10, 1), Copy: progressingFixture(&active, &closed), Inspect: func(context.Context, []byte) (media.Info, error) {
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}, NewOutput: func(_ context.Context, emit func([]byte), _ int) (Output, error) { return &testOutput{emit: emit}, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
	for ctx.Err() == nil {
		states := e.Status()
		if len(states) > 0 && states[0].Primary != 0 && states[0].Ghost != 0 {
			a, b := states[0].Primary, states[0].Ghost
			if !((a == 1 && b == 4) || (a == 4 && b == 1)) {
				t.Fatalf("wrong account pair %d %d", a, b)
			}
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("provider budget prevented fallback recruitment")
}

type delayedHistoryCatalogue struct {
	fixtureCatalogue
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *delayedHistoryCatalogue) Record(int64, catalog.Result) error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return nil
}
func TestChannelCleanupDoesNotWaitForDatabaseHistory(t *testing.T) {
	cat := &delayedHistoryCatalogue{fixtureCatalogue: fixtureCatalogue{candidates: []catalog.Candidate{{StreamID: 1, AccountID: 1, Limit: 1, URL: "fixture"}}}, entered: make(chan struct{}), release: make(chan struct{})}
	var active, closed atomic.Int32
	e := &Engine{Catalogue: cat, Budget: budget.New(10, 5), Copy: progressingFixture(&active, &closed), Inspect: func(context.Context, []byte) (media.Info, error) {
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}, NewOutput: func(_ context.Context, emit func([]byte), _ int) (Output, error) { return &testOutput{emit: emit}, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, "test", func([]byte) {}) }()
	defer func() { cancel(); close(cat.release); e.FlushMetrics(context.Background()) }()
	select {
	case <-cat.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("history was not written")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("channel cleanup blocked on history")
	}
	if active.Load() != closed.Load() {
		t.Fatal("source cleanup leaked")
	}
}
