package catalog

import (
	"antennarr/internal/ingest"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func addTestProfile(t *testing.T, s *Store, id, country string) Profile {
	t.Helper()
	p := Profile{ID: id, Kind: "hdhomerun", Name: id, Country: country, Enabled: true, PortOffset: 1, DeviceID: "12345678"}
	_, e := s.DB.Exec("INSERT INTO output_profiles(id,kind,name,country,enabled,port_offset,device_id) VALUES(?,?,?,?,1,1,?)", p.ID, p.Kind, p.Name, p.Country, p.DeviceID)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func saveProfileTestChannel(t *testing.T, s *Store, profile, name, country string) Channel {
	t.Helper()
	c := Channel{ProfileID: profile, Number: "101", Name: name, Region: country, Enabled: true}
	var e error
	c.ID, e = s.SaveChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestProfileScopedNumbersMatchingAndBulk(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	addTestProfile(t, s, "gb", "GB")
	us := saveProfileTestChannel(t, s, "primary", "Discovery", "US")
	gb := saveProfileTestChannel(t, s, "gb", "Discovery", "GB")
	for _, name := range []string{"US Discovery", "UK Discovery", "Discovery"} {
		v, _ := ingest.FromURL("https://example.test/"+name+".ts", name, "")
		if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range []Channel{us, gb} {
		got, e := s.Candidates(c.ID, false)
		if e != nil || len(got) != 1 {
			t.Fatalf("%s: candidates=%d err=%v", c.ProfileID, len(got), e)
		}
	}
	if _, e = s.SaveChannel(Channel{ID: us.ID, ProfileID: "gb", Number: "102", Name: "Discovery", Region: "GB", Enabled: true}); e == nil {
		t.Fatal("channel moved between tuner identities")
	}
	if e = s.ReplaceProfileChannels("gb", ""); e != nil {
		t.Fatal(e)
	}
	channels, e := s.ChannelsForProfile("primary")
	if e != nil || len(channels) != 1 || channels[0].ID != us.ID {
		t.Fatal("other tuner changed", channels, e)
	}
	rematched, e := s.Candidates(us.ID, false)
	if e != nil || len(rematched) != 2 {
		t.Fatal("clearing another lineup did not resolve saved ambiguity", len(rematched), e)
	}
}
func TestDisabledProfileRetainsRostersAndStopsWork(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := addTestProfile(t, s, "gb", "GB")
	c := saveProfileTestChannel(t, s, "gb", "Example GB", "GB")
	if e = s.EnableRosters(RosterPolicy{Target: 2, Preference: "stability", Race: 2}); e != nil {
		t.Fatal(e)
	}
	v, _ := ingest.FromURL("https://fixture.test/stream.ts", "UK Example GB", "")
	if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
		t.Fatal(e)
	}
	candidates, e := s.Candidates(c.ID, false)
	if e != nil || len(candidates) != 1 {
		t.Fatal(candidates, e)
	}
	if e = s.Record(candidates[0].StreamID, Result{Success: true}); e != nil {
		t.Fatal(e)
	}
	if e = s.EnsureRosterPass(time.Now().Unix()); e != nil {
		t.Fatal(e)
	}
	p.Enabled = false
	if _, e = s.SaveProfile(p); e != nil {
		t.Fatal(e)
	}
	if e = s.ReconcileRosters(); e != nil {
		t.Fatal(e)
	}
	entries, e := s.RosterEntries(c.ID)
	if e != nil || len(entries) != 1 {
		t.Fatal("disabled roster discarded", entries, e)
	}
	if e = s.EnsureRosterPass(time.Now().Unix()); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.NextRosterCheck(time.Now().Unix()); e != sql.ErrNoRows {
		t.Fatal("disabled roster checked", e)
	}
	state, e := s.RosterState()
	if e != nil || len(state.Channels) != 0 {
		t.Fatal("disabled roster recruited", state, e)
	}
	saved, e := s.RosterStateForProfile("gb")
	if e != nil || len(saved.Channels) != 1 || saved.Channels[0].Count != 1 || saved.Channels[0].Policy.Target != 2 {
		t.Fatal("disabled saved roster hidden", saved, e)
	}
	p.Enabled = true
	if _, e = s.SaveProfile(p); e != nil {
		t.Fatal(e)
	}
	entries, e = s.RosterEntries(c.ID)
	if e != nil || len(entries) != 1 {
		t.Fatal("re-enable lost roster", entries, e)
	}
}
func TestProfileLineupImportCannotReplaceAnotherTuner(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	addTestProfile(t, s, "gb", "GB")
	c := saveProfileTestChannel(t, s, "primary", "Example", "US")
	l, e := s.ExportLineup()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ImportLineupForProfile("gb", l); e != nil {
		t.Fatal(e)
	}
	primary, _ := s.ChannelsForProfile("primary")
	other, _ := s.ChannelsForProfile("gb")
	if len(primary) != 1 || primary[0].ID != c.ID || len(other) != 1 || other[0].ID == c.ID {
		t.Fatal("cross-tuner identity collision", primary, other)
	}
	if e = s.DeleteProfile("gb"); e != nil {
		t.Fatal(e)
	}
	primary, _ = s.ChannelsForProfile("primary")
	if len(primary) != 1 || primary[0].ID != c.ID {
		t.Fatal("profile deletion affected primary")
	}
}
func TestFreshProfilesAreOptionalAndNeverReseeded(t *testing.T) {
	root := t.TempDir()
	s, e := OpenWithUSDefaults(root)
	if e != nil {
		t.Fatal(e)
	}
	profiles, e := s.Profiles()
	if e != nil || len(profiles) != 2 {
		t.Fatal(profiles, e)
	}
	for _, p := range profiles {
		if p.Enabled {
			t.Fatal("fresh tuner enabled without user selection")
		}
		chs, e := s.ChannelsForProfile(p.ID)
		if e != nil || len(chs) == 0 {
			t.Fatal("preset empty", p, e)
		}
		if e = s.DeleteProfile(p.ID); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	s, e = OpenWithUSDefaults(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	profiles, e = s.Profiles()
	if e != nil || len(profiles) != 0 {
		t.Fatal("removed tuner resurrected", profiles, e)
	}
	p, e := s.AddProfilePreset("us-nyc", 8)
	if e != nil {
		t.Fatal(e)
	}
	if p.ID == "primary" || p.PortOffset <= 1 || p.DeviceID == "" {
		t.Fatal("old identity reused", p)
	}
	if _, e = s.AddProfilePreset("uk-london", p.PortOffset); e == nil {
		t.Fatal("allocation exceeded configured listener pool")
	}
}
func TestSchema8ProfileMigrationPreservesCustomData(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "database"), 0700); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("testdata/schema8.sql")
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "database/antennarr.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		"INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES('custom','101','User channel','NZ',0,'[\"Keep alias\"]')",
		"INSERT INTO settings(name,value) VALUES('channel_station:custom','TVNZ')",
		"INSERT INTO channel_policies(channel_id,target,preference,race) VALUES('custom',42,'diversity',3)",
		"INSERT INTO accounts(id,identity,provider,kind,secret,state,created) VALUES(1,'a','example.test','direct','{}','healthy',1)",
		"INSERT INTO streams(id,account_id,identity,name,secret,state) VALUES(1,1,'s','User channel','{}','healthy')",
		"INSERT INTO mappings(channel_id,stream_id) VALUES('custom',1)",
		"INSERT INTO roster(channel_id,stream_id,account_id,admitted,verified,due) VALUES('custom',1,1,1,2,3)",
		"PRAGMA user_version=8",
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	chs, e := s.ChannelsForProfile("primary")
	if e != nil || len(chs) != 1 || chs[0].ID != "custom" || chs[0].Enabled || chs[0].Station != "TVNZ" || chs[0].Aliases[0] != "Keep alias" {
		t.Fatal("custom data lost", chs, e)
	}
	p, e := s.Profile("primary")
	if e != nil || !p.Enabled || p.Country != "" || p.Name != "Existing tuner" {
		t.Fatal("migration guessed market", p, e)
	}
	roster, e := s.RosterEntries("custom")
	if e != nil || len(roster) != 1 {
		t.Fatal("migration lost roster", roster, e)
	}
	policy, e := s.ChannelPolicy("custom")
	if e != nil || policy.Target != 42 || policy.Race != 3 {
		t.Fatal("policy lost", policy, e)
	}
	if e = s.checkIntegrity(); e != nil {
		t.Fatal(e)
	}
	profiles, _ := s.Profiles()
	if len(profiles) != 2 {
		t.Fatal("London not offered", profiles)
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	again, _ := s.Profiles()
	if len(again) != 2 || again[1].ID != profiles[1].ID {
		t.Fatal("migration repeated")
	}
}

func TestSchema8MigrationRollbackPreservesOriginalFormat(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "database", "antennarr.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/schema8.sql")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	// A broken source reference forces the final integrity check to fail after
	// table replacement, proving SQLite rolls back schema and content together.
	if _, err = db.Exec("INSERT INTO channels(id,number,name) VALUES('kept','1','Kept'); INSERT INTO roster VALUES('kept',999,999,1,1,1); PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	if err = migrateProfiles(db); err == nil {
		t.Fatal("inconsistent data accepted")
	}
	var version, channels, assignments int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 8 {
		t.Fatal("schema version changed after failed migration", version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM channels WHERE id='kept'").Scan(&channels); err != nil || channels != 1 {
		t.Fatal("channel lost", channels, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM output_assignments WHERE channel_id='kept'").Scan(&assignments); err != nil || assignments != 1 {
		t.Fatal("old assignments lost", assignments, err)
	}
	var exists int
	if err = db.QueryRow("SELECT count(*) FROM pragma_table_info('channels') WHERE name='profile_id'").Scan(&exists); err != nil || exists != 0 {
		t.Fatal("partial schema committed", exists, err)
	}
}

func TestProfileSharedStreamHasIndependentRostersAndOneCheck(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	addTestProfile(t, s, "second", "US")
	first := saveProfileTestChannel(t, s, "primary", "Shared Service", "US")
	second := saveProfileTestChannel(t, s, "second", "Shared Service", "US")
	if err = s.EnableRosters(RosterPolicy{Target: 2, Preference: "stability", Race: 2}); err != nil {
		t.Fatal(err)
	}
	entry, err := ingest.FromURL("https://fixture.invalid/shared.ts", "US Shared Service", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Import([]ingest.Entry{entry}, "fixture"); err != nil {
		t.Fatal(err)
	}
	sources, err := s.Candidates(first.ID, false)
	if err != nil || len(sources) != 1 {
		t.Fatal(sources, err)
	}
	if err = s.Record(sources[0].StreamID, Result{Success: true}); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []Channel{first, second} {
		entries, err := s.RosterEntries(ch.ID)
		if err != nil || len(entries) != 1 {
			t.Fatal("independent roster missing", ch.ID, entries, err)
		}
	}
	if err = s.EnsureRosterPass(time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	pass, err := s.CheckPassStatus()
	if err != nil || pass.Total != 1 {
		t.Fatal("shared source scheduled twice", pass, err)
	}
	if err = s.ReplaceProfileChannels("second", ""); err != nil {
		t.Fatal(err)
	}
	entries, err := s.RosterEntries(first.ID)
	if err != nil || len(entries) != 1 {
		t.Fatal("other roster discarded", entries, err)
	}
	pass, err = s.CheckPassStatus()
	if err != nil || pass.Remaining != 1 {
		t.Fatal("other tuner check discarded", pass, err)
	}
}

func TestDisabledProfileCannotRecruitAmbiguousMarketFeeds(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profile := addTestProfile(t, s, "gb", "GB")
	saveProfileTestChannel(t, s, "primary", "Discovery", "US")
	saveProfileTestChannel(t, s, "gb", "Discovery", "GB")
	profile.Enabled = false
	if _, err = s.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableRosters(RosterPolicy{Target: 2, Preference: "stability", Race: 2}); err != nil {
		t.Fatal(err)
	}
	entries := []ingest.Entry{}
	for i, name := range []string{"Discovery", "UK Discovery", "US Discovery"} {
		entry, err := ingest.FromURL("https://fixture.invalid/"+string(rune('a'+i))+".ts", name, "")
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	relevant, err := s.RelevantEntries(entries)
	if err != nil || len(relevant) != 1 || relevant[0].Name != "US Discovery" {
		t.Fatal("disabled profile recruited or uncertain market admitted", relevant, err)
	}
}

func TestManualProfileMappingCannotCrossExplicitCountry(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	addTestProfile(t, s, "gb", "GB")
	us := saveProfileTestChannel(t, s, "primary", "Discovery", "US")
	gb := saveProfileTestChannel(t, s, "gb", "Discovery", "GB")
	entry, err := ingest.FromURL("https://fixture.invalid/us.ts", "US Discovery", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Import([]ingest.Entry{entry}, "fixture"); err != nil {
		t.Fatal(err)
	}
	candidates, err := s.Candidates(us.ID, false)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	candidate := candidates[0]
	if err = s.Map(gb.ID, candidate.StreamID, true); err == nil {
		t.Fatal("manual mapping crossed explicit country")
	}
	// An older persisted manual mapping is corrected by the same rules.
	if _, err = s.DB.Exec("INSERT INTO mappings VALUES(?,?,1,'manual')", gb.ID, candidate.StreamID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO roster VALUES(?,?,?,1,1,1)", gb.ID, candidate.StreamID, candidate.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("DELETE FROM settings WHERE name='matching_revision'"); err != nil {
		t.Fatal(err)
	}
	if err = s.RefreshMatching(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM roster WHERE channel_id=?", gb.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("unsafe old roster preserved", n, err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM mappings WHERE channel_id=? AND stream_id=?", gb.ID, candidate.StreamID).Scan(&n); err != nil || n != 0 {
		t.Fatal("unsafe old mapping preserved", n, err)
	}
}

func TestLondonReferencesStillVetoDeletedSisterChannel(t *testing.T) {
	channels, err := DefaultLondonChannels()
	if err != nil {
		t.Fatal(err)
	}
	var target channelMatcher
	for _, ch := range channels {
		if ch.Name == "5DRAMA" {
			target = compileChannel(ch)
			target.channel.ID = "5drama"
		}
	}
	if target.channel.ID == "" {
		t.Fatal("5Drama missing")
	}
	label := sourceLabelWithID("UK 5 Drama HD", "", "5star.uk")
	if got := resolveChannel([]channelMatcher{target}, label, "5star.uk", nil); got != nil {
		t.Fatal("deleted sister channel no longer vetoes conflicting identity")
	}
}

func TestLondonGuideIdentityRequiresNetworkAndMarket(t *testing.T) {
	_, matchers := londonPresetMatchers(t)
	for _, tc := range []struct{ name, guide, want string }{
		{"UK BBC One London HD", "bbc.one.london.uk", "BBC One London"},
		{"UK BBC One HD", "bbc.1.london.uk", "BBC One London"},
		{"UK ITV1 London HD", "itv1.london.uk", "ITV1 London"},
		{"UK ITV1 HD", "itv.london.uk", "ITV1 London"},
		{"UK BBC One London HD", "itv1.london.uk", ""},
		{"UK ITV1 London HD", "bbc.one.london.uk", ""},
		{"UK ITV1 HD", "bbc1.london.uk", ""},
	} {
		got := resolveChannel(matchers, sourceLabelWithID(tc.name, "", tc.guide), tc.guide, nil)
		if tc.want == "" {
			if got != nil {
				t.Errorf("conflicting %q / %q assigned to %s", tc.name, tc.guide, got.channel.Name)
			}
		} else if got == nil || got.channel.Name != tc.want {
			t.Errorf("%q / %q: got %s, want %s", tc.name, tc.guide, matcherName(got), tc.want)
		}
	}
	for _, matcher := range matchers {
		if matcher.channel.Station == "LONDON" && matcher.identifiesGuide(channelLabel{key: "london", country: "uk"}) {
			t.Errorf("shared market alone identifies %s", matcher.channel.Name)
		}
	}
}
