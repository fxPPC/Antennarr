package engine

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/ingest"
	"antennarr/internal/media"
	"antennarr/internal/roster"
	"antennarr/internal/stream"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestThreeChannelsDuringCatalogueImport(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("requires generated fixtures")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	channels := []string{}
	for c := 0; c < 3; c++ {
		id, e := store.SaveChannel(catalog.Channel{Number: strconv.Itoa(c + 1), Name: fmt.Sprint("Test ", c), Enabled: true})
		if e != nil {
			t.Fatal(e)
		}
		channels = append(channels, id)
		for a := 0; a < 5; a++ {
			raw := fmt.Sprintf("http://fixture-%d-%d.example/live/u/p/42.ts", c, a)
			entry, _ := ingest.FromURL(raw, fmt.Sprintf("Test %d", c), fmt.Sprintf("test-%d", c))
			if _, e = store.Import([]ingest.Entry{entry}, "Authorised synthetic test"); e != nil {
				t.Fatal(e)
			}
		}
		rows, _ := store.Candidates(id, false)
		if len(rows) != 5 {
			t.Fatal(rows)
		}
		if e = store.Map(id, rows[0].StreamID, true); e != nil {
			t.Fatal(e)
		}
	}
	for _, ch := range channels {
		cs, e := store.Candidates(ch, false)
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range cs {
			if e = store.Record(c.StreamID, catalog.Result{Success: true}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = store.EnableRosters(catalog.RosterPolicy{Target: 30, Preference: "stability", Race: 5}); e != nil {
		t.Fatal(e)
	}
	selected := &roster.Service{Store: store}
	limits := budget.New(10, 5)
	eng := &Engine{Catalogue: selected, Budget: limits, Binary: binary, Copy: func(ctx context.Context, _ string, emit func([]byte)) error {
		return stream.FixtureRunner(binary, dir)(ctx, "720", emit)
	}, Inspect: func(ctx context.Context, b []byte) (media.Info, error) { return media.Inspect(ctx, binary, b) }}
	seconds := 30
	if raw := os.Getenv("ANTENNARR_TEST_SOAK_SECONDS"); raw != "" {
		if n, e := strconv.Atoi(raw); e == nil && n >= 10 && n <= 600 {
			seconds = n
		}
	}
	// Race instrumentation can make a large SQLite import much slower on small
	// CI runners. Bound the whole exercise independently of its continuity
	// assertion: playback must still keep every output gap below two seconds.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+600)*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	last := map[string]time.Time{}
	maxGap := time.Duration(0)
	bytes := int64(0)
	for _, id := range channels {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			eng.Run(ctx, id, func(b []byte) {
				mu.Lock()
				defer mu.Unlock()
				now := time.Now()
				if !last[id].IsZero() && now.Sub(last[id]) > maxGap {
					maxGap = now.Sub(last[id])
				}
				last[id] = now
				bytes += int64(len(b))
			})
		}(id)
	}
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		pairs := 0
		for _, st := range eng.Status() {
			if st.Primary != 0 && st.Ghost != 0 {
				pairs++
			}
		}
		if pairs == 3 {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		cancel()
		wg.Wait()
		t.Fatal("pairs not ready")
	}
	imports := make([]ingest.Entry, 20000)
	for i := range imports {
		u := url.URL{Scheme: "https", Host: "unrelated-fixture.example", Path: "/stream/" + strconv.Itoa(i) + ".ts"}
		imports[i], _ = ingest.FromURL(u.String(), "Unselected fixture", "")
	}
	began := time.Now()
	if _, e = store.Import(imports, "Authorised catalogue load fixture"); e != nil {
		cancel()
		wg.Wait()
		t.Fatal(e)
	}
	importDuration := time.Since(began)
	select {
	case <-time.After(time.Duration(seconds) * time.Second):
	case <-ctx.Done():
		t.Error("soak deadline reached")
	}
	statuses := eng.Status()
	cancel()
	wg.Wait()
	if limits.Snapshot().Total != 0 {
		t.Fatal("reservation leak")
	}
	if len(statuses) != 3 {
		t.Fatal("channel controller lost")
	}
	for _, st := range statuses {
		if st.Primary == 0 || st.Ghost == 0 || st.DroppedMetrics != 0 {
			t.Fatal(st)
		}
	}
	var healthy int
	if e := store.DB.QueryRow("SELECT count(*) FROM streams WHERE state='healthy'").Scan(&healthy); e != nil || healthy < 6 {
		t.Fatalf("playback health history lost: %d, %v", healthy, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if maxGap > 2*time.Second {
		t.Errorf("output gap during catalogue import: %s", maxGap)
	}
	t.Logf("three pairs, %d s soak, 20,000 unrelated records imported in %s, maximum output gap %s, bytes %d", seconds, importDuration, maxGap, bytes)
}
