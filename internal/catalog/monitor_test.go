package catalog

import (
	"antennarr/internal/ingest"
	"context"
	"testing"
	"time"
)

func TestMonitorStagesCoverageAndPersistence(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ch, e := s.SaveChannel(Channel{Number: "1", Name: "CNN", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, host := range []string{"one", "two", "three"} {
		v, _ := ingest.FromURL("https://"+host+".example/live/u/p/1.ts", "CNN", "")
		s.Import([]ingest.Entry{v}, "fixture")
	}
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	now := time.Now().Unix()
	rows, _ := s.Candidates(ch, false)
	s.AccountMetadata(rows[0].AccountID, 2, 0, 0)
	s.CatalogueChecked(rows[0].AccountID, now)
	s.Record(rows[0].StreamID, Result{Success: true})
	s.DB.Exec("UPDATE accounts SET last_check=?,catalogue_checked=0 WHERE id=?", now, rows[1].AccountID)
	got, e := s.MonitorSummary(context.Background(), now)
	if e != nil {
		t.Fatal(e)
	}
	if got.Coverage.Single != 1 || got.Queue.InRosters != 1 || got.Queue.Discovery != 1 || got.Queue.Access != 1 || got.Progress.Successful != 1 {
		t.Fatalf("%+v", got)
	}
	if !got.EligibleMedia {
		t.Fatal("vacant channel candidates not recognised")
	}
	if got.Progress.LastSuccess == 0 || got.Progress.LastDiscovery == 0 {
		t.Fatal("saved progress missing")
	}
	// A read snapshot must not wait for a writer in WAL mode.
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e = s.MonitorSummary(ctx, now); e != nil {
		t.Fatal("status blocked behind writer", e)
	}
}
func TestPrimaryAssignmentsFollowExistingChannelEdits(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, e := s.SaveChannel(Channel{Number: "101", Name: "CNN", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.SaveChannel(Channel{ID: id, Number: "078", Name: "CNN US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	var n, name string
	if e = s.DB.QueryRow("SELECT number,display_name FROM output_assignments WHERE profile_id='primary' AND channel_id=?", id).Scan(&n, &name); e != nil || n != "078" || name != "CNN US" {
		t.Fatal(n, name, e)
	}
	var fk int
	s.DB.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&fk)
	if fk != 0 {
		t.Fatal("broken foreign keys")
	}
}

func TestFullRostersDoNotAdvertiseQueuedAccountsAsRunnableWork(t *testing.T) {
	s, ch, cs := rosterFixture(t, 2)
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	for _, v := range cs {
		s.Record(v.StreamID, Result{Success: true})
	}
	s.Import([]ingest.Entry{{Kind: "xtream", Endpoint: "https://queued.invalid", Username: "fixture", Password: "fixture"}}, "fixture")
	now := time.Now().Unix()
	s.EnsureRosterPass(now)
	for _, v := range cs {
		if err := s.ProviderTrouble(v.Provider, now*1000, (now+600)*1000, true); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.MonitorSummary(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Coverage.Vacancies != 0 || v.EligibleDiscovery != 0 || v.EligibleMedia || v.Queue.Access != 1 {
		t.Fatal("false eligible-work alert", v)
	}
	at, err := s.NextRosterCheckAt(now)
	if err != nil || at < now+600 {
		t.Fatal("missing provider retry time", at, err)
	}
	_ = ch
}
func TestCoverageVacanciesArePerChannelNotNetSurplus(t *testing.T) {
	s, _, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{3, "stability", 5})
	for _, v := range cs {
		s.Record(v.StreamID, Result{Success: true})
	}
	other, err := s.SaveChannel(Channel{Name: "CNN", Number: "2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	s.Map(other, cs[0].StreamID, true)
	s.Record(cs[0].StreamID, Result{Success: true})
	v, err := s.MonitorSummary(context.Background(), time.Now().Unix())
	if err != nil || v.Coverage.Vacancies != 1 || v.Coverage.Verified != 4 || v.Coverage.Target != 4 {
		t.Fatal("surplus hid an unfilled channel", v, err)
	}
}
