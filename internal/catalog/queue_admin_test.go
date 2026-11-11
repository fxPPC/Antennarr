package catalog

import (
	"antennarr/internal/ingest"
	"fmt"
	"testing"
	"time"
)

func TestInputPaginationAndPrunedLastPage(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 105; i++ {
		s.DB.Exec("INSERT INTO accounts(identity,provider,kind,secret,created)VALUES(?,?,'xtream','{}',0)", fmt.Sprint(i), fmt.Sprintf("p%03d.example", i))
	}
	p, e := s.InputPage("", 100)
	if e != nil || p.Total != 105 || len(p.Items) != 5 || p.HasNext {
		t.Fatal(p, e)
	}
	s.DB.Exec("DELETE FROM accounts WHERE id>50")
	p, e = s.InputPage("", 100)
	if e != nil || p.Offset != 0 || len(p.Items) != 50 || p.HasNext {
		t.Fatal(p, e)
	}
	p, e = s.InputPage("%", 0)
	if e != nil || p.Total != 0 {
		t.Fatal("wildcard filter", p, e)
	}
}
func TestRandomQueuePersistsAndRequeuesWithoutCooldown(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := s.SaveChannel(Channel{Number: "1", Name: "CNN", Enabled: true})
	for i := 0; i < 20; i++ {
		entry, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/live/u/p/1.ts", i), "CNN", "")
		s.Import([]ingest.Entry{entry}, "fixture")
	}
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	first, e := s.QueueCandidates(id, 20)
	if e != nil || len(first) != 20 {
		t.Fatal(len(first), e)
	}
	var before int64
	s.DB.QueryRow("SELECT position FROM queue_positions WHERE kind='stream' AND source_id=?", first[0].StreamID).Scan(&before)
	s.Record(first[0].StreamID, Result{Success: true})
	s.Record(first[0].StreamID, Result{Success: false})
	var after int64
	s.DB.QueryRow("SELECT position FROM queue_positions WHERE kind='stream' AND source_id=?", first[0].StreamID).Scan(&after)
	if before == after {
		t.Fatal("not randomized")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM work_leases WHERE kind='cooldown'").Scan(&n)
	if n != 0 {
		t.Fatal("cooldown remains")
	}
	rows, _ := s.QueueCandidates(id, 20)
	if len(rows) != 20 {
		t.Fatal("failed source not eligible", len(rows))
	}
	want := rows[0].StreamID
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.EnableRosters(RosterPolicy{2, "stability", 5})
	rows, _ = s.QueueCandidates(id, 20)
	if rows[0].StreamID != want {
		t.Fatal("restart reshuffled queue")
	}
	s.DB.QueryRow("SELECT count(*) FROM queue_positions WHERE kind='account'").Scan(&n)
	if n != 20 {
		t.Fatal(n)
	}
}
func TestRetirementCountsSurviveDeletionAndExpire(t *testing.T) {
	s := pruneFixture(t)
	result, e := s.PruneCatalogue([]int64{6})
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.RosterSummary()
	if e != nil || v.Retired24 != int(result.Accounts) {
		t.Fatal(v.Retired24, result, e)
	}
	s.DB.Exec("INSERT INTO settings VALUES(?, '99')", fmt.Sprintf("accounts_retired_minute_%d", time.Now().Unix()/60-3000))
	if e = s.Housekeep(); e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM settings WHERE name GLOB 'accounts_retired_minute_*'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}
func TestBulkResetAndClearPreserveSources(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, _ := s.SaveChannel(Channel{Number: "78", Name: "CNN", Region: "US", Enabled: true})
	entry, _ := ingest.FromURL("https://p.example/1.ts", "CNN", "")
	s.Import([]ingest.Entry{entry}, "fixture")
	if e = s.ReplaceChannels(true); e != nil {
		t.Fatal(e)
	}
	chs, _ := s.Channels()
	if len(chs) != 150 {
		t.Fatal(len(chs))
	}
	for _, c := range chs {
		if c.Name == "CNN" && c.ID != id {
			t.Fatal("identity changed")
		}
	}
	if e = s.ReplaceChannels(false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PruneCatalogue(nil); e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM accounts").Scan(&n)
	if n != 1 {
		t.Fatal("clearing outputs deleted input", n)
	}
}

func TestInputPagesCountAccountsAcrossSharedProviders(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 125; i++ {
		v, e := ingest.FromURL(fmt.Sprintf("https://group-%d.example/get.php?username=fixture-%d&password=fixture", i%4, i), "", "")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[int64]bool{}
	for offset := 0; offset < 125; offset += 50 {
		p, e := s.InputPage("", offset)
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Items) != min(50, 125-offset) || p.Total != 125 {
			t.Fatal(offset, len(p.Items), p.Total)
		}
		for _, a := range p.Items {
			if seen[a.ID] {
				t.Fatal("duplicate account across pages", a.ID)
			}
			seen[a.ID] = true
		}
	}
	if len(seen) != 125 {
		t.Fatal("accounts missing", len(seen))
	}
}
