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
	for i, service := range []struct{ name, uhd string }{{"CNN", " uHd+"}, {"MTV", " 4k"}} {
		name := service.name
		ch, _ := s.SaveChannel(Channel{Number: fmt.Sprint(i + 1), Name: name, Region: "US", Enabled: true})
		ids = append(ids, ch)
		for n, label := range []string{name + " HD", name + service.uhd} {
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

func TestTunerPlaybackQualityIsIndependent(t *testing.T) {
	s, us, cs := rosterFixture(t, 3)
	addTestProfile(t, s, "gb", "GB")
	gb := saveProfileTestChannel(t, s, "gb", "BBC One", "GB")
	if err := s.EnableRosters(RosterPolicy{30, "stability", 5}); err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	before, err := s.RosterMembers(us)
	if err != nil || len(before) == 0 {
		t.Fatal(before, err)
	}
	p, err := s.Profile("gb")
	if err != nil {
		t.Fatal(err)
	}
	p.PreferredHeight = 2160
	if _, err = s.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	after, err := s.RosterMembers(us)
	if err != nil || len(after) != len(before) || s.PreferredHeight(gb.ID) != 2160 || s.PreferredHeight(us) != 0 {
		t.Fatal(after, err)
	}
	if err = s.MigratePlaybackQuality(true); err != nil {
		t.Fatal(err)
	}
	p.PreferredHeight = 720
	if _, err = s.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	if err = s.MigratePlaybackQuality(true); err != nil {
		t.Fatal(err)
	}
	if s.PreferredHeight(gb.ID) != 720 || s.PreferredHeight(us) != 2160 {
		t.Fatal("legacy preference overwrote tuner edit or missed existing tuner")
	}
}

func TestUHDResolutionMarkers(t *testing.T) {
	for _, label := range []string{"Sport [4K]", "Film 8k", "Music 2160i", "Match 2160p50fps", "NEWS_4K", "News [Ultra HD]", "News_Ultra-HD", "Sport UltraHD", "Sport ⁴ᵏ", "ESPNUHD"} {
		if !IsUHD(label) {
			t.Errorf("expected UHD marker in %q", label)
		}
	}
	for _, label := range []string{"4Kids", "24Kitchen", "Radio 2160", "Sport 1080p", "4Kidz HD", "SKY 4Kids", "BBC Four", "É4K", "News Ultra Comedy HD", "UltraHDTV", "Ultra | HD"} {
		if IsUHD(label) {
			t.Errorf("unexpected UHD marker in %q", label)
		}
	}
}

func TestUHDDecorationsPreserveChannelIdentity(t *testing.T) {
	for _, tc := range []struct{ label, station string }{
		{"NEWS_4K", "News"}, {"News_2160p60", "News"}, {"News 2160i", "News"},
		{"News [Ultra HD]", "News"}, {"News_Ultra-HD", "News"}, {"News UltraHD", "News"},
		{"SKY 4Kids", "Sky 4Kids"}, {"BBC Four", "BBC Four"}, {"News Ultra Comedy HD", "News Ultra Comedy"},
	} {
		got, _ := channelKey(tc.label, "")
		want, _ := channelKey(tc.station, "")
		if got != want {
			t.Errorf("%q resolved to %q, want %q", tc.label, got, want)
		}
	}
}

func TestUpgradeReleasesOnlyIneligible4KRosterPlaces(t *testing.T) {
	s, hd, candidates := rosterFixture(t, 2)
	p := addTestProfile(t, s, "uhd", "US")
	p.PreferredHeight = 2160
	if _, err := s.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	uhd := saveProfileTestChannel(t, s, p.ID, "MTV", "US")
	if err := s.EnableRosters(RosterPolicy{30, "stability", 5}); err != nil {
		t.Fatal(err)
	}
	for _, c := range candidates {
		if err := s.Record(c.StreamID, Result{Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Previous builds could admit a 4K-labelled feed to either tuner.
	if _, err := s.DB.Exec("UPDATE streams SET name='MTV 4K' WHERE id=?", candidates[0].StreamID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.MigratePlaybackQuality(false); err != nil {
			t.Fatal(err)
		}
		for channel, want := range map[string]int{hd: 1, uhd.ID: 2} {
			var count int
			if err := s.DB.QueryRow("SELECT count(*) FROM roster WHERE channel_id=?", channel).Scan(&count); err != nil || count != want {
				t.Fatalf("roster %s has %d, want %d: %v", channel, count, want, err)
			}
		}
	}
}
