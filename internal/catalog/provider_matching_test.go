package catalog

import (
	"strings"
	"testing"
)

func presetMatchers(t *testing.T) []channelMatcher {
	t.Helper()
	channels, err := DefaultUSChannels()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]channelMatcher, len(channels))
	for i, c := range channels {
		c.ID = c.Number
		out[i] = compileChannel(c)
	}
	return out
}

// All preset aliases must remain unambiguous across the complete lineup.
// Synthetic guide IDs identify the intended service, not a live provider.
func TestEveryPresetAliasAndRegionalExclusion(t *testing.T) {
	ms := presetMatchers(t)
	for _, m := range ms {
		c := m.channel
		guide := c.Name + ".us"
		if c.Station != "" {
			guide = c.Station + ".us"
		}
		for _, alias := range append([]string{c.Name}, c.Aliases...) {
			if strings.HasPrefix(alias, "-") {
				continue
			}
			for _, name := range []string{strings.ToLower(alias), "US | " + alias + " HD"} {
				got := resolveChannel(ms, sourceLabelWithID(name, "", guide), guide, nil)
				if got == nil || got.channel.Number != c.Number {
					t.Errorf("%s alias %q has no unique assignment", c.Name, name)
				}
			}
		}
		for _, tail := range []string{"West", "Pacific", "LatinAmerica"} {
			id := compactIdentity(c.Name) + tail + ".us"
			if got := resolveChannel(ms, sourceLabelWithID(c.Name, "", id), id, nil); got != nil {
				t.Errorf("regional guide admitted: %s / %s", c.Name, id)
			}
		}
	}
}

