package engine

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/ingest"
	"antennarr/internal/media"
	"antennarr/internal/roster"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRosterRacesNewestConfiguredBatch(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ch, _ := s.SaveChannel(catalog.Channel{Number: "1", Name: "MTV", Region: "US", Enabled: true})
	for i := 0; i < 7; i++ {
		v, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/1.ts", i), "MTV", "")
		s.Import([]ingest.Entry{v}, "fixture")
	}
	cs, _ := s.Candidates(ch, false)
	for _, c := range cs {
		s.Record(c.StreamID, catalog.Result{Success: true})
	}
	if e = s.EnableRosters(catalog.RosterPolicy{Target: 30, Preference: "diversity", Race: 3}); e != nil {
		t.Fatal(e)
	}
	for i, c := range cs {
		s.DB.Exec("UPDATE roster SET verified=? WHERE stream_id=?", 100+i, c.StreamID)
	}
	rows, _ := s.RosterMembers(ch)
	expected := map[string]bool{}
	for _, c := range rows[:3] {
		expected[c.URL] = true
	}
	started := make(chan string, 20)
	ctx, cancel := context.WithCancel(context.Background())
	eng := &Engine{Catalogue: &roster.Service{Store: s}, Budget: budget.New(10, 5), Copy: func(ctx context.Context, url string, _ func([]byte)) error {
		started <- url
		<-ctx.Done()
		return ctx.Err()
	}}
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx, ch, func([]byte) {}) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 3; i++ {
		select {
		case url := <-started:
			if !expected[url] {
				t.Fatal("raced stale source", url)
			}
			delete(expected, url)
		case <-time.After(time.Second):
			t.Fatal("startup batch missing")
		}
	}
	select {
	case <-started:
		t.Fatal("exceeded configured batch")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStartupAndUnusedRacerCancellationRetainEntireRoster(t *testing.T) {
	store, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ch, _ := store.SaveChannel(catalog.Channel{Number: "1", Name: "MTV", Region: "US", Enabled: true})
	for i := 0; i < 12; i++ {
		v, _ := ingest.FromURL(fmt.Sprintf("https://r%d.example/1.ts", i), "MTV", "")
		if _, err = store.Import([]ingest.Entry{v}, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	store.EnableRosters(catalog.RosterPolicy{Target: 30, Preference: "stability", Race: 5})
	rows, _ := store.Candidates(ch, false)
	for _, v := range rows {
		store.Record(v.StreamID, catalog.Result{Success: true})
	}
	var opened, closed atomic.Int32
	e := &Engine{Catalogue: &roster.Service{Store: store}, Budget: budget.New(15, 5), Copy: progressingFixture(&opened, &closed), Inspect: func(context.Context, []byte) (media.Info, error) {
		return media.Info{Codec: "h264", AudioCodec: "aac"}, nil
	}, NewOutput: func(_ context.Context, emit func([]byte), _ int) (Output, error) {
		return &testOutput{emit: emit}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx, ch, func([]byte) {}) }()
	deadline := time.Now().Add(4 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		v := e.Status()
		if len(v) > 0 && v[0].Primary != 0 && v[0].Ghost != 0 && closed.Load() >= 3 {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	flush, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = e.FlushMetrics(flush); err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("startup did not establish a pair and release unused candidates")
	}
	var n, failed int
	store.DB.QueryRow("SELECT count(*) FROM roster").Scan(&n)
	store.DB.QueryRow("SELECT count(*) FROM validations WHERE success=0").Scan(&failed)
	if n != 12 || failed != 0 {
		t.Fatal("healthy/untried racers evicted from roster", n, failed)
	}
}
