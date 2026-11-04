package catalog

import (
	"antennarr/internal/ingest"
	"fmt"
	"sync"
	"testing"
	"time"
)

func rosterFixture(t *testing.T, n int) (*Store, string, []Candidate) {
	t.Helper()
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	ch, e := s.SaveChannel(Channel{Number: "1", Name: "MTV", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < n; i++ {
		v, e := ingest.FromURL(fmt.Sprintf("https://p%d.example/%d.ts", i%3, i), "MTV", "")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	cs, e := s.Candidates(ch, false)
	if e != nil || len(cs) != n {
		t.Fatal(len(cs), e)
	}
	return s, ch, cs
}
func TestRosterAdmissionBoundedConcurrentAndNewest(t *testing.T) {
	s, ch, cs := rosterFixture(t, 12)
	if e := s.EnableRosters(RosterPolicy{3, "stability", 5}); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for _, c := range cs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if e := s.Record(id, Result{Success: true}); e != nil {
				t.Error(e)
			}
		}(c.StreamID)
	}
	wg.Wait()
	rows, e := s.RosterMembers(ch)
	if e != nil || len(rows) != 3 {
		t.Fatal(rows, e)
	}
	for i, c := range rows {
		s.DB.Exec("UPDATE roster SET verified=? WHERE stream_id=?", 100+i, c.StreamID)
	}
	rows, e = s.RosterMembers(ch)
	if e != nil || rows[0].LastSuccess != 102 {
		t.Fatal(rows, e)
	}
	victim := rows[0].StreamID
	if e = s.Record(victim, Result{Success: false}); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.RosterMembers(ch)
	if len(rows) != 2 {
		t.Fatal("failure did not evict", rows)
	}
	q, e := s.QueueCandidates(ch, 20)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, c := range q {
		if c.StreamID == victim {
			found = true
		}
	}
	if !found {
		t.Fatal("failed entry was not returned to the queue")
	}
	if len(q) == 0 {
		t.Fatal("replacement missing")
	}
	s.Record(q[0].StreamID, Result{Success: true})
	rows, _ = s.RosterMembers(ch)
	if len(rows) != 3 {
		t.Fatal("replacement failed")
	}
}
func TestSeededRosterSurvivesRestartWithoutTimedDeadlines(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	now := time.Now().Unix()
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
		s.DB.Exec("UPDATE streams SET last_success=? WHERE id=?", now-47*3600, c.StreamID)
	}
	p := RosterPolicy{2, "stability", 5}
	if e := s.EnableRosters(p); e != nil {
		t.Fatal(e)
	}
	var before, after int64
	s.DB.QueryRow("SELECT min(due) FROM roster").Scan(&before)
	if before != now-47*3600 {
		t.Fatal("legacy field should be verification timestamp", before)
	}
	s.EnableRosters(p)
	s.SaveChannelPolicy(ch, RosterPolicy{Target: 2})
	s.DB.QueryRow("SELECT min(due) FROM roster").Scan(&after)
	if after != before {
		t.Fatal("legacy hours still changed scheduling")
	}
	s.EnsureRosterPass(now)
	if _, _, e := s.NextRosterCheck(now); e != nil {
		t.Fatal(e)
	}
}
func TestRosterDiversityAndLeases(t *testing.T) {
	s, ch, cs := rosterFixture(t, 8)
	s.EnableRosters(RosterPolicy{5, "diversity", 5})
	s.Record(cs[0].StreamID, Result{Success: true})
	pool, e := s.QueueCandidates(ch, 20)
	if e != nil || len(pool) == 0 {
		t.Fatal(e)
	}
	if ProviderKey(pool[0].Provider) == ProviderKey(cs[0].Provider) {
		t.Fatal("did not favour unrepresented provider")
	}
	id := pool[0].StreamID
	now := time.Now().Unix()
	if !s.Claim("stream", id, now) || s.Claim("stream", id, now) {
		t.Fatal("duplicate claim")
	}
	if !s.Claim("stream", id, now+121) {
		t.Fatal("claim did not expire")
	}
	s.ReleaseClaim("stream", id)
	if !s.Claim("stream", id, now) {
		t.Fatal("claim not released")
	}
}

func TestProviderPacingLeavesPassPending(t *testing.T) {
	s, ch, cs := rosterFixture(t, 2)
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	s.Record(cs[0].StreamID, Result{Success: true})
	now := time.Now().Unix()
	s.EnsureRosterPass(now)
	s.ProviderTrouble(cs[0].Provider, now*1000, (now+300)*1000, true)
	if _, _, e := s.NextRosterCheck(now); e == nil {
		t.Fatal("provider throttle ignored")
	}
	s.EnsureRosterPass(now + 1)
	p, _ := s.CheckPassStatus()
	if p.Released != 0 || p.Checked != 0 || p.Remaining != 1 || p.Generation != 1 {
		t.Fatal(p)
	}
	rows, _ := s.RosterMembers(ch)
	if len(rows) != 1 {
		t.Fatal("blocked source evicted")
	}
}

func TestLowerRosterTargetRetainsExtrasUntilNaturalAttrition(t *testing.T) {
	s, ch, _ := rosterFixture(t, 0)
	// Distinct accounts: a channel roster admits at most one stream per account.
	for i := 0; i < 35; i++ {
		v, e := ingest.FromURL(fmt.Sprintf("https://unique%d.example/stream.ts", i), "MTV", "")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	cs, e := s.Candidates(ch, false)
	if e != nil || len(cs) != 35 {
		t.Fatal(len(cs), e)
	}
	size := func() int {
		var n int
		if e := s.DB.QueryRow("SELECT count(*) FROM roster WHERE channel_id=?", ch).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	record := func(id int64, ok bool) {
		if e := s.Record(id, Result{Success: ok}); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.EnableRosters(RosterPolicy{30, "stability", 5}); e != nil {
		t.Fatal(e)
	}
	for _, v := range cs[:30] {
		record(v.StreamID, true)
	}
	if size() != 30 {
		t.Fatal(size())
	}
	for _, override := range []bool{false, true} {
		var e error
		if override {
			e = s.SaveChannelPolicy(ch, RosterPolicy{Target: 20})
		} else {
			e = s.EnableRosters(RosterPolicy{20, "stability", 5})
		}
		if e != nil {
			t.Fatal(e)
		}
		if e = s.ReconcileRosters(); e != nil {
			t.Fatal(e)
		}
		if size() != 30 {
			t.Fatalf("lowering discarded members: %d", size())
		}
	}
	for _, v := range cs {
		record(v.StreamID, true)
	}
	if size() != 30 {
		t.Fatalf("recruited above threshold: %d", size())
	}
	for _, v := range cs[:10] {
		record(v.StreamID, false)
	}
	record(cs[30].StreamID, true)
	if size() != 20 {
		t.Fatalf("recruited at threshold: %d", size())
	}
	record(cs[10].StreamID, false)
	if size() != 19 {
		t.Fatal(size())
	}
	record(cs[30].StreamID, true)
	if size() != 20 {
		t.Fatalf("did not refill below threshold: %d", size())
	}
}
