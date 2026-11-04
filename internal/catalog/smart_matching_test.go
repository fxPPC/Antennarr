package catalog

import (
	"antennarr/internal/ingest"
	"strings"
	"testing"
)

func TestSmartNameMatching(t *testing.T) {
	for _, tc := range []struct {
		channel, source, region string
		want                    bool
	}{
		{"CNN", "001 | USA: CNNHD H.264", "US", true},
		{"CNN", "US CNN International HD", "US", false},
		{"Discovery", "Discovery Science FHD", "US", false},
		{"MTV", "MTV 2 HD", "US", false},
		{"Channel 4", "Channel 4+1 HD", "UK", false},
		{"Channel 4 +1", "UK Channel 4 Plus 1 HD", "UK", true},
		{"Canal+", "CANAL HD", "FR", false},
		{"Canal+", "FR CANAL+ FHD", "FR", true},
		{"CNN", "CNN uHd+", "US", true},
		{"CNN", "CNN 1080p60fps HEVC", "US", true},
		{"Café TV", "Cafe\u0301 TV HD", "", true},
		{"日本テレビ", "日本テレビ HD", "JP", true},
		{"日本テレビ", "日本テレビHD", "JP", true},
		{"A&E", "US A&E HD", "US", true},
		{"A&E", "A and E HD", "US", true},
		{"East", "East HD", "", true},
		{"Россия 1", "Россия 1 HD", "", true},
		{"Россия 1", "Россия 2 HD", "", false},
		{"日本テレビ", "東京放送 HD", "", false},
		{"CNN", "ＣＮＮ ＨＤ", "US", true},
		{"CNN", "００１｜ＣＮＮ ＨＤ", "US", true},
		{"A&E", "US A&amp;E HD", "US", true},
		{"HD", "SD", "", false},
		{"!!!", "???", "", false},
		{"IT TAKES TWO", "IT TAKES TWO HD", "", true},
		{"TAKES TWO", "IT TAKES TWO HD", "", false},
		{"East Asia News", "Asia News HD", "", false},
		{"MTV", "USA West MTV HD", "US", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			if got := nameMatches(Channel{Name: tc.channel, Region: tc.region}, tc.source, ""); got != tc.want {
				t.Fatalf("%q vs %q: %v want %v", tc.channel, tc.source, got, tc.want)
			}
		})
	}
	if !nameExcluded(Channel{Aliases: []string{"-international"}}, "CNN InternationalHD") {
		t.Fatal("quality suffix hid exclusion")
	}
	if !nameExcluded(Channel{Aliases: []string{"- cafe\u0301", "-日本"}}, "Café HD") {
		t.Fatal("normalised exclusion failed")
	}
	if nameExcluded(Channel{Aliases: []string{"-日本"}}, "日本テレビ") {
		t.Fatal("exclusion matched part of a word")
	}
}
func TestLearnedIDsRespectCountryAndManualAssignmentsPersist(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ch, _ := s.SaveChannel(Channel{Number: "1", Name: "Example", Region: "US", Enabled: true})
	first, _ := ingest.FromURL("https://one.example/1.ts", "US Example HD", "")
	first.TVGID = "shared-id"
	s.Import([]ingest.Entry{first}, "fixture")
	cs, _ := s.Candidates(ch, false)
	if len(cs) != 1 {
		t.Fatal(cs)
	}
	s.Map(ch, cs[0].StreamID, true)
	other, _ := ingest.FromURL("https://two.example/2.ts", "GB Example HD", "")
	other.TVGID = "shared-id"
	s.Import([]ingest.Entry{other}, "fixture")
	cs, _ = s.Candidates(ch, false)
	if len(cs) != 1 {
		t.Fatal("learned ID crossed country", cs)
	}
	relevant, _ := s.RelevantEntries([]ingest.Entry{other})
	if len(relevant) != 0 {
		t.Fatal("metadata refresh crossed country")
	}
	// A new, unmatched explicit assignment must be marked manual on INSERT too.
	manual, _ := ingest.FromURL("https://three.example/3.ts", "Different name", "")
	s.Import([]ingest.Entry{manual}, "fixture")
	var id int64
	s.DB.QueryRow("SELECT id FROM streams WHERE name='Different name'").Scan(&id)
	if e = s.Map(ch, id, true); e != nil {
		t.Fatal(e)
	}
	s.RefreshMatching()
	cs, _ = s.Candidates(ch, false)
	if len(cs) != 2 {
		t.Fatal("new manual assignment lost on rematch", cs)
	}
}
func TestUnicodeLengthsAndRuleLimit(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	aliases := make([]string, 100)
	for i := range aliases {
		aliases[i] = "別名"
	}
	c := Channel{Number: "1", Name: strings.Repeat("日", 70), Aliases: aliases, Enabled: true}
	if _, e = s.SaveChannel(c); e != nil {
		t.Fatal("valid Unicode name/rules rejected", e)
	}
	c.Name = strings.Repeat("日", 101)
	if _, e = s.SaveChannel(c); e == nil {
		t.Fatal("overlong name accepted")
	}
}
