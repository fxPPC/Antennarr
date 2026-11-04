package catalog

import (
	"antennarr/internal/ingest"
	"testing"
)

func TestAutomaticRegionalMatchingAndManualExclusion(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, e := s.SaveChannel(Channel{Number: "102", Name: "MTV", Region: "US", Enabled: true, Aliases: []string{"MTV East", "West MTV"}})
	if e != nil {
		t.Fatal(e)
	}
	names := []string{"US/MTV East", "USA West MTV", "MTV HD", "MTV 2", "MTV Classic", "UK MTV", "CNN International"}
	var rows []ingest.Entry
	for i, name := range names {
		rows = append(rows, ingest.Entry{Kind: "direct", Endpoint: "https://provider.example", URL: "https://provider.example/" + string(rune('a'+i)) + ".ts", Name: name})
	}
	if _, e = s.Import(rows, "test"); e != nil {
		t.Fatal(e)
	}
	all, e := s.Assignments()
	if e != nil || len(all) != 3 {
		t.Fatalf("assignments=%+v error=%v", all, e)
	}
	for _, a := range all {
		if a.ChannelID != id || a.Origin != "automatic" {
			t.Fatal(a)
		}
	}
	excluded := all[0].StreamID
	s.Map(id, excluded, false)
	s.Import(rows, "again")
	all, _ = s.Assignments()
	for _, a := range all {
		if a.StreamID == excluded && a.Enabled {
			t.Fatal("explicit exclusion was lost")
		}
	}
	s.SaveChannel(Channel{ID: id, Number: "102", Name: "MTV", Region: "US", Enabled: true, Aliases: []string{"MTV East", "West MTV", "-west"}})
	all, _ = s.Assignments()
	for _, a := range all {
		if a.Name == "USA West MTV" && a.Enabled {
			t.Fatal("West feed survived East-only edit")
		}
	}
	// Two selected editions and an unspecified upstream are ambiguous: no automatic match.
	s.SaveChannel(Channel{Number: "103", Name: "MTV", Region: "US", Enabled: true})
	all, _ = s.Assignments()
	for _, a := range all {
		if a.Name == "MTV HD" && a.Origin == "automatic" {
			t.Fatal("ambiguous feed auto-mapped")
		}
	}
}

func TestProviderNameVariations(t *testing.T) {
	for _, c := range []struct {
		channel, source string
		want            bool
	}{
		{"MTV", "123 | US: MTVHD (WEST) H.264", false}, {"MTV", "MTV1 FHD", true}, {"MTV", "MTV 2 HD", false}, {"MTV", "MTV Classic HD", false},
		{"FOX News", "USA | FOXNEWS UHD", true}, {"Animal Planet", "US: AnimalPlanetHD", true}, {"CNN", "CNNHD US", true}, {"CNN", "CNN InternationalHD", false},
		{"MS NOW", "US MSNBC FHD", true}, {"Discovery", "DiscoveryChannel HD", true}, {"Discovery", "Discovery Science HD", false}, {"TLC", "The Learning Channel US", true},
	} {
		t.Run(c.source, func(t *testing.T) {
			got := nameMatches(Channel{Name: c.channel, Region: "US"}, c.source, "")
			if got != c.want {
				t.Fatalf("match %q: %v want %v", c.source, got, c.want)
			}
		})
	}
}
