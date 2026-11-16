package catalog

import (
	"antennarr/internal/ingest"
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
		{"UK - SYFY HD", "syfy.uk", "Sky Sci-Fi"},
		{"US - SYFY HD", "syfy.us", ""},
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
		{"UK - RACING UK FHD", "racinguk.uk", "Racing TV"},
		{"UK - RACINGUK HD", "racinguk.uk", "Racing TV"},
		{"UK - RACING HD", "racinguk.uk", ""},
		{"UK - RACING TV EXTRA HD", "racingtv.uk", ""},
		{"UK - RACING UK HD", "skysportsracing.uk", ""},
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

// A plausible name must not override a different recognised programme service.
// Run the whole preset cross-product, including when the other service is not
// selected by the user: removing a channel must not weaken identity checks.
func TestLondonPresetRejectsOtherServicesGuideIDs(t *testing.T) {
	channels, matchers := londonPresetMatchers(t)
	for i, channel := range channels {
		for j, other := range channels {
			if i == j {
				continue
			}
			guide := compactIdentity(other.Name) + ".uk"
			label := sourceLabelWithID("UK | "+channel.Name+" HD", "", guide)
			for _, selected := range [][]channelMatcher{matchers, {matchers[i]}} {
				if got := resolveChannel(selected, label, guide, nil); got != nil {
					t.Errorf("%s accepted %s guide with %d selected channels", channel.Name, other.Name, len(selected))
				}
			}
		}
	}
}

func TestLondonRegionalAndTimeshiftConflicts(t *testing.T) {
	_, matchers := londonPresetMatchers(t)
	for _, tc := range []struct{ name, guide string }{
		{"BBC One London", "bbcnorthwest.uk"},
		{"BBC One London", "bbconecambridgeshire.uk"},
		{"BBC One London", "bbcyourkshirelincolnshire.uk"},
		{"BBC One London", "bbcone.northwest.uk"},
		{"BBC One London", "bbconelondonplus1.uk"},
		{"BBC One London", "bbconelondonplus24.uk"},
		{"BBC One", "london.uk"},
		{"ITV1", "london.uk"},
		{"ITV1 London", "itv1tynetees.uk"},
		{"ITV1 London", "itvlondonplus1.uk"},
		{"ITV1 London", "itv1westcountryhd.uk"},
		{"BBC One London", "bbc1unknownregion.uk"},
		{"ITV1 London", "itv1unknownregion.uk"},
		{"U&Dave", "davejavu.uk"},
		{"GREAT! Action", "greatmovies.uk"},
		{"Legend", "legendxtraplus1.uk"},
		{"Sky Sports Main Event", "skysportsmaineventplus1.uk"},
	} {
		t.Run(tc.name+"_"+tc.guide, func(t *testing.T) {
			label := sourceLabelWithID("UK | "+tc.name+" HD", "", tc.guide)
			if got := resolveChannel(matchers, label, tc.guide, nil); got != nil {
				t.Fatalf("conflicting or insufficient identity accepted as %s", got.channel.Name)
			}
		})
	}
}

func TestLondonStationNameAndGuideEvidence(t *testing.T) {
	_, matchers := londonPresetMatchers(t)
	for _, tc := range []struct{ name, guide, want string }{
		{"BBC1 London", "", "BBC One London"},
		{"BBCOneLondon", "", "BBC One London"},
		{"ITV1London", "", "ITV1 London"},
		{"BBC One London", "123456", "BBC One London"},
		{"BBC One London", "bbc1.uk", "BBC One London"},
		{"ITV1 London", "itv1.uk", "ITV1 London"},
		{"BBC One", "bbc.one.london.uk", "BBC One London"},
		{"ITV1", "itv.london.uk", "ITV1 London"},
		{"BBC One", "123456", ""},
		{"ITV1", "123456", ""},
	} {
		t.Run(tc.name+"_"+tc.guide, func(t *testing.T) {
			got := resolveChannel(matchers, sourceLabelWithID("UK | "+tc.name, "", tc.guide), tc.guide, nil)
			if (tc.want == "" && got != nil) || (tc.want != "" && (got == nil || got.channel.Name != tc.want)) {
				t.Fatalf("got %s, want %q", matcherName(got), tc.want)
			}
		})
	}
}

func TestExplicitTimeshiftChannelsRemainSelectable(t *testing.T) {
	for _, name := range []string{"Legend Xtra +1", "BBC One London +1", "Example TV +24"} {
		c := Channel{ID: "delayed", Name: name, Region: "GB", Enabled: true}
		guide := compactIdentity(name) + ".uk"
		got := resolveChannel([]channelMatcher{compileChannel(c)}, sourceLabelWithID("UK | "+name, "", guide), guide, nil)
		if got == nil {
			t.Fatalf("explicit delayed channel %q rejected", name)
		}
	}
}

// The revision refresh must repair old manual/learned assignments as well as
// future imports. Correct rosters and account health are not collateral damage.
func TestLondonMatchingRefreshRepairsSavedRosters(t *testing.T) {
	s, usChannel, usFeeds := rosterFixture(t, 1)
	addTestProfile(t, s, "gb", "GB")
	if err := s.EnableRosters(RosterPolicy{Target: 3, Race: 2, Preference: "stability"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(usFeeds[0].StreamID, Result{Success: true}); err != nil {
		t.Fatal(err)
	}
	channels, _ := londonPresetMatchers(t)
	for _, tc := range []struct{ name, goodGuide, badGuide string }{
		{"BBC One London", "bbconelondon.uk", "bbcnorthwest.uk"},
		{"GREAT! Action", "greataction.uk", "greatmovies.uk"},
		{"Legend", "legend.uk", "legendxtraplus1.uk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var channel Channel
			for _, c := range channels {
				if c.Name == tc.name {
					channel = c
				}
			}
			channel.ID, channel.ProfileID = "", "gb"
			var err error
			channel.ID, err = s.SaveChannel(channel)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				entry, err := ingest.FromURL(fmt.Sprintf("https://uk-%s-%d.fixture.invalid/stream.ts", channel.Number, i), "UK | "+tc.name, "")
				if err != nil {
					t.Fatal(err)
				}
				entry.TVGID = tc.goodGuide
				if _, err = s.Import([]ingest.Entry{entry}, "synthetic"); err != nil {
					t.Fatal(err)
				}
			}
			feeds, err := s.Candidates(channel.ID, false)
			if err != nil || len(feeds) != 2 {
				t.Fatalf("candidate setup: %d %v", len(feeds), err)
			}
			for _, feed := range feeds {
				if err := s.Record(feed.StreamID, Result{Success: true}); err != nil {
					t.Fatal(err)
				}
			}
			wrong := feeds[0].StreamID
			if _, err = s.DB.Exec("UPDATE streams SET tvg_id=? WHERE id=?", tc.badGuide, wrong); err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec("UPDATE mappings SET origin='manual' WHERE stream_id=?", wrong); err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec("UPDATE settings SET value='v1.1-profiles-2' WHERE name='matching_revision'"); err != nil {
				t.Fatal(err)
			}
			if err = s.RefreshMatching(); err != nil {
				t.Fatal(err)
			}
			roster, err := s.RosterMembers(channel.ID)
			if err != nil || len(roster) != 1 || roster[0].StreamID != feeds[1].StreamID {
				t.Fatalf("unsafe or lost roster after refresh: %+v %v", roster, err)
			}
			if err = s.Map(channel.ID, wrong, true); err == nil {
				t.Fatal("manual mapping bypassed identity rules")
			}
			if err = s.RefreshMatching(); err != nil {
				t.Fatal(err)
			}
			var events int
			if err = s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='roster_removed' AND source_id=?", wrong).Scan(&events); err != nil || events != 1 {
				t.Fatal("missing/duplicate removal event", events, err)
			}
		})
	}
	roster, err := s.RosterMembers(usChannel)
	if err != nil || len(roster) != 1 || roster[0].StreamID != usFeeds[0].StreamID {
		t.Fatal("US roster changed", roster, err)
	}
	var accounts, strikes int
	if err = s.DB.QueryRow("SELECT count(*),sum(failures) FROM accounts").Scan(&accounts, &strikes); err != nil || accounts != 7 || strikes != 0 {
		t.Fatal("identity repair changed account health", accounts, strikes, err)
	}
}
