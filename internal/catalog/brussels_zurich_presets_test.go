package catalog

import "testing"

func TestBrusselsAndZurichOptionalLineups(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, tc := range []struct {
		id, postcode, timezone, first, last, lastNumber string
		count                                           int
	}{
		{"be-brussels", "1000", "Europe/Brussels", "La Une", "Ketnet", "262", 16},
		{"ch-zurich", "8001", "Europe/Zurich", "SRF 1", "Tele Ticino", "503", 16},
	} {
		profile, err := s.AddProfilePreset(tc.id, 5)
		if err != nil {
			t.Fatal(err)
		}
		if profile.Enabled || profile.GuideLocation != tc.postcode || profile.Timezone != tc.timezone {
			t.Errorf("%s has unexpected defaults: %+v", tc.id, profile)
		}
		channels, err := s.ChannelsForProfile(profile.ID)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, ch := range channels {
			seen[ch.Number] = ch.Name
		}
		if len(seen) != tc.count || seen["001"] != tc.first || seen[tc.lastNumber] != tc.last {
			t.Errorf("%s guide numbers: %v", tc.id, seen)
		}
	}
}

func TestBrusselsAndZurichProviderRules(t *testing.T) {
	matchers := append(europeanPresetMatchers(t, "be-brussels", "BE"), europeanPresetMatchers(t, "ch-zurich", "CH")...)
	for _, tc := range []struct{ label, guide, want string }{
		{"BE: RTBF La Une FHD", "LaUne.be", "La Une"},
		{"BE | RTL Plug", "plugrtl.be", "PLUG RTL"},
		{"BEL | Club RTL HD", "rtlclub.be", "RTL Club"},
		{"[BE] Eén HD", "", "VRT 1"},
		{"BE - VTM2", "VTM2.be", "VTM 2"},
		{"FR | La Une", "LaUne.be", ""},
		{"NL | VRT Canvas", "vrtcanvas.nl", ""},
		{"BE | VTM 2", "VTM3.be", ""},
		{"BE | Ketnet +1", "ketnet.be", ""},
		{"BE| VTM GOLD", "", ""},
		{"Be Happy", "", ""},
		{"La Une", "", ""},
		{"SWS: TELE ZURI", "", "TeleZüri"},
		{"|SUI| TeleZüri", "TeleZueri.ch", "TeleZüri"},
		{"CH | Tele Basel", "telebasel.ch", "Telebasel"},
		{"SWISS | Swiss 1", "", "Swiss 1"},
		{"Swiss 1", "", ""},
		{"|SW| SRF ZWEI HD", "srf2.ch", "SRF Zwei"},
		{"SW: SRF Zwei", "", ""},
		{"SW: SRF Zwei", "srf2.se", ""},
		{"SH| SRF ZWEI HD", "srfzwei.ch", "SRF Zwei"},
		{"SH| SRF ZWEI HD", "", ""},
		{"SH| SRF ZWEI HD", "srfinfo.ch", ""},
		{"AT: SRF ZWEI", "srfzwei.ch", ""},
		{"CH | RTS 1", "RTSDeux.ch", ""},
		{"CH | blue SPORT 1", "blueSport2.ch", ""},
		{"SWS: blue Sport Event 1", "", ""},
		{"CH | RSI La 1 replay", "RSILa1.ch", ""},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.label, "", tc.guide), tc.guide, nil)
		if tc.want == "" && got != nil || tc.want != "" && (got == nil || got.channel.Name != tc.want) {
			t.Errorf("%q with guide %q matched %s, want %q", tc.label, tc.guide, matcherName(got), tc.want)
		}
	}
}

func TestBeAtLabelBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, key, country string }{
		{"Be Happy", "be happy", ""},
		{"Let It Be", "let it be", ""},
		{"BE | La Une", "la une", "be"},
		{"La Une | BE", "la une", "be"},
		{"[BE] Tipik", "tipik", "be"},
	} {
		if key, country := channelKey(tc.name, ""); key != tc.key || country != tc.country {
			t.Errorf("%q yielded %q/%q, want %q/%q", tc.name, key, country, tc.key, tc.country)
		}
	}
}
