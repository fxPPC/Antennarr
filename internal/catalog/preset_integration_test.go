package catalog

import (
	"antennarr/internal/ingest"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPresetSiblingAssignments(t *testing.T) {
	b, e := presetFixtures.ReadFile("testdata/us-manhattan-expanded.json")
	if e != nil {
		t.Fatal(e)
	}
	var preset struct {
		Channels []Channel `json:"channels"`
	}
	if e = json.Unmarshal(b, &preset); e != nil {
		t.Fatal(e)
	}
	groups := [][]string{{"CNN", "CNN International", "CNN en Español"}, {"Discovery Channel", "Discovery Life", "Discovery Family", "Discovery Familia", "Discovery en Español"}, {"HBO", "HBO Hits", "HBO Drama", "HBO Movies", "HBO Latino"}, {"STARZ ENCORE", "STARZ ENCORE Classic", "STARZ ENCORE Westerns"}, {"MGM+", "MGM+ Hits", "MGM+ Marquee", "MGM+ Drive-In"}}
	for _, names := range groups {
		t.Run(names[0], func(t *testing.T) {
			s, e := Open(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			for i, name := range names {
				var c Channel
				for _, v := range preset.Channels {
					if v.Name == name {
						c = v
						break
					}
				}
				c.ID = name
				c.Number = fmt.Sprint(i + 1)
				if _, e = s.SaveChannel(c); e != nil {
					t.Fatal(e)
				}
			}
			for i, name := range names {
				ent, e := ingest.FromURL(fmt.Sprintf("https://fixture.example/live/u/p/%d.ts", i), "US | "+name+" HD", "")
				if e != nil {
					t.Fatal(e)
				}
				ent.TVGID = "fixture." + name
				if _, e = s.Import([]ingest.Entry{ent}, "fixture"); e != nil {
					t.Fatal(e)
				}
			}
			for _, name := range names {
				rows, e := s.Candidates(name, false)
				if e != nil || len(rows) != 1 {
					t.Fatalf("%s: %d candidates, %v", name, len(rows), e)
				}
			}
		})
	}
}

func TestAmbiguousCompactLabelCannotInheritTVG(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c, e := s.SaveChannel(Channel{Name: "HBO", Number: "1", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	first, _ := ingest.FromURL("https://fixture.example/1.ts", "HBO", "")
	first.TVGID = "hbo.us"
	if _, e = s.Import([]ingest.Entry{first}, "fixture"); e != nil {
		t.Fatal(e)
	}
	streams, _ := s.Streams("")
	if len(streams) != 1 {
		t.Fatal(streams)
	}
	if e = s.Map(c, streams[0].ID, true); e != nil {
		t.Fatal(e)
	}
	second, _ := ingest.FromURL("https://other.example/2.ts", "US | HBOFHD East", "")
	second.TVGID = "hbo.us"
	if _, e = s.Import([]ingest.Entry{second}, "fixture"); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Candidates(c, false)
	if e != nil || len(rows) != 1 {
		t.Fatalf("ambiguous feed inherited TVG mapping: %d %v", len(rows), e)
	}
}

func TestAllMarketsResolveIndependently(t *testing.T) {
	var matchers []channelMatcher
	for _, preset := range tunerPresets {
		channels, err := preset.channels()
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range channels {
			ch.ProfileID = preset.profile.Preset
			ch.ID = ch.ProfileID + "-" + ch.Number
			matchers = append(matchers, compileChannel(ch))
		}
	}
	for _, tc := range []struct{ label, guide, market, name string }{
		{"US | CNN HD", "cnn.us", "us-nyc", "CNN"},
		{"UK | BBC One London", "bbconelondon.uk", "uk-london", "BBC One London"},
		{"MX | Las Estrellas HD", "lasestrellas.mx", "mx-mexico-city", "Las Estrellas"},
		{"IN | DD News HD", "ddnews.in", "in-mumbai", "DD News"},
		{"DE | RTL HD", "rtl.de", "de-berlin", "RTL"},
		{"NL | RTL 4 HD", "rtl4.nl", "nl-amsterdam", "RTL 4"},
		{"ARG | ESPN 3 HD", "espn3.ar", "ar-buenos-aires", "ESPN3"},
		{"CO | Canal Uno", "", "co-bogota", "Canal Uno"},
		{"AR | Cartoon Network", "", "", ""},
		{"COL | Citytv", "Citytv.ca", "", ""},
		{"DE | RTL HD", "rtl4.nl", "", ""},
		{"UK | CNN HD", "cnn.us", "", ""},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.label, "", tc.guide), tc.guide, nil)
		if got == nil && tc.market == "" {
			continue
		}
		if got == nil || got.channel.ProfileID != tc.market || got.channel.Name != tc.name {
			t.Errorf("%s with %s matched %s, want %s/%s", tc.label, tc.guide, matcherName(got), tc.market, tc.name)
		}
	}
}
