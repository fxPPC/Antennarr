package catalog

import (
	"embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/us-manhattan-*.json
var presetFixtures embed.FS

func TestUSManhattanPresetAliases(t *testing.T) {
	for _, file := range []string{"us-manhattan-88.json", "us-manhattan-expanded.json"} {
		t.Run(file, func(t *testing.T) { testManhattanPreset(t, file) })
	}
}
func testManhattanPreset(t *testing.T, file string) {
	b, e := presetFixtures.ReadFile("testdata/" + file)
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		Channels []Channel `json:"channels"`
	}

	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	matchers := make([]channelMatcher, len(p.Channels))
	for i, c := range p.Channels {
		matchers[i] = compileChannel(c)
	}
	for _, ambiguous := range []string{"HBOFHD"} {
		for _, m := range matchers {
			if m.matches(parseSourceLabel(ambiguous, "US")) {
				t.Errorf("ambiguous label %q matched %s", ambiguous, m.channel.Name)
			}
		}
	}
	for i, c := range p.Channels {
		if len(c.Aliases) > 200 {
			t.Errorf("too many rules %s", c.Name)
		}
		for _, a := range append([]string{c.Name}, c.Aliases...) {
			if strings.HasPrefix(a, "-") {
				if !matchers[i].excludes(parseSourceLabel(c.Name+" "+strings.TrimPrefix(a, "-"), "US")) {
					t.Errorf("%s exclusion %q did not block", c.Name, a)
				}
				continue
			}
			for _, decorated := range []string{strings.ToLower(a), "US | " + a + " HD", "[US] " + a + " FHD", a + " | US", "123 | US | " + a + " 1080p"} {
				label := parseSourceLabel(decorated, "US")
				if !matchers[i].matches(label) {
					t.Errorf("%s rejects decorated alias %q", c.Name, decorated)
				}
				for j, other := range p.Channels {
					if j != i && matchers[j].matches(label) {
						t.Errorf("%s decorated alias %q matches %s", c.Name, decorated, other.Name)
					}
				}
			}
			for _, foreign := range []string{"UK | " + a, "CA | " + a, "[IN] " + a} {
				if matchers[i].matches(parseSourceLabel(foreign, "")) {
					t.Errorf("%s accepts foreign label %q", c.Name, foreign)
				}
			}
			label := parseSourceLabel(a, "US")
			if !matchers[i].matches(label) {
				t.Errorf("%s rejects alias %q", c.Name, a)
			}
			for j, other := range p.Channels {
				if other.Name != c.Name && matchers[j].matches(label) {
					t.Errorf("%s alias %q also matches %s", c.Name, a, other.Name)
				}
			}
		}
		for _, attached := range []string{c.Name + "HD", c.Name + "FHD", c.Name + "1080p"} {
			if attached == "ESPNUHD" || attached == "HBOFHD" {
				continue
			} // ESPNUHD explicitly means ESPN UHD, not ESPNU HD; HBOFHD stays ambiguous.
			if !matchers[i].matches(parseSourceLabel(attached, "US")) {
				t.Errorf("%s rejects attached quality %q", c.Name, attached)
			}
		}
		for _, bad := range []string{c.Name + " West", c.Name + " Pacific", c.Name + " On Demand"} {
			if nameMatches(c, bad, "US") {
				t.Errorf("%s accepts %s", c.Name, bad)
			}
		}
	}
}
