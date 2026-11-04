package catalog

import (
	"antennarr/internal/ingest"
	"testing"
	"time"
)

func TestQualityCacheRestoresAndReturnsStreamsToQueue(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	now := time.Now().Unix()
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	before, _ := s.RosterEntries(ch)
	if e := s.setUHDAllowedAt(true, now); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.RosterMembers(ch)
	if len(rows) != 0 {
		t.Fatal("new mode did not clear membership")
	}
	queue, e := s.QueueCandidates(ch, 20)
	if e != nil || len(queue) != 3 {
		t.Fatal("departing streams not returned to input queue", len(queue), e)
	}
	if e = s.setUHDAllowedAt(false, now+3599); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.RosterMembers(ch)
	if len(rows) != 3 {
		t.Fatal("cache not restored", len(rows))
	}
	after, _ := s.RosterEntries(ch)
	for i := range before {
		if before[i]["verified"] != after[i]["verified"] || before[i]["due"] != after[i]["due"] {
			t.Fatal("cache invented new verification/deadlines")
		}
	}
	queue, _ = s.QueueCandidates(ch, 20)
	if len(queue) != 0 {
		t.Fatal("assigned streams still counted as queued")
	}
}
func TestQualityCacheExpiresAtOneHour(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	now := time.Now().Unix()
	s.setUHDAllowedAt(true, now)
	if e := s.setUHDAllowedAt(false, now+3600); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.RosterMembers(ch)
	if len(rows) != 0 {
		t.Fatal("expired cache restored")
	}
	queue, _ := s.QueueCandidates(ch, 20)
	if len(queue) != 3 {
		t.Fatal("expired entries not queued")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM settings WHERE name=?", qualityCacheKey(false)).Scan(&n)
	if n != 0 {
		t.Fatal("expired cache retained")
	}
}
func TestQualityCacheFiltersFailuresMappingsAndCurrentTarget(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	now := time.Now().Unix()
	s.setUHDAllowedAt(true, now)
	// A playback failure with dropped metrics still invalidates a cached entry.
	s.RosterObserved(cs[0].StreamID, false)
	s.Map(ch, cs[1].StreamID, false)
	if e := s.setUHDAllowedAt(false, now+1); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.RosterMembers(ch)
	if len(rows) != 1 || rows[0].StreamID != cs[2].StreamID {
		t.Fatal("restored failed/excluded entries", rows)
	}
	s.setUHDAllowedAt(true, now+2)
	s.SaveChannelPolicy(ch, RosterPolicy{Target: 2})
	// The on-mode snapshot is empty; cached restoration must not invent members.
	rows, _ = s.RosterMembers(ch)
	if len(rows) != 0 {
		t.Fatal("empty cached roster changed")
	}
}
func TestQualityCacheSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	ch, _ := s.SaveChannel(Channel{Number: "1", Name: "MTV", Region: "US", Enabled: true})
	// Reuse a fixture's ordinary import format without writing secrets into cache.
	entry, e := ingest.FromURL("https://fixture.example/1.ts", "MTV", "")
	if e != nil {
		t.Fatal(e)
	}
	s.Import([]ingest.Entry{entry}, "fixture")
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	cs, _ := s.Candidates(ch, false)
	s.Record(cs[0].StreamID, Result{Success: true})
	now := time.Now().Unix()
	s.setUHDAllowedAt(true, now)
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	if e = s.setUHDAllowedAt(true, now+1); e != nil {
		t.Fatal(e)
	} // same choice at startup
	if e = s.setUHDAllowedAt(false, now+2); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.RosterMembers(ch)
	if len(rows) != 1 {
		t.Fatal("restart lost cached assignments")
	}
}

func TestQualityCacheRetainsSurplusAfterTargetReduction(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	now := time.Now().Unix()
	s.setUHDAllowedAt(true, now)
	s.SaveChannelPolicy(ch, RosterPolicy{Target: 2})
	if e := s.setUHDAllowedAt(false, now+1); e != nil {
		t.Fatal(e)
	}
	members, _ := s.RosterMembers(ch)
	if len(members) != 3 {
		t.Fatal("cache trimmed verified surplus", len(members))
	}
}

func TestQualityCacheAllowsFreshSnapshotAfterEarlierFailure(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	s.Record(cs[0].StreamID, Result{Success: false})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	s.DB.Exec("DELETE FROM work_leases WHERE kind='cooldown'")
	now := time.Now().Unix() + 10 // Snapshot taken after the failure and successful recovery.
	s.setUHDAllowedAt(true, now)
	if e := s.setUHDAllowedAt(false, now+1); e != nil {
		t.Fatal(e)
	}
	members, _ := s.RosterMembers(ch)
	if len(members) != 3 {
		t.Fatal("old failure invalidated a newer healthy snapshot", len(members))
	}
}
