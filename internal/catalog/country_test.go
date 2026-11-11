package catalog

import (
	"encoding/json"
	"testing"
)

func TestCountryMetadataAndAliases(t *testing.T) {
	var records map[string][]string
	json.Unmarshal(countryData, &records)
	if len(records) < 249 {
		t.Fatalf("country metadata incomplete: %d", len(records))
	}
	for code, aliases := range records {
		want := code
		if want == "gb" {
			want = "uk"
		}
		for _, alias := range aliases {
			if got := regionCountry(alias); got != want {
				t.Errorf("country %q: %s want %s", alias, got, want)
			}
		}
	}
	for alias, want := range map[string]string{"UK": "uk", "GBR": "uk", "United States of America": "us", "South Korea": "kr", "UAE": "ae", "US East": "us", "no": "no"} {
		if got := regionCountry(alias); got != want {
			t.Errorf("%s: %s want %s", alias, got, want)
		}
	}
}
func TestInternationalCountryMatching(t *testing.T) {
	for _, tc := range []struct{ name, region, key, country string }{
		{"NZ | Example TV HD", "", "example tv", "nz"},
		{"Example TV New Zealand FHD", "", "example tv", "nz"},
		{"BRA Example HD", "", "example", "br"},
		{"Example South Africa", "", "example", "za"},
		{"GBR BBC One HD", "", "bbc one", "uk"},
		{"DE | Example HD", "", "example", "de"},
		{"US MS NOW FHD", "", "msnow", "us"},
		{"US MSNBC FHD", "", "msnow", "us"},
		{"US | The CW HD", "", "the cw", "us"},
		{"CW East | US", "", "cw east", "us"},
		{"US | (The CW) HD", "", "the cw", "us"},
		{"US | [WCBS TV] HD", "", "wcbs tv", "us"},
		{"[US] {The CW} HD", "", "the cw", "us"},
		{"US The CW) HD", "", "the cw", "us"},
		{"[CW] Example HD", "", "example", "cw"},
		{"(CW) Example HD", "", "example", "cw"},
		{"{ CW } Example HD", "", "example", "cw"},
		{"Example HD", "CW", "example", "cw"},
		{"Made in Chelsea", "", "made in chelsea", ""},
		{"It Takes Two", "", "it takes two", ""},
		{"No Limit", "", "no limit", ""},
		{"Example", "Western Sahara", "example", "eh"},
		{"Western Sahara Example HD", "", "example", "eh"},
		{"Example HD", "Brazil", "example", "br"},
	} {
		k, c := channelKey(tc.name, tc.region)
		if k != tc.key || c != tc.country {
			t.Errorf("%q/%q: %s,%s want %s,%s", tc.name, tc.region, k, c, tc.key, tc.country)
		}

	}
	if nameMatches(Channel{Name: "Example", Region: "Brazil"}, "ARG Example HD", "") {
		t.Fatal("different countries merged")
	}
	if !nameMatches(Channel{Name: "Example", Region: "New Zealand"}, "NZ Example HD", "") {
		t.Fatal("country aliases did not match")
	}
}
