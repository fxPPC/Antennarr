package catalog

import (
	"antennarr/internal/ingest"
	"fmt"
	"testing"
)

func TestIncrementalImportDoesNotTouchUnchangedMappings(t *testing.T) {
	s, ch, cs := rosterFixture(t, 2)
	if len(cs) != 2 {
		t.Fatal(cs)
	}
	id := cs[0].StreamID
	if _, e := s.DB.Exec("CREATE TRIGGER protect_mapping BEFORE DELETE ON mappings WHEN old.stream_id=" + fmt.Sprint(id) + " BEGIN SELECT RAISE(ABORT,'unrelated mapping touched');END"); e != nil {
		t.Fatal(e)
	}
	v, _ := ingest.FromURL("https://new.example/new.ts", "MTV", "")
	if _, e := s.Import([]ingest.Entry{v}, "fixture"); e != nil {
		t.Fatal(e)
	}
	// A channel number-only edit must not rematch anything either.
	c := Channel{ID: ch, Number: "2", Name: "MTV", Region: "US", Enabled: true}
	if _, e := s.SaveChannel(c); e != nil {
		t.Fatal(e)
	}
}
func TestChangedChannelResolvesAmbiguityAndLeavesOtherChannelsAlone(t *testing.T) {
	s, ch, cs := rosterFixture(t, 1)
	other, e := s.SaveChannel(Channel{Number: "2", Name: "CNN", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	v, _ := ingest.FromURL("https://cnn.example/stream.ts", "CNN", "")
	s.Import([]ingest.Entry{v}, "fixture")
	var cnn int64
	s.DB.QueryRow("SELECT stream_id FROM mappings WHERE channel_id=?", other).Scan(&cnn)
	s.DB.Exec("CREATE TRIGGER protect_cnn BEFORE DELETE ON mappings WHEN old.stream_id=" + fmt.Sprint(cnn) + " BEGIN SELECT RAISE(ABORT,'unrelated CNN touched');END")
	duplicate, e := s.SaveChannel(Channel{Number: "3", Name: "MTV", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM mappings WHERE stream_id=?", cs[0].StreamID).Scan(&n)
	if n != 0 {
		t.Fatal("ambiguous name assigned", n)
	}
	if e = s.DeleteChannel(duplicate); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM mappings WHERE channel_id=? AND stream_id=?", ch, cs[0].StreamID).Scan(&n)
	if n != 1 {
		t.Fatal("resolved ambiguity not assigned", n)
	}
}
func TestStartupMatchingRevisionSkipsUnchangedCatalogue(t *testing.T) {
	s, _, _ := rosterFixture(t, 2)
	if e := s.RefreshMatching(); e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("CREATE TRIGGER forbid_refresh BEFORE DELETE ON mappings BEGIN SELECT RAISE(ABORT,'unexpected full refresh');END")
	if e := s.RefreshMatching(); e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("UPDATE settings SET value='old' WHERE name='matching_revision'")
	if e := s.RefreshMatching(); e == nil {
		t.Fatal("new matcher revision did not refresh")
	}
}
func TestIncrementalAndFullMatchingAgree(t *testing.T) {
	s, _, _ := rosterFixture(t, 5)
	cnn, _ := s.SaveChannel(Channel{Number: "3", Name: "CNN", Region: "US", Enabled: true, Aliases: []string{"-international", "-west"}})
	for _, name := range []string{"US CNN HD", "US CNN International", "US MTV HD", "UK MTV", "CNN West"} {
		v, _ := ingest.FromURL("https://fixture.example/"+fmt.Sprint(len(name))+".ts", name, "")
		if _, e := s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	get := func() string {
		var out string
		rows, _ := s.DB.Query("SELECT channel_id,stream_id,enabled,origin FROM mappings ORDER BY channel_id,stream_id")
		defer rows.Close()
		for rows.Next() {
			var c, o string
			var id, n int
			rows.Scan(&c, &id, &n, &o)
			out += fmt.Sprint(c, id, n, o)
		}
		return out
	}
	before := get()
	tx, _ := s.DB.Begin()
	if e := autoMap(tx); e != nil {
		t.Fatal(e)
	}
	tx.Commit()
	if get() != before {
		t.Fatal("incremental/full differ", cnn)
	}
}

func BenchmarkIncrementalImport(b *testing.B) {
	s, e := Open(b.TempDir())
	if e != nil {
		b.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 143; i++ {
		if _, e = s.SaveChannel(Channel{Number: fmt.Sprint(i + 1), Name: fmt.Sprintf("Service %d", i), Enabled: true}); e != nil {
			b.Fatal(e)
		}
	}
	entries := make([]ingest.Entry, 10000)
	for i := range entries {
		entries[i], _ = ingest.FromURL(fmt.Sprintf("https://fixture.example/%d.ts", i), fmt.Sprintf("Service %d", i%143), "")
	}
	if _, e = s.Import(entries, "fixture"); e != nil {
		b.Fatal(e)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry, _ := ingest.FromURL(fmt.Sprintf("https://new.example/%d.ts", i), "Service 0", "")
		if _, e = s.Import([]ingest.Entry{entry}, "fixture"); e != nil {
			b.Fatal(e)
		}
	}
}

func TestRuleExclusionEvictsPreviouslyAssignedRosterEntry(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		s, ch, cs := rosterFixture(t, 1)
		s.EnableRosters(RosterPolicy{30, "stability", 5})
		s.Record(cs[0].StreamID, Result{Success: true})
		if disabled {
			if e := s.Map(ch, cs[0].StreamID, false); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := s.SaveChannel(Channel{ID: ch, Number: "1", Name: "MTV", Region: "US", Enabled: true, Aliases: []string{"-mtv"}}); e != nil {
			t.Fatal(e)
		}
		var n int
		s.DB.QueryRow("SELECT count(*) FROM roster WHERE channel_id=?", ch).Scan(&n)
		if n != 0 {
			t.Fatal("forbidden roster member retained", disabled, n)
		}
	}
}

func TestBatchedImportPrevalidatesAndRetriesPartialDatabaseFailure(t *testing.T) {
	s, _, _ := rosterFixture(t, 0)
	entries := make([]ingest.Entry, 250)
	for i := range entries {
		entries[i], _ = ingest.FromURL(fmt.Sprintf("https://bulk.example/%d.ts", i), "MTV", "")
	}
	invalid := append(append([]ingest.Entry{}, entries...), ingest.Entry{URL: "invalid"})
	if n, e := s.Import(invalid, "fixture"); e == nil || n != 0 {
		t.Fatal(n, e)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM streams").Scan(&n)
	if n != 0 {
		t.Fatal("malformed list partially committed", n)
	}
	entries[220].Name = "Break"
	s.DB.Exec("CREATE TRIGGER fail_batch BEFORE INSERT ON streams WHEN new.name='Break' BEGIN SELECT RAISE(ABORT,'fixture database failure');END")
	if n, e := s.Import(entries, "fixture"); e == nil || n != 200 {
		t.Fatal("wrong committed batch count", n, e)
	}
	s.DB.QueryRow("SELECT count(*) FROM streams").Scan(&n)
	if n != 200 {
		t.Fatal("failed batch was not atomic", n)
	}
	s.DB.Exec("DROP TRIGGER fail_batch")
	if _, e := s.Import(entries, "fixture"); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM streams").Scan(&n)
	if n != 250 {
		t.Fatal("retry did not deduplicate", n)
	}
}
