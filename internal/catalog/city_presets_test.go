package catalog

import (
	"strings"
	"testing"
)

// A guide number is unique inside its tuner. Every retained service must accept
// its advertised name, while a shared bare brand cannot pick a country.
func TestCityPresetIdentities(t *testing.T) {
	for _, preset := range tunerPresets {
		channels, err := preset.channels()
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range channels {
			m := compileChannel(ch)
			if !m.strictGuide {
				continue
			}
			_, embeddedCountry := channelKey(ch.Name, "")
			id := compactIdentity(ch.Name) + "." + strings.ToLower(ch.Region)
			countryTag := ch.Region
			if countryTag == "AR" {
				countryTag = "ARG"
			}
			for _, tc := range []struct {
				name, guide string
				want        bool
			}{
				{countryTag + " | " + ch.Name + " HD", "", true},
				{ch.Name, id, true},
				{ch.Name, "", embeddedCountry == strings.ToLower(ch.Region)},
				{ch.Region + " | " + ch.Name, "UnrelatedStation." + strings.ToLower(ch.Region), false},
				{ch.Region + " | " + ch.Name, "foreignservice.us", false},
				{ch.Region + " | " + ch.Name, ".us", false},
				{ch.Region + " | " + ch.Name, "???", false},
				{ch.Region + " | " + ch.Name + " +1", id, false},
			} {
				got := resolveChannel([]channelMatcher{m}, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
				if (got != nil) != tc.want {
					t.Errorf("%s / %s: %q with %q, matched %t", preset.profile.Market, ch.Name, tc.name, tc.guide, got != nil)
				}
			}
		}
	}
}

func TestCityProviderLabels(t *testing.T) {
	for _, tc := range []struct{ preset, label, guide, want string }{
		{"ca-toronto", "CA: CBC Toronto", "", "CBC Toronto"},
		{"ca-toronto", "CA: CTV Toronto", "CBC Vancouver", ""},
		{"es-madrid", "ES - LA 1 HD", "", "La 1"},
		{"es-madrid", "ES - NEOX", "canalcocina.es", ""},
		{"fr-paris", "FR| TF1 FHD", "tf1.fr", "TF1"},
		{"fr-paris", "FR| TF1 FHD", "01tv.fr", ""},
		{"it-rome", "IT - RAI 2 FHD", "rai2.it", "Rai 2"},
		{"it-rome", "IT - RAI 2 HD", "rai3.it", ""},
		{"pl-warsaw", "|PL| TVN 4K", "", "TVN"},
		{"pt-lisbon", "PT| RTP 1 HD", "", "RTP 1"},
		{"pt-lisbon", "PT| RTP Memória", "rtpmadeira.pt", ""},
		{"br-sao-paulo", "BR - GLOBO SP HD", "", "Globo São Paulo"},
		{"br-sao-paulo", "BR - GLOBO RIO HD", "", ""},
		{"se-stockholm", "SE: SVT 1", "", "SVT1"},
		{"dk-copenhagen", "DK| TV2 NEWS HD", "", "TV2 News"},
		{"dk-copenhagen", "DK| TV3+ HD", "", ""},
		{"no-oslo", "NO| NRK 1 HD", "", "NRK1"},
		{"no-oslo", "NO| SF-KANALEN", "matkanalen.no", ""},
	} {
		p, err := presetProfile(tc.preset)
		if err != nil {
			t.Fatal(err)
		}
		ms := europeanPresetMatchers(t, tc.preset, p.Country)
		got := resolveChannel(ms, sourceLabelWithID(tc.label, "", tc.guide), tc.guide, nil)
		if (got == nil) != (tc.want == "") || got != nil && got.channel.Name != tc.want {
			t.Errorf("%s: %q with %q gave %s, want %q", tc.preset, tc.label, tc.guide, matcherName(got), tc.want)
		}
	}
}

func TestDelimitedTagsPreserveBrandWords(t *testing.T) {
	for _, tc := range []struct{ label, key, country string }{
		{"ES - La Sexta", "la sexta", "es"},
		{"LA - Example HD", "example", "la"},
		{"IT - Sky Uno HD", "sky uno", "it"},
		{"NO| NRK 1", "nrk 1", "no"},
		{"No Comment", "no comment", ""},
		{"It Takes Two", "it takes two", ""},
		{"In-N-Out", "in n out", ""},
	} {
		key, country := channelKey(tc.label, "")
		if key != tc.key || country != tc.country {
			t.Errorf("%q gave %q/%q, want %q/%q", tc.label, key, country, tc.key, tc.country)
		}
	}
}
