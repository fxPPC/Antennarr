package catalog

import (
	"antennarr/internal/ingest"
	"fmt"
	"testing"
)

func TestUHDFilteringAndToggleRestore(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ids := []string{}
	for i, name := range []string{"CNN", "MTV"} {
		ch, _ := s.SaveChannel(Channel{Number: fmt.Sprint(i + 1), Name: name, Region: "US", Enabled: true})
		ids = append(ids, ch)
		for n, label := range []string{name + " HD", name + " uHd+"} {
			entry, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/%d.ts", n, i), label, "")
			s.Import([]ingest.Entry{entry}, "fixture")
		}
	}
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, ch := range ids {
		cs, _ := s.Candidates(ch, false)
		for _, c := range cs {
			s.Record(c.StreamID, Result{Success: true})
		}
		members, e := s.RosterMembers(ch)
		if e != nil || len(members) != 1 || members[0].UHD {
			t.Fatal("UHD admitted while off", members, e)
		}
		queue, _ := s.QueueCandidates(ch, 20)
		for _, c := range queue {
			if c.UHD {
				t.Fatal("UHD queued while off")
			}
		}
	}
	if e = s.SetUHDAllowed(false); e != nil {
		t.Fatal(e)
	}
	state, _ := s.RosterState()
	if state.Ready != 2 {
		t.Fatal("unchanged setting rebuilt roster")
	}
	if e = s.SetUHDAllowed(true); e != nil {
		t.Fatal(e)
	}
	state, _ = s.RosterState()
	if state.Ready != 0 {
		t.Fatal("enabling did not rebuild ALL rosters")
	}
	for _, ch := range ids {
		cs, _ := s.Candidates(ch, false)
		for _, c := range cs {
			s.Record(c.StreamID, Result{Success: true})
		}
		members, _ := s.RosterMembers(ch)
		if len(members) != 2 || !members[0].UHD {
			t.Fatal("UHD not preferred", members)
		}
	}
	s.SetUHDAllowed(true)
	state, _ = s.RosterState()
	if state.Ready != 4 {
		t.Fatal("same setting rebuilt")
	}
	s.SetUHDAllowed(false)
	state, _ = s.RosterState()
	if state.Ready != 2 {
		t.Fatal("disabling did not restore the previous non-UHD roster")
	}
	queued, _ := s.RosterSummary()
	if queued.QueuedStreams != 2 {
		t.Fatal("UHD entries were not retained in the input queue", queued.QueuedStreams)
	}
	var sources int
	s.DB.QueryRow("SELECT count(*) FROM streams").Scan(&sources)
	if sources != 4 {
		t.Fatal("source records deleted")
	}
	for _, ch := range ids {
		cs, _ := s.Candidates(ch, false)
		for _, c := range cs {
			s.Record(c.StreamID, Result{Success: true})
		}
	}
	s.DB.Exec("UPDATE roster SET due=0")
	s.EnsureRosterPass(100)
	due, _, e := s.NextRosterCheck(100)
	if e != nil || due.UHD {
		t.Fatal("invalid due probe", due, e)
	}
}
func TestDiversitySpreadsHostnamesAndQuality(t *testing.T) {
	in := []Candidate{{StreamID: 1, Provider: "https://a.example:80", UHD: true}, {StreamID: 2, Provider: "https://a.example:8080", UHD: true}, {StreamID: 3, Provider: "https://b.example", UHD: true}, {StreamID: 4, Provider: "https://c.example", UHD: false}, {StreamID: 5, Provider: "https://d.example", UHD: false}, {StreamID: 6, Provider: "https://c.example:81", UHD: false}}
	out := OrderCandidates(in, true, true)
	want := []int64{1, 3, 2, 4, 5, 6}
	for i, c := range out {
		if c.StreamID != want[i] {
			t.Fatal("hostname spread or quality order wrong", out)
		}
	}
	if !IsUHD("CNN uHd+") || IsUHD("CNN FHD") {
		t.Fatal("UHD classifier")
	}
}

func TestCompactESPNUHDUsesESPNQualityPolicy(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	espn, e := s.SaveChannel(Channel{Name: "ESPN", Number: "1", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	espnu, e := s.SaveChannel(Channel{Name: "ESPNU", Number: "2", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for i, name := range []string{"ESPN HD", "ESPNUHD", "ESPNU HD"} {
		ent, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/live.ts", i), name, "")
		if _, e = s.Import([]ingest.Entry{ent}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	if !IsUHD("ESPNUHD") || IsUHD("ESPNU HD") {
		t.Fatal("compact UHD classification")
	}
	for _, label := range []string{"ESPNUHD", "espnuhd", "US | ESPNUHD", "ESPN UHD"} {
		if !nameMatches(Channel{Name: "ESPN", Region: "US"}, label, "US") || nameMatches(Channel{Name: "ESPNU", Region: "US"}, label, "US") {
			t.Fatalf("wrong channel identity for %q", label)
		}
	}
	a, e := s.QueueCandidates(espn, 20)
	if e != nil || len(a) != 1 || a[0].UHD {
		t.Fatal("UHD leaked while off", a, e)
	}
	u, e := s.Candidates(espnu, false)
	if e != nil || len(u) != 1 || u[0].UHD {
		t.Fatal("ESPNU HD lost or misclassified", u, e)
	}
	if e = s.EnableRosters(RosterPolicy{30, "stability", 5}); e != nil {
		t.Fatal(e)
	}
	if e = s.SetUHDAllowed(true); e != nil {
		t.Fatal(e)
	}
	a, e = s.Candidates(espn, false)
	if e != nil || len(a) != 2 {
		t.Fatal("compact UHD missing while on", a, e)
	}
	for _, v := range a {
		if e = s.Record(v.StreamID, Result{Success: true}); e != nil {
			t.Fatal(e)
		}
	}
	roster, e := s.RosterMembers(espn)
	if e != nil || len(roster) != 2 || !roster[0].UHD {
		t.Fatal("UHD not promoted", roster, e)
	}
	if e = s.SetUHDAllowed(false); e != nil {
		t.Fatal(e)
	}
	a, e = s.QueueCandidates(espn, 20)
	if e != nil || len(a) != 1 || a[0].UHD {
		t.Fatal("compact UHD remains selectable while off", a, e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM streams").Scan(&count)
	if count != 3 {
		t.Fatal("toggle deleted input streams")
	}
}