// Representative provider spellings with all URLs, account data, and provider
// identifiers removed. Accept only a single consistent service identity.
func TestProviderNameAndGuideAgreement(t *testing.T) {
	ms := presetMatchers(t)
	for _, tc := range []struct{ name, guide, number string }{
		{"US - CBS 2 NEW YORK NY (WCBS) FHD", "cbs2wcbs.us", "002"},
		{"US - NBC 4 NEW YORK NY (WNBC) FHD", "nbcwnbc.us", "004"},
		{"US - FOX 5 NEW YORK NY (WNYW) HD", "foxwnyw.us", "005"},
		{"US - ABC 07 NEW YORK NY (WABC) HD", "abcwabc.us", "007"},
		{"US - MS NOW HD [BK]", "msnbc.us", "014"},
		{"US - DISCOVERY INVESTIGATION EAST HD", "investigationdiscovery.us", "023"},
		{"US - ESPN 1 HD", "espn.us", "028"},
		{"VIP - CNN 4K", "cnn.us", "078"},
		{"US - AMERICAN HEROES FHD", "americanheroeschannel.us", "140"},
		{"US - GREAT AMERICAN COUNTRY HD", "gacfamily.us", "295"},
		{"US - CINEMAX MORE MAX EAST HD", "moremax.us", "532"},
		{"US - CINEMAX ACTION MAX EAST HD", "actionmax.us", "533"},
		{"US - CINEMAX 5 STARMAX HD", "5starmax.us", "537"},
		{"US STARZ ENCORE Westerns", "StarzEncoreWesterns.us", "607"},
		{"ESPN UHD", "espn.us", "028"}, {"ESPNUHD", "espn.us", "028"},
		{"ESPNU HD", "espnu.us", "370"},
		{"NBC WNBC", "nbcknbc.us", ""}, {"CBS WCBS", "WBZ.us", ""},
		{"CBSN NEW YORK", "cbs2wcbs.us", ""}, {"CBS 55 New York WLNY", "cbs2wcbs.us", ""},
		{"PBS WLIW New York", "wnet.us", ""}, {"ABC News", "abcwabc.us", ""},
		{"FOX NEWS LIVE NOW", "foxnews.us", ""}, {"NBC E! (East)", "eentertainment.us", ""},
		{"CNN International", "cnn.us", ""}, {"Discovery Family", "discoveryfamilia.us", ""},
		{"ESPN ACC Network", "espn.us", ""}, {"ESPN SEC Network", "espn.us", ""},
		{"COOK'S COUNTRY CHANNEL", "cookingchannel.us", ""}, {"TENNIS TV", "tennischannel.us", ""},
		{"LAT - ESPN HD", "espn.us", ""}, {"EXYU - Discovery", "discovery.us", ""},
		{"##### PARAMOUNT #####", "", ""},
		{"Cinemax East", "cinemaxpacific.us", ""}, {"TNT EAST", "tntpacific.us", ""},
		{"5StarMAX HD", "Cinemax5StarMAXPacific.us", ""},
		{"Cartoon Network HD+", "CartoonNetwork+.in", ""},
		{"HBO Signature HD", "HBOSignatureLatinAmerica.us", ""},
		{"HBO 2 HD", "HBO2LatinAmerica.us", ""},
		{"HBO Hits East HD", "360north.us", ""}, {"Travel Channel", "a3cine.us", ""},
		{"DIY Network", "3abn.latino.houston.tx.us", ""}, {"Hallmark Channel", "360northanchorage.us", ""},
		{"MILITARY CHANNEL", "MilitaryHistoryChannel.us", ""}, {"Starz Edge", "starzencoreespanol.us", ""},
		{"BTN", "SaigonBroadcastingNetwork.us", ""}, {"Travel", "andtv.us", ""},
		{"Hallmark Movies and Mysteries", "Racer.Network.us", ""},
		{"BRAVO", "3ABN.Proclaim.Network.us", ""}, {"Discovery", "CW.Plus.Mountain.us", ""},
		{"CNN", "12345", "078"}, {"NBC", "12345", ""},
	} {
		t.Run(tc.name+"/"+tc.guide, func(t *testing.T) {
			got := resolveChannel(ms, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
			number := ""
			if got != nil {
				number = got.channel.Number
			}
			if number != tc.number {
				t.Fatalf("got %q, want %q", number, tc.number)
			}
		})
	}
}

func TestExplicitGuideCountrySuffix(t *testing.T) {
	for _, tc := range []struct{ id, key, country string }{
		{"CartoonNetwork.in", "cartoonnetwork", "in"}, {"Example.ID", "example", "id"},
		{"Example.IT", "example", "it"}, {"Example.tv", "example", "tv"},
		{"UK.Example.us", "example", "conflict"}, {"STARZ in Black.us", "starz in black", "us"},
		{"12345", "12345", ""},
	} {
		k, c := guideKey(tc.id)
		if k != tc.key || c != tc.country {
			t.Errorf("%s: %q/%q", tc.id, k, c)
		}
	}
}

// Updating the matcher must remove unsafe saved memberships without penalising
// an account or throwing away the unrelated healthy primary/standby pool.
func TestMatchingUpgradeDropsUnsafeMetadataOnly(t *testing.T) {
	for _, tc := range []struct{ name, guide string }{
		{"##### MTV #####", ""}, {"MTV", "MTVPacific.us"}, {"MTV", "MTV.in"}, {"MTV", "360north.us"},
	} {
		t.Run(tc.name+tc.guide, func(t *testing.T) {
			s, ch, feeds := rosterFixture(t, 2)
			channels, err := s.Channels()
			if err != nil {
				t.Fatal(err)
			}
			c := channels[0]
			c.Aliases = append(c.Aliases, "-pacific")
			if _, err = s.SaveChannel(c); err != nil {
				t.Fatal(err)
			}
			if err = s.EnableRosters(RosterPolicy{Target: 2, Race: 2, Preference: "stability"}); err != nil {
				t.Fatal(err)
			}
			for _, f := range feeds {
				if err = s.Record(f.StreamID, Result{Success: true}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.DB.Exec("UPDATE streams SET name=?,tvg_id=? WHERE id=?", tc.name, tc.guide, feeds[0].StreamID); err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec("UPDATE settings SET value='v1-incremental-3' WHERE name='matching_revision'"); err != nil {
				t.Fatal(err)
			}
			if err = s.RefreshMatching(); err != nil {
				t.Fatal(err)
			}
			got, err := s.RosterMembers(ch)
			if err != nil || len(got) != 1 || got[0].StreamID != feeds[1].StreamID {
				t.Fatalf("roster=%+v: %v", got, err)
			}
			var n, strikes int
			if err = s.DB.QueryRow("SELECT count(*),sum(failures) FROM accounts").Scan(&n, &strikes); err != nil || n != 2 || strikes != 0 {
				t.Fatal(n, strikes, err)
			}
		})
	}
}
