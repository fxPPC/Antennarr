package catalog

import (
	"fmt"
	"strings"
	"testing"
)

// The preset is deliberately conservative: aliases may describe the same
// service, but cannot turn a different region, timeshift, or sister channel
// into the London guide entry. Fixtures contain labels only, never accounts.
func londonPresetMatchers(t *testing.T) ([]Channel, []channelMatcher) {
	t.Helper()
	channels, err := DefaultLondonChannels()
	if err != nil {
		t.Fatal(err)
	}
	matchers := make([]channelMatcher, len(channels))
	seenNumbers := map[string]bool{}
	for i := range channels {
		c := &channels[i]
		c.ID = "london-" + c.Number
		c.ProfileID = "uk-london"
		if c.Region != "GB" || !c.Enabled || c.Name == "" || seenNumbers[c.Number] {
			t.Fatalf("invalid London preset entry: %s (%s)", c.Name, c.Number)
		}
		seenNumbers[c.Number] = true
		matchers[i] = compileChannel(*c)
	}
	return channels, matchers
}

func TestLondonPresetAliasesAreUnambiguous(t *testing.T) {
	channels, matchers := londonPresetMatchers(t)
	if len(channels) != 110 {
		t.Fatalf("got %d London channels, want 110", len(channels))
	}
	for i, c := range channels {
		for _, alias := range append([]string{c.Name}, c.Aliases...) {
			if strings.HasPrefix(alias, "-") {
				label := sourceLabelWithID(c.Name+" "+strings.TrimPrefix(alias, "-"), "GB", "")
				if !matchers[i].excludes(label) {
					t.Errorf("%s exclusion %q did not block", c.Name, alias)
				}
				continue
			}
			// A generic BBC/ITV network name needs independent London identity.
			guide := ""
			if c.Station != "" {
				guide = strings.ReplaceAll(c.Name, " ", "") + ".uk"
			}
			for _, decorated := range []string{strings.ToLower(alias), "UK | " + alias + " HD", "[GB] " + alias + " FHD", alias + " | UK", "123 | UK | " + alias + " 1080p"} {
				label := sourceLabelWithID(decorated, "GB", guide)
				got := resolveChannel(matchers, label, guide, nil)
				if got == nil || got.channel.ID != c.ID {
					t.Errorf("%s alias %q resolves to %v", c.Name, decorated, matcherName(got))
				}
			}
			for _, country := range []string{"US", "CA", "IE", "AU", "DE"} {
				label := sourceLabelWithID(country+" | "+alias, "", "")
				if matchers[i].matches(label) {
					t.Errorf("%s accepted country-conflicting alias %q", c.Name, label.key)
				}
			}
		}
	}
}

func matcherName(m *channelMatcher) string {
	if m == nil {
		return "<unassigned>"
	}
	return m.channel.Name
}

