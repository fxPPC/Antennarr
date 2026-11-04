package catalog

import (
	"antennarr/internal/ingest"
	"fmt"
	"testing"
)

func TestRegionalWordsAreOrdinaryChannelRules(t *testing.T) {
	for _, region := range []string{"US", "NZ", "GBR", ""} {
		t.Run(region, func(t *testing.T) {
			s, e := Open(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			c := Channel{Number: "1", Name: "Example", Region: region, Enabled: true, Aliases: []string{"Example East", "West Example", "Example W", "Example Pacific"}}
			c.ID, e = s.SaveChannel(c)
			if e != nil {
				t.Fatal(e)
			}
			s.EnableRosters(RosterPolicy{Target: 30, Preference: "stability", Race: 5})
			for i, name := range []string{"Example East", "West Example", "Example W", "Example Pacific"} {
				v, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/1.ts", i), name, "")
				v.Region = region
				s.Import([]ingest.Entry{v}, "fixture")
			}
			cs, e := s.Candidates(c.ID, false)
			if e != nil || len(cs) != 4 {
				t.Fatal("hidden regional policy", cs, e)
			}
			for _, v := range cs {
				s.Record(v.StreamID, Result{Success: true})
			}
			c.Aliases = append(c.Aliases, "-west", "-western", "-pacific", "-w")
			if _, e = s.SaveChannel(c); e != nil {
				t.Fatal(e)
			}
			cs, e = s.Candidates(c.ID, false)
			if e != nil || len(cs) != 1 {
				t.Fatal("negative rules ignored", cs, e)
			}
			members, _ := s.RosterMembers(c.ID)
			if len(members) != 1 {
				t.Fatal("excluded roster entries remain", members)
			}
			// A user's edit, without any global policy change, can accept West instead.
			c.Aliases = []string{"Example East", "West Example", "-east"}
			s.SaveChannel(c)
			cs, e = s.Candidates(c.ID, false)
			if e != nil || len(cs) != 1 || cs[0].URL != "https://p1.example/1.ts" {
				t.Fatal("cannot select West with rules", cs, e)
			}
		})
	}
	if !nameMatches(Channel{Name: "East Asia News"}, "East Asia News HD", "") {
		t.Fatal("regional word lost")
	}
	if nameMatches(Channel{Name: "Asia News"}, "East Asia News HD", "") {
		t.Fatal("regional word swallowed")
	}
}

func TestNegativeRulesCheckProviderIDsToo(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := Channel{Number: "1", Name: "CNN", Region: "US", Enabled: true, Aliases: []string{"-west"}}
	c.ID, e = s.SaveChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	entry, _ := ingest.FromURL("https://fixture.example/1.ts", "CNN HD", "")
	entry.TVGID = "cnn.us.west"
	s.Import([]ingest.Entry{entry}, "fixture")
	cs, _ := s.Candidates(c.ID, false)
	if len(cs) != 0 {
		t.Fatal("ID exclusion bypassed", cs)
	}
	relevant, _ := s.RelevantEntries([]ingest.Entry{entry})
	if len(relevant) != 0 {
		t.Fatal("ID exclusion bypassed by import")
	}
	var id int64
	s.DB.QueryRow("SELECT id FROM streams WHERE tvg_id='cnn.us.west'").Scan(&id)
	if e = s.Map(c.ID, id, true); e == nil {
		t.Fatal("manual mapping bypassed exclusion")
	}
	c.Aliases = nil
	s.SaveChannel(c)
	if e = s.Map(c.ID, id, true); e != nil {
		t.Fatal(e)
	}
	s.EnableRosters(RosterPolicy{Target: 30, Preference: "stability", Race: 5})
	s.Record(id, Result{Success: true})
	c.Aliases = []string{"-west"}
	s.SaveChannel(c)
	cs, _ = s.RosterMembers(c.ID)
	if len(cs) != 0 {
		t.Fatal("excluded ID remained in roster")
	}
}
