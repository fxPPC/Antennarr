package catalog

import (
	"strings"
	"testing"
)

func europeanPresetMatchers(t *testing.T, id, country string) []channelMatcher {
	t.Helper()
	channels, err := presetChannels(id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var matchers []channelMatcher
	for _, c := range channels {
		if c.Name == "" || c.Number == "" || c.Region != country || !c.Enabled || seen[c.Number] {
			t.Fatalf("invalid channel in %s: %+v", id, c)
		}
		seen[c.Number] = true
		c.ProfileID = id
		c.ID = id + "-" + c.Number
		matchers = append(matchers, compileChannel(c))
	}
	return matchers
}

func TestBerlinAndAmsterdamPresetAliases(t *testing.T) {
	for _, preset := range []struct{ id, country string }{{"de-berlin", "DE"}, {"nl-amsterdam", "NL"}} {
		ms := europeanPresetMatchers(t, preset.id, preset.country)
		for _, m := range ms {
			c := m.channel
			id := compactIdentity(c.Name) + "." + strings.ToLower(c.Region)
			for _, alias := range append([]string{c.Name}, c.Aliases...) {
				if strings.HasPrefix(alias, "-") {
					continue
				}
				name := preset.country + " | " + alias + " FHD"
				got := resolveChannel(ms, sourceLabelWithID(name, "", id), id, nil)
				if got == nil || got.channel.ID != c.ID {
					t.Errorf("%s / %s with %s resolved to %s", preset.id, name, id, matcherName(got))
				}
			}
			for _, other := range ms {
				if other.channel.ID == c.ID {
					continue
				}
				bad := compactIdentity(other.channel.Name) + "." + strings.ToLower(c.Region)
				// Removing a sister service from the tuner must not make its conflicting guide ID acceptable.
				if got := resolveChannel([]channelMatcher{m}, sourceLabelWithID(c.Name, c.Region, bad), bad, nil); got != nil {
					t.Errorf("%s accepted guide for %s", c.Name, other.channel.Name)
				}
			}
		}
	}
}

func TestBerlinAndAmsterdamStationRules(t *testing.T) {
	de := europeanPresetMatchers(t, "de-berlin", "DE")
	nl := europeanPresetMatchers(t, "nl-amsterdam", "NL")
	for _, tc := range []struct{ name, guide, want string }{
		{"DE | RBB Fernsehen HD", "rbbberlin.de", "rbb Berlin"},
		{"rbb Fernsehen Berlin", "", "rbb Berlin"},
		{"DE | rbb", "rbb.de", ""},
		{"DE | rbb Berlin", "rbbbrandenburg.de", ""},
		{"DE | rbb Brandenburg", "rbbberlin.de", ""},
		{"[AT] RTL HD", "rtl.at", ""},
		{"NL | RTL4", "rtl4.nl", "RTL 4"},
		{"DE | RTL HD", "rtl4.nl", ""},
		{"RTL HD", "rtl.de", "RTL"},
		{"DE | RTL HD", "rtlplus1.de", ""},
		{"DE | RTL HD", "rtlnitro.de", ""},
		{"DE | ZDF HD", "zdfneo.de", ""},
		{"DE | ZDF HD", "10457", "ZDF"},
		{"NL | NPO1", "npo1extra.nl", ""},
		{"NL | NPO 2 extra HD", "npo2extra.nl", "NPO 2 extra"},
		{"NL | NH Nieuws", "nh.nl", "NH"},
		{"NL | AT5 Amsterdam HD", "at5.nl", "AT5"},
		{"NL | AT5 Radio", "at5.nl", ""},
		{"US | ESPN", "espn.us", ""},
		{"NL | ESPN 3 1080p", "espn3.nl", "ESPN 3"},
	} {
		t.Run(tc.name+"_"+tc.guide, func(t *testing.T) {
			got := resolveProfileGroups([][]channelMatcher{de, nl}, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
			if tc.want == "" && len(got) != 0 || tc.want != "" && (len(got) != 1 || got[0].channel.Name != tc.want) {
				t.Errorf("got %v match(es), want %q", len(got), tc.want)
			}
		})
	}
}
