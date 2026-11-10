package catalog

import (
	"antennarr/internal/ingest"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestPassVisitsEveryStreamBeforeRepeatingAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	ch, _ := s.SaveChannel(Channel{Number: "1", Name: "CNN", Enabled: true})
	s.EnableRosters(RosterPolicy{3, "stability", 5})
	for i := 0; i < 3; i++ {
		v, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/1.ts", i), "CNN", "")
		s.Import([]ingest.Entry{v}, "fixture")
	}
	cs, _ := s.Candidates(ch, false)
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	// Old future deadlines must not postpone a continuous pass.
	s.DB.Exec("UPDATE roster SET due=?", time.Now().Unix()+999999)
	if e = s.EnsureRosterPass(100); e != nil {
		t.Fatal(e)
	}
	seen := map[int64]bool{}
	for i := 0; i < 2; i++ {
		c, _, e := s.NextRosterCheck(time.Now().Unix())
		if e != nil || seen[c.StreamID] {
			t.Fatal(c, e)
		}
		seen[c.StreamID] = true
		s.FinishRosterCheck(c.StreamID)
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.EnableRosters(RosterPolicy{3, "stability", 5})
	if e = s.EnsureRosterPass(200); e != nil {
		t.Fatal(e)
	}
	p, _ := s.CheckPassStatus()
	if p.Generation != 1 || p.Checked != 2 || p.Remaining != 1 {
		t.Fatal(p)
	}
	c, _, e := s.NextRosterCheck(time.Now().Unix())
	if e != nil || seen[c.StreamID] {
		t.Fatal(c, e)
	}
	s.FinishRosterCheck(c.StreamID)
	if _, _, e = s.NextRosterCheck(time.Now().Unix()); e != sql.ErrNoRows {
		t.Fatal(e)
	}
	if e = s.EnsureRosterPass(205); e != nil {
		t.Fatal(e)
	}
	p, _ = s.CheckPassStatus()
	if p.Generation != 2 || p.LastSeconds != 105 || p.LastChecked != 3 || p.Remaining != 3 {
		t.Fatal(p)
	}
}
func TestCompleteDiscoveryFailureStreakAndRandomQueueRotation(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SaveRemovalPolicy(RemovalPolicy{2, 2})
	for i := 0; i < 30; i++ {
		v, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/get.php?username=fixture&password=fixture", i), "", "")
		s.Import([]ingest.Entry{v}, "fixture")
	}
	a, e := s.QueueAccount(time.Now().Unix(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	var first int64
	s.DB.QueryRow("SELECT position FROM queue_positions WHERE kind='account' AND source_id=?", a.ID).Scan(&first)
	if e = s.DiscoveryFailed(a.ID, false, true); e != nil {
		t.Fatal(e)
	}
	s.AccountMetadata(a.ID, 2, 0, 0) // Successful login cannot erase catalogue failure.
	var failures int
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", a.ID).Scan(&failures)
	if failures != 1 {
		t.Fatal(failures)
	}
	var next int64
	s.DB.QueryRow("SELECT position FROM queue_positions WHERE kind='account' AND source_id=?", a.ID).Scan(&next)
	if next == first {
		t.Fatal("retry not randomly inserted")
	}
	s.DiscoveryFailed(a.ID, false, true)
	var state string
	s.DB.QueryRow("SELECT state FROM accounts WHERE id=?", a.ID).Scan(&state)
	if state != "retired" {
		t.Fatal(state)
	}
	if _, e = s.PruneCatalogue(nil); e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=?", a.ID).Scan(&n)
	if n != 0 {
		t.Fatal("account not deleted")
	}
	// Saved successes reset the streak, while a zero threshold stays opt-out.
	b, _ := s.QueueAccount(time.Now().Unix(), time.Hour)
	s.DiscoveryFailed(b.ID, false)
	s.CatalogueChecked(b.ID, time.Now().Unix())
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", b.ID).Scan(&failures)
	if failures != 0 {
		t.Fatal(failures)
	}
	s.DiscoveryFailed(b.ID, false)
	s.AccountMetadata(b.ID, 2, 0, 0)
	s.AccountUsable(b.ID)
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", b.ID).Scan(&failures)
	if failures != 0 {
		t.Fatal("fresh successful access with saved catalogue did not reset auth failure")
	}
	s.SaveRemovalPolicy(RemovalPolicy{})
	for i := 0; i < 3; i++ {
		s.DiscoveryFailed(b.ID, false)
	}
	s.DB.QueryRow("SELECT state FROM accounts WHERE id=?", b.ID).Scan(&state)
	if state == "retired" {
		t.Fatal("zero threshold ignored")
	}
}

// A roster membership is either pending or checked. A released marker attached
// to a surviving member is repaired, rather than silently losing its check.
func TestPassRepairsReleasedMarkersForSurvivingMemberships(t *testing.T) {
	s, _, cs := rosterFixture(t, 2)
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	now := time.Now().Unix()
	s.EnsureRosterPass(now)
	s.DB.Exec("UPDATE roster_pass_work SET status=2")
	if e := s.EnsureRosterPass(now + 1); e != nil {
		t.Fatal(e)
	}
	p, e := s.CheckPassStatus()
	if e != nil || p.Generation != 1 || p.Checked != 0 || p.Remaining != 2 || p.Released != 0 {
		t.Fatal(p, e)
	}
}

func TestFullRosterAccountReleasesPlacesWithoutStrike(t *testing.T) {
	s, _, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	now := time.Now().Unix()
	if err := s.EnsureRosterPass(now); err != nil {
		t.Fatal(err)
	}
	s.AccountMetadata(cs[0].AccountID, 1, 0, 1)
	if err := s.AccountFull(cs[0].AccountID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureRosterPass(now + 1); err != nil {
		t.Fatal(err)
	}
	var status, n int
	s.DB.QueryRow("SELECT status FROM roster_pass_work WHERE stream_id=?", cs[0].StreamID).Scan(&status)
	s.DB.QueryRow("SELECT count(*) FROM roster").Scan(&n)
	p, err := s.CheckPassStatus()
	if err != nil || status != 2 || n != 2 || p.Deferred != 0 || p.Released != 1 {
		t.Fatal("full account was rearmed/removed", status, n, p, err)
	}
}

func TestNewPassPrioritisesOldestVerification(t *testing.T) {
	s, _, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for i, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
		s.DB.Exec("UPDATE roster SET verified=? WHERE stream_id=?", 100+i, c.StreamID)
	}
	if e := s.EnsureRosterPass(time.Now().Unix()); e != nil {
		t.Fatal(e)
	}
	c, _, e := s.NextRosterCheck(time.Now().Unix())
	if e != nil || c.StreamID != cs[0].StreamID {
		t.Fatal(c, e)
	}
}
