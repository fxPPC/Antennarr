package catalog

import (
	"antennarr/internal/ingest"
	"encoding/json"
	"reflect"
	"testing"
)

func TestStationIdentityAndUnlearnedGuideConflicts(t *testing.T) {
	c := Channel{ID: "nbc", Name: "NBC", Number: "004", Region: "US", Enabled: true, Station: "WNBC", Aliases: []string{"WNBC", "NBC New York", "WNBC NBC", "NBC WNBC", "-news", "-cozi"}}
	m := compileChannel(c)
	for _, tc := range []struct {
		name, id string
		want     bool
	}{
		{"US NBC HD", "", false}, {"NBC New York", "", false},
		{"US NBC HD", "cnbc.us", false}, {"WNBC", "cnbc.us", false},
		{"WNBC", "nbcnewsnow.us", false},
		{"US WNBC HD", "", true}, {"NBC WNBC", "", true},
		{"US NBC HD", "nbcwnbc.us", true}, {"NBC", "WNBC.US", true},
		{"NBC", "nbcknbc.us", false}, {"NBC Los Angeles KNBC", "", false},
		{"NBC", "WNBC2.us", false}, {"NBC", "wnbc.ca", false},
		{"UK WNBC HD", "wnbc.us", false}, {"WNBC Cozi TV", "wnbc.us", false},
		{"NBC News Now", "wnbc.us", false},
	} {
		t.Run(tc.name+"/"+tc.id, func(t *testing.T) {
			got := resolveChannel([]channelMatcher{m}, sourceLabelWithID(tc.name, "", tc.id), tc.id, nil)
			if (got != nil) != tc.want {
				t.Fatalf("matched=%v, want %v", got != nil, tc.want)
			}
		})
	}
	// The conflict safeguard also applies to channels without a station lock.
	c.Station = ""
	m = compileChannel(c)
	for _, id := range []string{"CNBC.US", "cnbc", "foxnews.us"} {
		if resolveChannel([]channelMatcher{m}, sourceLabelWithID("NBC", "", id), id, nil) != nil {
			t.Errorf("unlearned contradictory ID %q admitted", id)
		}
	}
	if resolveChannel([]channelMatcher{m}, sourceLabelWithID("NBC", "", "12345"), "12345", nil) == nil {
		t.Fatal("opaque provider ID rejected")
	}
}

