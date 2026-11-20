package catalog

import (
	"strings"
	"testing"
)

func TestMexicoCityAndMumbaiPresetIdentities(t *testing.T) {
	var matchers []channelMatcher
	for _, market := range []struct{ preset, region string }{
		{"mx-mexico-city", "MX"}, {"in-mumbai", "IN"},
	} {
		channels, err := presetChannels(market.preset)
		if err != nil || len(channels) == 0 {
			t.Fatalf("%s: missing preset (%v)", market.preset, err)
		}
		used := map[string]bool{}
		for _, c := range channels {
			if !c.Enabled || c.Region != market.region || c.Name == "" || c.Number == "" || used[c.Number] {
				t.Fatalf("%s: invalid channel %+v", market.preset, c)
			}
			used[c.Number] = true
			c.ProfileID = market.preset
			c.ID = c.ProfileID + "-" + c.Number
			matchers = append(matchers, compileChannel(c))
		}
	}

	for _, m := range matchers {
		for _, alias := range append([]string{m.channel.Name}, m.channel.Aliases...) {
			if strings.HasPrefix(alias, "-") {
				continue
			}
			name := "[" + m.channel.Region + "] " + alias + " HD"
			got := resolveChannel(matchers, sourceLabelWithID(name, "", ""), "", nil)
			if got == nil || got.channel.ID != m.channel.ID {
				t.Errorf("%q: got %s, want %s", name, matcherName(got), m.channel.Name)
			}
		}
	}
	// Removing a sister service from the lineup must not make its guide ID
	// acceptable for a different service with a convincing display name.
	for _, target := range matchers {
		for _, other := range matchers {
			if target.channel.ID == other.channel.ID || target.channel.Region != other.channel.Region {
				continue
			}
			guide := compactIdentity(other.channel.Name) + "." + strings.ToLower(other.channel.Region)
			label := sourceLabelWithID(target.channel.Name, target.channel.Region, guide)
			if got := resolveChannel([]channelMatcher{target}, label, guide, nil); got != nil {
				t.Errorf("%s accepted conflicting guide %s", target.channel.Name, guide)
			}
		}
	}
	for _, tc := range []struct{ name, guide, want string }{
		{"[MX] Canal Cinco FHD", "XHGC.mx", "Canal 5"},
		{"[MX] Canal Cinco", "Canal5.uy", ""},
		{"[MX] Las Estrellas Internacional", "LasEstrellas.mx", ""},
		{"[MX] Las Estrellas", "Canal5.mx", ""},
		{"[MX] Once Ninas y Ninos", "OnceNiñasyNiños.mx", "Once Niñas y Niños"},
		{"[MX] Canal Once", "OnceNinasyNinos.mx", ""},
		{"[MX] Canal 22 Internacional", "Canal22.mx", ""},
		{"MX - NU9VE TV FHD", "", "NU9VE"},
		{"HINDI - DD NEWS", "ddnews.in", "DD News"},
		{"SPORTS - DD SPORTS FHD", "ddsports.in", "DD Sports"},
		{"HINDI - NEWS | ZEE NEWS", "", "Zee News"},
		{"HINDI - NEWS | ZEE NEWS BIHAR", "", ""},
		{"MARATHI - TV9 MARATHI", "", "TV9 Marathi"},
		{"HINDI - DD NATIONAL", "sports18.in", ""},
		{"HINDI - B4U MOVIES USA", "", ""},
		{"HINDI - B4U MOVIES", "b4umovies.uk", ""},
		{"HINDI - DD NEWS", "ddnews.pk", ""},
		{"HINDI - Canal Cinco", "", ""},
		{"[IN] दूरदर्शन सह्याद्री HD", "DDSahyadri.in", "DD Sahyadri"},
		{"[IN] TV 9 Marathi FHD", "tv9marathi.in", "TV9 Marathi"},
		{"[IN] TV9 Telugu HD", "tv9marathi.in", ""},
		{"[IN] DD Sports", "DDNews.in", ""},
		{"[IN] Star Utsav Movies", "StarUtsavMovies.in", "Star Utsav Movies"},
		{"[IN] Star Utsav", "StarUtsavMovies.in", ""},
		{"[IN] Sansad TV2", "SansadTV1.in", ""},
		{"[IN] Aaj Tak +1", "AajTak.in", ""},
		{"[GB] B4U Movies", "B4UMovies.uk", ""},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
		if got == nil && tc.want != "" || got != nil && got.channel.Name != tc.want {
			t.Errorf("%q (%s): got %s, want %q", tc.name, tc.guide, matcherName(got), tc.want)
		}
	}
}
