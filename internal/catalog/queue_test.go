package catalog

import (
	"antennarr/internal/ingest"
	"testing"
)

func TestClearQueueKeepsRostersAndInUseAccounts(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	s.Record(cs[0].StreamID, Result{Success: true})
	oldMax := cs[2].StreamID
	out, e := s.ClearQueue([]int64{cs[1].AccountID})
	if e != nil {
		t.Fatal(e)
	}
	if out.Streams != 1 || out.Accounts != 1 {
		t.Fatal(out)
	}
	members, _ := s.RosterMembers(ch)
	if len(members) != 1 || members[0].StreamID != cs[0].StreamID {
		t.Fatal("roster deleted")
	}
	out, e = s.ClearQueue(nil)
	if e != nil || out.Streams != 1 || out.Accounts != 1 {
		t.Fatal(out, e)
	}
	r, _ := s.RosterSummary()
	if r.QueuedStreams != 0 || r.QueuedAccounts != 0 {
		t.Fatal("queue remains", r)
	}
	entry, _ := ingest.FromURL("https://new.example/1.ts", "MTV", "")
	s.Import([]ingest.Entry{entry}, "fixture")
	pool, _ := s.QueueCandidates(ch, 10)
	if len(pool) != 1 || pool[0].StreamID <= oldMax {
		t.Fatal("deleted source ID reused", pool)
	}
	var broken int
	rows, _ := s.DB.Query("PRAGMA foreign_key_check")
	for rows.Next() {
		broken++
	}
	rows.Close()
	if broken != 0 {
		t.Fatal("foreign keys broken")
	}
}
func TestClearQueueDoesNotRestoreDeletedCachedEntries(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	s.SetUHDAllowed(true)
	if _, e := s.ClearQueue(nil); e != nil {
		t.Fatal(e)
	}
	if e := s.SetUHDAllowed(false); e != nil {
		t.Fatal(e)
	}
	members, _ := s.RosterMembers(ch)
	if len(members) != 0 {
		t.Fatal("deleted input queue returned from cache")
	}
}