func TestLondonProviderLabelRegressions(t *testing.T) {
	_, matchers := londonPresetMatchers(t)
	for _, tc := range []struct{ name, guide, want string }{
		{"UK - BBC ONE LONDON FHD", "bbconelondon.uk", "BBC One London"},
		{"UK - BBC 1 HD", "bbconelondon.uk", "BBC One London"},
		{"UK - BBC 1 HD", "bbc1.uk", ""},
		{"UK - BBC ONE LONDON HD", "bbc1scotland.uk", ""},
		{"UK - BBC ONE SCOTLAND HD", "bbconelondon.uk", ""},
		{"UK - BBC ONE WALES HD", "bbc1wales.uk", ""},
		{"UK - BBC ONE NORTHEN IRELAND HD", "bbc1northernireland.uk", ""},
		{"UK - BBC TWO ENGLAND FHD", "bbc2.uk", "BBC Two"},
		{"UK - BBC TWO WALES HD", "bbc2wales.uk", ""},
		{"UK - BBC4/CBEEBIES HD", "bbc4.uk", ""},
		{"UK - BBC THREE CBBC", "bbc3.uk", ""},
		{"UK - ITV LONDON SD", "itvlondon.uk", "ITV1 London"},
		{"UK - ITV1 FHD", "itvlondon.uk", "ITV1 London"},
		{"UK - ITV1 FHD", "itv1anglia.uk", ""},
		{"UK - ITV1 FHD", "itv1.uk", ""},
		{"UK - ITV LONDON HD", "itv1anglia.uk", ""},
		{"UK - ITV2 FHD", "itv2.uk", "ITV2"},
		{"UK - ITV QUIZ HD", "itvquiz.uk", "ITV Quiz"},
		{"UK - ITVBE HD", "itvbe.uk", ""},
		{"UK - CHANNEL 4 FHD", "channel4.uk", "Channel 4"},
		{"UK - CHANNEL 4 +1", "channel4plus1.uk", ""},
		{"UK - 5 HD", "channel5.uk", "5"},
		{"UK - E4 EXTRA", "e4extra.uk", "E4 Extra"},
		{"UK - SKY ONE FHD", "skyone.uk", "Sky One"},
		{"UK - SKY ONE HD", "skyshowcase.uk", ""},
		{"UK - SKY MAX FHD", "skymax.uk", ""},
		{"UK - SKY SCI-FI HD", "skyscifi.uk", "Sky Sci-Fi"},
		{"UK - SKY CINEMA SCI-FI HD", "skyscifi.uk", ""},
		{"UK - SKY SCI-FI & HOR HD", "skyscifihorror.uk", "Sky Cinema Sci-Fi & Horror"},
		{"UK - SKY CINEMA COMEDY HD", "skycinemacomedy.uk", "Sky Cinema Comedy"},
		{"UK - SKY COMEDY HD", "skycomedy.uk", "Sky Comedy"},
		{"UK - SKY CINEMA SELECT HD", "skygreats.uk", ""},
		{"UK - INVESTIGATION DISCOVERY (ID) HD", "investigationdiscovery.uk", "Investigation Discovery"},
		{"UK - AL JAZEERA EN SD", "aljazeera.uk", "Al Jazeera English"},
		{"UK - 5 DRAMA SD", "5star.uk", ""},
		{"UK - QUEST RED HD", "questredplus1.uk", ""},
		{"UK - DISCOVERY HISTORY FHD", "discoveryhistory.uk", "Discovery History"},
		{"US - DISCOVERY HISTORY HD", "discoveryhistory.uk", ""},
		{"UK - DISCOVERY FHD", "discovery.uk", "Discovery"},
		{"UK - DISCOVERY FHD", "discoveryhistory.uk", ""},
		{"UK - DMAX FHD", "discoverydmax.uk", "DMAX"},
		{"UK - DAVE HD", "dave.uk", "U&Dave"},
		{"UK - GOLD FHD", "gold.uk", "U&Gold"},
		{"UK - U&DRAMA HD", "drama.uk", "U&Drama"},
		{"UK - GOLD TV 24/7", "gold.uk", ""},
		{"UK - QUEST RED HD", "questred.uk", "Quest Red"},
		{"UK - QUEST HD", "questred.uk", ""},
		{"UK - MTV MUSIC HD", "mtvmusic.uk", ""},
		{"UK - SKY SPORTS MAIN EVENT UHD", "skysportsmainevent.uk", "Sky Sports Main Event"},
		{"UK - SKY SPORTS PREMIER LEAGUE FHD", "skysportspremiereleague.uk", "Sky Sports Premier League"},
		{"UK - SKY SPORTS ARENA HD", "4seven.uk", ""},
		{"UK - SKY SPORTS+ HD", "skysportsplus.uk", "Sky Sports+"},
		{"UK - SKY SPORTS+ 12", "skysportsplus.uk", ""},
		{"UK - SKY SPORTS NFL HD", "skysportsaction.uk", "Sky Sports Action"},
		{"UK - TNT SPORTS 1 HD", "tntsports1.uk", "TNT Sports 1"},
		{"UK - TNT SPORTS BOX OFFICE", "tntsports1.uk", ""},
		{"UK - PREMIER SPORTS 1 HD", "premiersports1.uk", "Premier Sports 1"},
		{"IE - PREMIER SPORTS 1 HD", "premiersports1.ie", ""},
		{"UK - BBC NEWS HD", "bbcnews.uk", "BBC News"},
		{"UK - BBC WORLD NEWS HD", "bbcworldnews.uk", ""},
		{"UK - SKY NEWS AUSTRALIA", "skynews.au", ""},
		{"UK - CNBC FHD", "cnbc.uk", "CNBC Europe"},
		{"UK - BLOOMBERG HD", "bloombergtv.uk", "Bloomberg Television Europe"},
		{"UK - CNN INTERNATIONAL HD", "cnninternational.uk", "CNN International"},
		{"UK - CNN HD", "cnn.us", ""},
		{"UK - CARTOON NET HD", "cartoonnetwork.uk", "Cartoon Network"},
		{"UK - NICK JR TOO HD", "nickjrtoo.uk", "Nick Jr. Too"},
		{"UK - NICK JR HD", "nickjrtoo.uk", ""},
	} {
		t.Run(fmt.Sprintf("%s_%s", tc.name, tc.guide), func(t *testing.T) {
			got := resolveChannel(matchers, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("unsafe label matched %s", got.channel.Name)
				}
			} else if got == nil || got.channel.Name != tc.want {
				t.Fatalf("got %s, want %s", matcherName(got), tc.want)
			}
		})
	}
}
