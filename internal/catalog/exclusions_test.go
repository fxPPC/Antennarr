package catalog

import (
	"antennarr/internal/ingest"
	"testing"
)

func TestNegativeAliases(t *testing.T) {
	for _, alias := range []string{"-international", "- international", "  - INTERNATIONAL  "} {
		c := Channel{Name: "CNN", Region: "US", Aliases: []string{"CNN International", alias}}
		if nameMatches(c, "US / CNN International HD", "US") {
			t.Fatalf("exclusion ignored: %q", alias)
		}
		if !nameMatches(c, "USA CNN HD", "US") {
			t.Fatal("excluded the main channel")
		}
	}
	c := Channel{Name: "CNN", Aliases: []string{"- spanish feed", "-"}}
	if !nameExcluded(c, "CNN / Spanish-Feed HD") {
		t.Fatal("phrase/punctuation ignored")
	}
	if nameExcluded(c, "CNN Spanish News Feed") {
		t.Fatal("matched non-contiguous phrase")
	}
	if nameExcluded(Channel{Aliases: []string{"-international"}}, "CNN Internationalist") {
		t.Fatal("matched part of a word")
	}
	if nameMatches(Channel{Name: "CNN", Aliases: []string{"- Bloomberg"}}, "Bloomberg", "") {
		t.Fatal("negative alias acted as an inclusion")
	}
}
func TestExclusionsOverrideLearnedTVGAndRemoveRoster(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := Channel{Number: "1", Name: "CNN", Region: "US", Enabled: true, Aliases: []string{"CNN International"}}
	c.ID, e = s.SaveChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	entry, _ := ingest.FromURL("https://fixture.example/live/u/p/1.ts", "CNN International", "")
	entry.TVGID = "cnn.us"
	if _, e = s.Import([]ingest.Entry{entry}, "fixture"); e != nil {
		t.Fatal(e)
	}
	cs, _ := s.Candidates(c.ID, false)
	if len(cs) != 1 {
		t.Fatal("positive fixture did not map")
	}
	id := cs[0].StreamID
	if e = s.Map(c.ID, id, true); e != nil {
		t.Fatal(e)
	} // Persist a learned TVG match.
	s.Record(id, Result{Success: true})
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	c.Aliases = append(c.Aliases, "- international")
	if _, e = s.SaveChannel(c); e != nil {
		t.Fatal(e)
	}
	cs, e = s.Candidates(c.ID, false)
	if e != nil || len(cs) != 0 {
		t.Fatal("learned match bypassed exclusion", cs, e)
	}
	members, e := s.RosterMembers(c.ID)
	if e != nil || len(members) != 0 {
		t.Fatal("excluded stream retained roster place", e)
	}
	relevant, e := s.RelevantEntries([]ingest.Entry{entry})
	if e != nil || len(relevant) != 0 {
		t.Fatal("excluded TVG ingested", e)
	}
	entry.Name = "CNN HD"
	relevant, e = s.RelevantEntries([]ingest.Entry{entry})
	if e != nil || len(relevant) != 1 {
		t.Fatal("valid channel lost", e)
	}
	// Removing the exclusion restores ordinary matching; no source records are deleted.
	c.Aliases = c.Aliases[:1]
	s.SaveChannel(c)
	cs, e = s.Candidates(c.ID, false)
	if e != nil || len(cs) != 1 {
		t.Fatal("could not undo exclusion", e)
	}
}
