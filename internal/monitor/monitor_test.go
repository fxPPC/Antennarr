package monitor

import (
	"antennarr/internal/activity"
	"antennarr/internal/catalog"
	"antennarr/internal/engine"
	"antennarr/internal/roster"
	"antennarr/internal/stream"
	"context"
	"testing"
	"time"
)

func TestCollectorCachesDatabaseButRefreshesLiveState(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	viewers := 0
	c := Collector{Store: s, Playback: func() stream.Status { return stream.Status{Viewers: viewers} }, Live: func() []engine.Status { return nil }, Work: func() activity.Snapshot { return activity.Snapshot{} }, Checker: func() roster.CheckerState { return roster.CheckerState{} }}
	a, e := waitSnapshot(t, &c)
	if e != nil {
		t.Fatal(e)
	}
	s.SaveChannel(catalog.Channel{Name: "CNN", Number: "1", Enabled: true})
	viewers = 1
	b, e := waitSnapshot(t, &c)
	if e != nil {
		t.Fatal(e)
	}
	if a.Database.Coverage.Total != b.Database.Coverage.Total || b.Playback.Viewers != 1 {
		t.Fatal("cache froze live state or repeated database scan")
	}
}

func TestSummaryFailureKeepsLiveStatusAndWarning(t *testing.T) {
	store, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := Collector{Store: store, Playback: func() stream.Status { return stream.Status{Viewers: 2} }, Live: func() []engine.Status { return nil }, Work: func() activity.Snapshot { return activity.Snapshot{} }, Checker: func() roster.CheckerState { return roster.CheckerState{} }}
	store.Close()
	v, err := waitSnapshot(t, &c)
	if err != nil || v.DatabaseError == "" || v.Playback.Viewers != 2 || v.Database.Channels == nil {
		t.Fatal("failed summary blanked live status", v, err)
	}
}

func TestSummaryFailureRetainsPreviousCounts(t *testing.T) {
	store, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.SaveChannel(catalog.Channel{Name: "CNN", Number: "1", Enabled: true})
	c := Collector{Store: store, Playback: func() stream.Status { return stream.Status{} }, Live: func() []engine.Status { return nil }, Work: func() activity.Snapshot { return activity.Snapshot{} }, Checker: func() roster.CheckerState { return roster.CheckerState{} }}
	if _, err = waitSnapshot(t, &c); err != nil {
		t.Fatal(err)
	}
	store.Close()
	c.at = time.Time{}
	v, err := waitSnapshot(t, &c)
	if err != nil || v.DatabaseError == "" || v.Database.Coverage.Total != 1 {
		t.Fatal("lost cached counts", v, err)
	}
}

// Wait only in tests that assert database contents; HTTP callers never wait.
func waitSnapshot(t *testing.T, c *Collector) (Snapshot, error) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		_, e := c.Snapshot(context.Background())
		if e != nil {
			return Snapshot{}, e
		}
		c.mu.Lock()
		done := !c.refreshing
		c.mu.Unlock()
		if done {
			// Refresh can finish after Snapshot copied its previous cache but
			// before this completion check. Read the now-completed cache again.
			return c.Snapshot(context.Background())
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("summary refresh did not finish")
	return Snapshot{}, nil
}

func TestSlowDatabaseDoesNotBlockLiveStatus(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.DB.SetMaxOpenConns(1)
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	c := Collector{Store: s, Playback: func() stream.Status { return stream.Status{Viewers: 2} }, Live: func() []engine.Status { return nil }, Work: func() activity.Snapshot { return activity.Snapshot{} }, Checker: func() roster.CheckerState { return roster.CheckerState{} }}
	start := time.Now()
	v, e := c.Snapshot(context.Background())
	if e != nil || v.Playback.Viewers != 2 || time.Since(start) > 100*time.Millisecond {
		t.Fatal("live status blocked", e)
	}
	tx.Rollback()
	waitSnapshot(t, &c)
}
