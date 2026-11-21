package catalog

import "testing"

func TestSourceCountryEvidenceAgrees(t *testing.T) {
	target := compileChannel(Channel{Name: "CNN", Region: "US"})
	for _, tc := range []struct {
		name, region string
		want         bool
	}{
		{"US US | CNN HD US", "USA", true},
		{"US US | GB | CNN HD", "", false},
		{"US | CNN HD", "GB", false},
		{"CNN HD", "US", true},
	} {
		if got := target.matches(parseSourceLabel(tc.name, tc.region)); got != tc.want {
			t.Errorf("match %q with region %q = %v, want %v", tc.name, tc.region, got, tc.want)
		}
	}
}

func TestLondonRecognises5SelectRenameWithoutSisterOrDelayedFeeds(t *testing.T) {
	_, channels := londonPresetMatchers(t)
	// An upgraded lineup can still contain its original 5DRAMA definition.
	channels = append(channels, compileChannel(Channel{ID: "saved-drama", Name: "5DRAMA", Region: "GB", Aliases: []string{"5 Drama", "Five Drama", "5SELECT", "5 Select", "Five Select", "-+1", "-ireland"}}))
	for _, tc := range []struct {
		name, guide string
		want        bool
	}{
		{"UK: 5SELECT HD", "5Drama.uk", true},
		{"5 Select", "5Select.uk", true},
		{"Five Select", "", true},
		{"UK 5Drama", "5Select.uk", true},
		{"UK 5Select", "5STAR.uk", false},
		{"UK 5ACTION", "5Drama.uk", false},
		{"UK 5SELECT +1", "5DRAMA.uk", false},
		{"UK 5DRAMA", "5SelectPlus1.uk", false},
		{"5DRAMA", "5Select.ie", false},
	} {
		got := resolveChannel(channels, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
		if (got != nil && got.channel.Name == "5DRAMA") != tc.want {
			t.Errorf("%q, %q: matched %s", tc.name, tc.guide, matcherName(got))
		}
	}
}
