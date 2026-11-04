package catalog

import (
	"os"
	"testing"
	"time"
)

func TestProductionRosterSnapshot(t *testing.T) {
	root := os.Getenv("ANTENNARR_MIGRATION_REVIEW")
	if root == "" {
		t.Skip("private migration copy not supplied")
	}
	start := time.Now()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.EnableRosters(RosterPolicy{30, "stability", 5}); e != nil {
		t.Fatal(e)
	}
	r, e := s.RosterSummary()
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Channels) == 0 {
		t.Fatal("snapshot contains no channels")
	}
	for _, ch := range r.Channels {
		t.Logf("%s: %d/%d, %d providers", ch.Name, ch.Count, ch.Policy.Target, ch.Providers)
		if ch.Count > 30 {
			t.Fatal("oversized roster")
		}
	}
	t.Logf("Migration %s; verified %d; queued accounts %d; queued streams %d", time.Since(start), r.Ready, r.QueuedAccounts, r.QueuedStreams)
	var bad int
	if e = s.DB.QueryRow("SELECT count(*) FROM roster WHERE due>verified+48*3600").Scan(&bad); e != nil || bad > 0 {
		t.Fatal("deadline violation", bad, e)
	}
	start = time.Now()
	for i := 0; i < 10; i++ {
		if _, e = s.RosterSummary(); e != nil {
			t.Fatal(e)
		}
	}
	t.Logf("Summary average %s", time.Since(start)/10)
}