func TestStationEditRemovesWrongRosterWithoutStrikes(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := Channel{ID: "nbc", Name: "NBC", Number: "004", Region: "US", Enabled: true, Aliases: []string{"WNBC", "NBC New York"}}
	if _, err = s.SaveChannel(c); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableRosters(RosterPolicy{Target: 5, Race: 5, Preference: "stability"}); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ name, id string }{{"NBC New York", ""}, {"NBC", "nbcwnbc.us"}, {"WNBC", ""}} {
		e, err := ingest.FromURL("https://fixture"+string(rune('a'+i))+".example/stream.ts", tc.name, "")
		if err != nil {
			t.Fatal(err)
		}
		e.TVGID = tc.id
		if _, err = s.Import([]ingest.Entry{e}, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := s.Candidates(c.ID, false)
	if err != nil || len(candidates) != 3 {
		t.Fatalf("candidates %v %v", candidates, err)
	}
	var wrong int64
	if err = s.DB.QueryRow("SELECT id FROM streams WHERE name='NBC New York'").Scan(&wrong); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if err = s.Record(candidate.StreamID, Result{Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Map(c.ID, wrong, true); err != nil {
		t.Fatal(err)
	} // old manual mapping must also be removed
	c.Station = " wnbc "
	if _, err = s.SaveChannel(c); err != nil {
		t.Fatal(err)
	}
	members, err := s.RosterMembers(c.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("roster=%v %v", members, err)
	}
	for _, member := range members {
		if member.StreamID == wrong {
			t.Fatal("unsafe backup retained")
		}
	}
	if err = s.Map(c.ID, wrong, true); err == nil {
		t.Fatal("manual map bypasses station rule")
	}
	var accounts, strikes int
	if err = s.DB.QueryRow("SELECT count(*),sum(failures) FROM accounts").Scan(&accounts, &strikes); err != nil || accounts != 3 || strikes != 0 {
		t.Fatalf("accounts=%d strikes=%d %v", accounts, strikes, err)
	}
	// Portable export and restore include the constraint, without any streams.
	lineup, err := s.ExportLineup()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(lineup)
	if err != nil {
		t.Fatal(err)
	}
	var imported Lineup
	if err = json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Channels[0].Station != "WNBC" {
		t.Fatal("station missing from export")
	}
	if err = s.ImportLineup(imported); err != nil {
		t.Fatal(err)
	}
	restored, err := s.ExportLineup()
	if err != nil || !reflect.DeepEqual(lineup, restored) {
		t.Fatal("station lost in restore", err)
	}
	if err = s.DeleteChannel(c.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM settings WHERE name GLOB 'channel_station:*'").Scan(&n); err != nil || n != 0 {
		t.Fatal("orphan station setting", n, err)
	}
}

func TestFreshBroadcastStationDefaultsAndReset(t *testing.T) {
	s, err := OpenWithUSDefaults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for pass := 0; pass < 3; pass++ {
		channels, err := s.ChannelsForProfile("primary")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range channels {
			if c.Station != "" {
				n++
			}
		}
		if n != 7 {
			t.Fatalf("pass %d: %d station constraints", pass, n)
		}
		if pass == 0 {
			_, err = s.ApplyUSPreset()
		} else {
			err = s.ReplaceChannels(true)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = s.ReplaceChannels(false); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM settings WHERE name GLOB 'channel_station:*'").Scan(&n); err != nil || n != 2 {
		t.Fatal("bulk reset must preserve London station settings", n, err)
	}
}

func TestStationGuideCombinesKnownNetworkAliases(t *testing.T) {
	channels, err := DefaultUSChannels()
	if err != nil {
		t.Fatal(err)
	}
	var matchers []channelMatcher
	for _, c := range channels {
		c.ID = c.Number
		matchers = append(matchers, compileChannel(c))
	}
	for _, tc := range []struct{ name, guide, number string }{
		{"US - MY 9 HD", "my9wwor.us", "009"},
		{"US NBC HD", "nbcwnbc.us", "004"},
		{"The CW HD", "cwwpix.us", "011"},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
		if got == nil || got.channel.Number != tc.number {
			t.Fatalf("%s / %s: wrong assignment %+v", tc.name, tc.guide, got)
		}
	}
}

// An upgrade must repair already verified memberships, including explicit maps
// created by an older build. Rejecting only new catalogue entries is insufficient.
func TestMatchingUpgradeEvictsContradictoryGuideWithoutStrikes(t *testing.T) {
	s, ch, feeds := rosterFixture(t, 2)
	if err := s.EnableRosters(RosterPolicy{Target: 2, Race: 2, Preference: "stability"}); err != nil {
		t.Fatal(err)
	}
	for _, feed := range feeds {
		if err := s.Record(feed.StreamID, Result{Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	wrong := feeds[0].StreamID
	// Simulate a pre-fix saved MTV feed advertising a different recognised service.
	if _, err := s.DB.Exec("UPDATE streams SET tvg_id='cnbc.us' WHERE id=?", wrong); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE mappings SET origin='manual' WHERE stream_id=?", wrong); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("INSERT INTO settings(name,value) VALUES('matching_revision','v1-incremental-1') ON CONFLICT(name) DO UPDATE SET value=excluded.value"); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshMatching(); err != nil {
		t.Fatal(err)
	}
	got, err := s.RosterMembers(ch)
	if err != nil || len(got) != 1 || got[0].StreamID != feeds[1].StreamID {
		t.Fatalf("incorrect upgraded roster: %+v %v", got, err)
	}
	if err = s.Map(ch, wrong, true); err == nil {
		t.Fatal("manual map bypassed conflicting guide identity")
	}
	var accounts, strikes, events int
	if err = s.DB.QueryRow("SELECT count(*),sum(failures) FROM accounts").Scan(&accounts, &strikes); err != nil || accounts != 2 || strikes != 0 {
		t.Fatal(accounts, strikes, err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='roster_removed' AND source_id=?", wrong).Scan(&events); err != nil || events != 1 {
		t.Fatal("missing or duplicate removal event", events, err)
	}
	if err = s.RefreshMatching(); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='roster_removed' AND source_id=?", wrong).Scan(&events); err != nil || events != 1 {
		t.Fatal("upgrade refresh repeated", events, err)
	}
}
