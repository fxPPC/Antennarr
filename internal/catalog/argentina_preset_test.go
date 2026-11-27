package catalog

import "testing"

func TestBuenosAiresGuideNumbersAndOrigin(t *testing.T) {
	matchers := europeanPresetMatchers(t, "ar-buenos-aires", "AR")
	// Cablevisión – Capital Federal, postcode C1064. The guide distinguishes
	// ESPN3 (104) from the other ESPN services at 102, 103, 105, and 123.
	guide := map[string]string{
		"009": "América TV", "010": "Telefe", "011": "TV Pública",
		"015": "América 24", "016": "C5N", "017": "Crónica TV",
		"018": "Canal 26", "022": "Encuentro", "100": "DeporTV",
		"104": "ESPN3", "123": "ESPN Premium Argentina",
		"203": "Cartoon Network", "303": "Volver",
	}
	if len(matchers) != len(guide) {
		t.Fatalf("Buenos Aires has %d channels, want %d", len(matchers), len(guide))
	}
	for _, m := range matchers {
		if want := guide[m.channel.Number]; m.channel.Name != want {
			t.Fatalf("guide number %s has %q, want %q", m.channel.Number, m.channel.Name, want)
		}
		name := m.channel.Name
		if m.channel.Number == "123" {
			name = "ESPN Premium"
		}
		for _, tc := range []struct {
			label, region, guideID string
			match                  bool
		}{
			{"ARG | " + name, "", "", true},
			{"AR | " + name, "", "", false},
			{name, "Argentina", "", true},
			{"AR | " + name, "", compactIdentity(name) + ".ar", true},
			{"Argentina | " + name, "", "anotherchannel.ar", false},
			{"ARG | " + name + " Internacional", "", "", false},
		} {
			got := resolveChannel(matchers, sourceLabelWithID(tc.label, tc.region, tc.guideID), tc.guideID, nil)
			if (got != nil) != tc.match || got != nil && got.channel.Number != m.channel.Number {
				t.Errorf("%q with region %q and guide %q resolved to %s; expected match=%t for %s",
					tc.label, tc.region, tc.guideID, matcherName(got), tc.match, m.channel.Name)
			}
		}
	}
}

func TestBuenosAiresProviderLabels(t *testing.T) {
	matchers := europeanPresetMatchers(t, "ar-buenos-aires", "AR")
	for _, tc := range []struct{ label, guide, number string }{
		{"ARG: America TV HD", "", "009"},
		{"ARG: TV PUBLICA", "", "011"},
		{"Argentina | A24", "", "015"},
		{"ARG: Cronica", "", "017"},
		{"ARG | ESPN 3 HD", "", "104"},
		{"AR | ESPN Premium", "ESPNPremium.ar", "123"},
		{"AR: Cartoon Network", "CartoonNetwork.ar", "203"},
		{"[AR] Cartoon Network", "", ""},
		{"AR | MBC 1", "", ""},
		{"US | Cartoon Network", "CartoonNetwork.ar", ""},
		{"ARG | Cartoon Network", "CartoonNetwork.us", ""},
		{"ARG | Cartoon Network MENA", "CartoonNetwork.ar", ""},
		{"ARG | ESPN 3", "ESPN2.ar", ""},
		{"ARG | Telefe Internacional", "Telefe.ar", ""},
		{"ARG | Telefe +1", "Telefe.ar", ""},
		{"ARG | América TV Perú", "AmericaTV.ar", ""},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.label, "", tc.guide), tc.guide, nil)
		number := ""
		if got != nil {
			number = got.channel.Number
		}
		if number != tc.number {
			t.Errorf("%q with guide %q resolved to %q, want %q", tc.label, tc.guide, number, tc.number)
		}
	}
}
