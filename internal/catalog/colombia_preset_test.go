package catalog

import "testing"

// Plex offers Claro TV Colombia (DTH) in Bogotá at 110111. Its callsigns
// distinguish national Caracol at 106 (CARA) from Canal Uno at 104 (CAD1HD),
// and national RCN TV at 111 (RCNTVHD) from the radio service at 852 (RCNRAD).
func TestBogotaClaroSatelliteLineup(t *testing.T) {
	matchers := europeanPresetMatchers(t, "co-bogota", "CO")
	cases := []struct {
		number, name, label, guide string
	}{
		{"104", "Canal Uno", "COL - CANAL 1", "Canal.1.(Canal.1.HD).co"},
		{"106", "Caracol Televisión", "CO | Caracol TV Canal 1", "Caracol.(Caracol.HD).co"},
		{"111", "Canal RCN", "COLOMBIA | RCN HD", "RCN.(RCN.HD).co"},
		{"112", "Citytv", "COL | City TV Bogotá", "City.TV.(City.TV).co"},
		{"116", "Canal Capital", "CO | Canal Capital Colombia", "Canal.Capital.(Canal.Capital).co"},
		{"118", "Telecaribe", "CO: TELE CARIBE", "Telecaribe.(Telecaribe).co"},
		{"119", "Telepacífico", "CO: TELE PACIFICO", "Telepacifico.(Telepacifico).co"},
		{"120", "Canal Trece", "COLOMBIA: TRECE", "Canal.Trece.(Canal.Trece).co"},
		{"122", "Telecafé", "COL - TELECAFÉ", "Telecafé.(Telecafe).co"},
		{"123", "Canal TRO", "|COLOMBIA| CANAL TRO", "Canal.TRO.(Canal.Tro).co"},
		{"129", "Señal Colombia", "COL | CANAL SEÑAL COLOMBIA HD", "Señal.Colombia.(Señal.Colombia.HD).co"},
		{"445", "RCN Novelas", "COL | RCN NOVELAS HD", "rcnnovelas.co"},
		{"521", "Win Sports", "COLOMBIA: WIN SPORTS HD", "Win.Sports.(Win.Sports.HD).co"},
	}
	if len(matchers) != len(cases) {
		t.Fatalf("Bogotá has %d stations, want %d", len(matchers), len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveChannel(matchers, sourceLabelWithID(tc.label, "", tc.guide), tc.guide, nil)
			if got == nil || got.channel.Name != tc.name || got.channel.Number != tc.number {
				t.Fatalf("%q with %q resolved to %s, want %s on %s", tc.label, tc.guide, matcherName(got), tc.name, tc.number)
			}
		})
	}
}

func TestBogotaRejectsOtherEditionsAndConflictingGuideIDs(t *testing.T) {
	matchers := europeanPresetMatchers(t, "co-bogota", "CO")
	for _, tc := range []struct{ label, guide string }{
		{"CO | Canal Uno", "caracol.co"},
		{"CO | Caracol TV Canal 1", "canaluno.co"},
		{"CO | Caracol Internacional", "caracol.co"},
		{"CO | Caracol", "caracolinternacional.co"},
		{"CO | Caracol Radio", "caracol.co"},
		{"CO | RCN Radio", "rcn.co"},
		{"CO | RCN", "RCNRAD.co"},
		{"CO | RCN Novelas", "rcn.co"},
		{"CO | RCN", "rcnnovelas.co"},
		{"CO | RCN", "Nuestra.Tele.Noticias.24.(NTN24).pa"},
		{"CO | City TV", "citytv.ca"},
		{"CA | Citytv Toronto", "citytv.co"},
		{"AR | Canal 13", "canaltrece.co"},
		{"CO | Telecafé 2", "telecafé.co"},
		{"CO | Telecaribe Plus", "telecaribe.co"},
		{"CO | Canal TRO", "Canal.TRO.(Canal.Trece).co"},
		{"CO | Canal TRO Plus", "Canal.TRO.(Canal.Tro).co"},
		{"Canal TRO", ""},
		{"CO | Canal Capital +1", "canalcapital.co"},
		{"CO | Win Sports + HD", "Win.Sports+.(Win.Sports.P.HD).co"},
		{"CO | Win Sports", "winsportsplus.co"},
		{"CO | Win Sports Replay", "winsports.co"},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.label, "", tc.guide), tc.guide, nil)
		if got != nil {
			t.Errorf("%q with %q incorrectly matched %s", tc.label, tc.guide, got.channel.Name)
		}
	}
}
