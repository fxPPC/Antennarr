package catalog

import (
	"strings"
	"testing"
	"time"
)

func TestDeletionNoticesSurviveSourceDeletionAndDoNotRepeat(t *testing.T) {
	s := pruneFixture(t)
	if _, e := s.DB.Exec("INSERT INTO events(happened,kind,source_id,reason) VALUES(?,'roster_removed',2,'Media failed; replacement queued')", time.Now().Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PruneCatalogue([]int64{6}); e != nil {
		t.Fatal(e)
	}
	var reason string
	var source int
	if e := s.DB.QueryRow("SELECT reason,source_id FROM events WHERE kind='roster_removed'").Scan(&reason, &source); e != nil || source != 0 || !strings.Contains(reason, "stream #2") {
		t.Fatal(reason, source, e)
	}
	var notices int
	s.DB.QueryRow("SELECT count(*) FROM events WHERE kind IN('account_removed','stream_removed')").Scan(&notices)
	if notices != 5 {
		t.Fatal("expected four account notices and one independent stream notice", notices)
	}
	if e := s.DB.QueryRow("SELECT reason FROM events WHERE kind='stream_removed'").Scan(&reason); e != nil || !strings.Contains(reason, "stream #2") || !strings.Contains(reason, "account #1") {
		t.Fatal(reason, e)
	}
	if _, e := s.PruneCatalogue([]int64{6}); e != nil {
		t.Fatal(e)
	}
	var after int
	s.DB.QueryRow("SELECT count(*) FROM events WHERE kind IN('account_removed','stream_removed')").Scan(&after)
	if after != notices {
		t.Fatal("duplicate removal notices", after)
	}
}

func TestRemovalNoticeRollsBackWithFailedDeletion(t *testing.T) {
	s := pruneFixture(t)
	s.DB.Exec("CREATE TRIGGER block_removal BEFORE DELETE ON accounts BEGIN SELECT RAISE(ABORT,'fixture');END")
	if _, e := s.PruneCatalogue([]int64{6}); e == nil {
		t.Fatal("expected rollback")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM events WHERE kind IN('account_removed','stream_removed')").Scan(&n)
	if n != 0 {
		t.Fatal("reported a deletion that rolled back", n)
	}
}

func TestManualQueueClearRecordsOneSummary(t *testing.T) {
	s, _, _ := rosterFixture(t, 3)
	out, e := s.ClearQueue(nil)
	if e != nil {
		t.Fatal(e)
	}
	if out.Accounts != 3 || out.Streams != 3 {
		t.Fatal(out)
	}
	var reason string
	var source int
	e = s.DB.QueryRow("SELECT reason,source_id FROM events WHERE kind='queue_cleared'").Scan(&reason, &source)
	if e != nil || source != 0 || !strings.Contains(reason, "3 accounts and 3 streams") {
		t.Fatal(reason, e)
	}
	s.ClearQueue(nil)
	var n int
	s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='queue_cleared'").Scan(&n)
	if n != 1 {
		t.Fatal("empty clear created noise", n)
	}
}

func TestRemovalNoticePreservesSafeReasonAndCounter(t *testing.T) {
	s, _, cs := rosterFixture(t, 1)
	s.SaveRemovalPolicy(RemovalPolicy{AccountFailures: 2, StreamFailures: 2})
	for i := 0; i < 2; i++ {
		if e := s.DiscoveryFailedReason(cs[0].AccountID, false, "http_error", "catalogue", 503, true); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.PruneCatalogue(nil); e != nil {
		t.Fatal(e)
	}
	var reason string
	s.DB.QueryRow("SELECT reason FROM events WHERE kind='account_removed'").Scan(&reason)
	for _, part := range []string{"provider p0.example", "http_error", "catalogue", "503"} {
		if !strings.Contains(reason, part) {
			t.Fatal(reason)
		}
	}
	v, e := s.RosterSummary()
	if e != nil || v.Retired24 != 1 || v.RetiredDetails24 != 1 || v.RetiredMissing24 != 0 {
		t.Fatal(v, e)
	}
	// Retention or legacy deletion totals must remain honest when detail is absent.
	s.DB.Exec("DELETE FROM events WHERE kind='account_removed'")
	v, e = s.RosterSummary()
	if e != nil || v.Retired24 != 1 || v.RetiredDetails24 != 0 || v.RetiredMissing24 != 1 {
		t.Fatal(v, e)
	}
}
