package engine

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/media"
	"antennarr/internal/stream"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureCatalogue struct {
	mu         sync.Mutex
	events     int
	results    int
	candidates []catalog.Candidate
}

func (c *fixtureCatalogue) Candidates(_ string, _ bool) ([]catalog.Candidate, error) {
	return c.candidates, nil
}
func (c *fixtureCatalogue) Record(_ int64, _ catalog.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results++
	return nil
}
func (c *fixtureCatalogue) Event(_, _ string, _ int64, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events++
}

type fixtureTransport struct {
	target    *url.URL
	transport http.RoundTripper
}

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = f.target.Scheme
	u.Host = f.target.Host
	clone.URL = &u
	return f.transport.RoundTrip(clone)
}
func TestLiveGhostPromotionAndReplenishment(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("requires generated fixtures and ffmpeg")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	var active, peak atomic.Int32
	var faultAt atomic.Int64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if old >= n || peak.CompareAndSwap(old, n) {
				break
			}
		}
		id := r.URL.Path
		delay := time.Duration(0)
		if id == "/2" {
			delay = 250 * time.Millisecond
		} else if id != "/1" {
			delay = 2 * time.Second
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		faultMode := os.Getenv("ANTENNARR_TEST_FAULT")
		if (id == "/1" && faultMode != "ghost") || (id == "/2" && faultMode == "ghost") {
			delay := 7 * time.Second
			if faultMode == "ghost" {
				delay = 5 * time.Second
			}
			timer := time.AfterFunc(delay, func() {
				faultAt.Store(time.Now().UnixNano())
				if os.Getenv("ANTENNARR_TEST_FAULT") != "stall" {
					cancel()
				}
			})
			defer timer.Stop()
		}
		fixture := "720"
		if id == "/2" {
			fixture = "480"
			if os.Getenv("ANTENNARR_TEST_GHOST_CODEC") == "mpeg2" {
				fixture = "mpeg2"
			}
		}
		w.Header().Set("Content-Type", "video/mp2t")
		_ = stream.FixtureRunner(binary, dir)(ctx, fixture, func(b []byte) {
			if id == "/1" && os.Getenv("ANTENNARR_TEST_FAULT") == "stall" && faultAt.Load() > 0 {
				return
			}
			if _, e := w.Write(b); e != nil {
				cancel()
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		})
	}))
	defer source.Close()
	target, _ := url.Parse(source.URL)
	g, e := media.NewGateway(&http.Client{Transport: fixtureTransport{target, http.DefaultTransport}})
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	cat := &fixtureCatalogue{}
	for i := 1; i <= 5; i++ {
		cat.candidates = append(cat.candidates, catalog.Candidate{StreamID: int64(i), AccountID: int64(i), Provider: fmt.Sprint(i), Limit: 1, URL: fmt.Sprintf("http://authorised-fixture.example/%d", i)})
	}
	limits := budget.New(10, 5)
	eng := &Engine{Catalogue: cat, Budget: limits, Binary: binary, Copy: func(ctx context.Context, u string, emit func([]byte)) error { return g.Copy(ctx, binary, u, emit) }, Inspect: func(ctx context.Context, b []byte) (media.Info, error) { return media.Inspect(ctx, binary, b) }}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out := filepath.Join(t.TempDir(), "failover.ts")
	file, e := os.Create(out)
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	var outputMu sync.Mutex
	var packets atomic.Int64
	var lastOutput time.Time
	var maxGap time.Duration
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(ctx, "test", func(b []byte) {
			outputMu.Lock()
			now := time.Now()
			if !lastOutput.IsZero() && now.Sub(lastOutput) > maxGap {
				maxGap = now.Sub(lastOutput)
			}
			lastOutput = now
			file.Write(b)
			outputMu.Unlock()
			packets.Add(int64(len(b) / 188))
		})
	}()
	deadline := time.Now().Add(22 * time.Second)
	passed := false
	var initialPrimary int64
	ghostWasReady := false
	var switchedAt time.Time
	for time.Now().Before(deadline) {
		statuses := eng.Status()
		if len(statuses) > 0 {
			st := statuses[0]
			if initialPrimary == 0 && st.Primary != 0 {
				initialPrimary = st.Primary
			}
			if st.Ghost == 2 {
				ghostWasReady = true
			}
			valid := st.Failovers > 0 && st.Primary == 2 && st.Ghost != 0 && st.Ghost != 1
			if os.Getenv("ANTENNARR_TEST_FAULT") == "ghost" {
				valid = ghostWasReady && st.Primary == 1 && st.Ghost >= 3 && st.Failovers == 0
			}
			if valid {
				switchedAt = time.Now()
				passed = true
				break
			}
		}
		select {
		case err := <-done:
			t.Fatalf("engine exited early: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !passed {
		t.Logf("status before stopping failed test: %+v", eng.Status())
		cancel()
		<-done
		t.Fatalf("no successful ghost promotion/replenishment; status=%+v peak=%d", eng.Status(), peak.Load())
	}
	if initialPrimary != 1 {
		t.Errorf("first responsive source not selected: %d", initialPrimary)
	}
	if peak.Load() != 5 {
		t.Errorf("startup race peak=%d", peak.Load())
	}
	time.Sleep(3 * time.Second)
	cancel()
	<-done
	outputMu.Lock()
	file.Close()
	outputMu.Unlock()
	if limits.Snapshot().Total != 0 {
		t.Fatal("upstream reservations leaked")
	}
	if packets.Load() < 1000 {
		t.Fatal("no meaningful output")
	}
	// Decode well before the arbitrary final capture cut. Switching resolution is intentional.
	cmd := exec.Command(binary, "-v", "error", "-i", out, "-t", "10", "-f", "null", "-")
	errors, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("output decoder failed: %s", errors)
	}
	t.Logf("maximum output gap=%s; fault-to-ready-pair=%s", maxGap, switchedAt.Sub(time.Unix(0, faultAt.Load())))
	t.Logf("ghost promotion/replenishment observed at %s; packet count=%d; decoder diagnostics=%q", switchedAt.Format(time.RFC3339), packets.Load(), string(errors))
	if dest := os.Getenv("ANTENNARR_TEST_CAPTURE"); dest != "" {
		b, _ := os.ReadFile(out)
		if e = os.WriteFile(dest, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

type multiCatalogue struct{ fixtureCatalogue }

func (c *multiCatalogue) Candidates(channel string, _ bool) ([]catalog.Candidate, error) {
	base := int64(100)
	if channel == "two" {
		base = 200
	}
	if channel == "three" {
		base = 300
	}
	out := []catalog.Candidate{}
	for i := int64(1); i <= 5; i++ {
		out = append(out, catalog.Candidate{StreamID: base + i, AccountID: base + i, Provider: channel, Limit: 1, URL: "720"})
	}
	return out, nil
}
func TestThreeChannelsRespectGlobalRaceBudget(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("requires generated fixtures")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	limits := budget.New(10, 5)
	cat := &multiCatalogue{}
	eng := &Engine{Catalogue: cat, Budget: limits, Binary: binary, Copy: func(ctx context.Context, fixture string, emit func([]byte)) error {
		return stream.FixtureRunner(binary, dir)(ctx, fixture, emit)
	}, Inspect: func(ctx context.Context, b []byte) (media.Info, error) { return media.Inspect(ctx, binary, b) }}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var bytes atomic.Int64
	for _, name := range []string{"one", "two", "three"} {
		wg.Add(1)
		go func(name string) { defer wg.Done(); eng.Run(ctx, name, func(b []byte) { bytes.Add(int64(len(b))) }) }(name)
	}
	ready := false
	peak := 0
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		n := limits.Snapshot().Total
		if n > peak {
			peak = n
		}
		if n > 10 {
			t.Error("global connection budget exceeded")
		}
		status := eng.Status()
		count := 0
		for _, st := range status {
			if st.Primary != 0 && st.Ghost != 0 {
				count++
			}
		}
		if count == 3 && bytes.Load() > 10000 {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	if !ready {
		t.Fatal("three primary/ghost pairs not established")
	}
	if limits.Snapshot().Total != 0 {
		t.Fatal("connection leak")
	}
	t.Logf("three concurrent pairs established; peak connections=%d; output bytes=%d", peak, bytes.Load())
}
