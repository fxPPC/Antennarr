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
	a, e := c.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s.SaveChannel(catalog.Channel{Name: "CNN", Number: "1", Enabled: true})
	viewers = 1
	b, e := c.Snapshot(context.Background())
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
	v, err := c.Snapshot(context.Background())
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
	if _, err = c.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.Close()
	c.at = time.Time{}
	v, err := c.Snapshot(context.Background())
	if err != nil || v.DatabaseError == "" || v.Database.Coverage.Total != 1 {
		t.Fatal("lost cached counts", v, err)
	}
}
