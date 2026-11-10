package catalog

import (
	"antennarr/internal/ingest"
	"encoding/json"
	"testing"
)

func TestLineupRoundTripKeepsAccountsButRebuildsRosters(t *testing.T) {
	s, ch, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	s.Record(cs[0].StreamID, Result{Success: true})
	s.SaveChannelPolicy(ch, RosterPolicy{Target: 7, Race: 3, Preference: "diversity"})
	var before int
	s.DB.QueryRow("SELECT count(*) FROM accounts").Scan(&before)
	v, err := s.ExportLineup()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	var next Lineup
	if err = json.Unmarshal(raw, &next); err != nil {
		t.Fatal(err)
	}
	if err = s.ImportLineup(next); err != nil {
		t.Fatal(err)
	}
	var after, n int
	s.DB.QueryRow("SELECT count(*) FROM accounts").Scan(&after)
	s.DB.QueryRow("SELECT count(*) FROM roster").Scan(&n)
	if before != after || n != 0 {
		t.Fatal("source state replaced", before, after, n)
	}
	got, _ := s.ExportLineup()
	raw2, _ := json.Marshal(got)
	if string(raw) != string(raw2) {
		t.Fatal("lineup roundtrip lost definitions or overrides")
	}
	bad := got
	bad.Channels = append(bad.Channels, bad.Channels[0])
	if s.ImportLineup(bad) == nil {
		t.Fatal("duplicate channel accepted")
	}
	got2, _ := s.ExportLineup()
	if len(got2.Channels) != len(got.Channels) {
		t.Fatal("invalid upload changed channels")
	}
}
func TestFullAccountReleasesRostersButLocalLimitsPreserveThem(t *testing.T) {
	s, _, cs := rosterFixture(t, 3)
	s.EnableRosters(RosterPolicy{30, "stability", 5})
	for _, c := range cs {
		s.Record(c.StreamID, Result{Success: true})
	}
	for _, kind := range []string{"oversized_catalogue", "oversized_response"} {
		if err := s.DeferCatalogue(cs[0].AccountID, kind); err != nil {
			t.Fatal(err)
		}
	}
	var before int
	s.DB.QueryRow("SELECT count(*) FROM roster").Scan(&before)
	if before != 3 {
		t.Fatal("local limit evicted rosters", before)
	}
	if err := s.AccountFull(cs[0].AccountID); err != nil {
		t.Fatal(err)
	}
	var n, f int
	s.DB.QueryRow("SELECT count(*) FROM roster").Scan(&n)
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", cs[0].AccountID).Scan(&f)
	if n != 2 || f != 0 {
		t.Fatal("full account roster release or strike incorrect", n, f)
	}
}

func TestReplacingMarketRediscoversXtreamBeforeDeletingUnmatchedAccounts(t *testing.T) {
	s, _, _ := rosterFixture(t, 1)
	if _, err := s.Import([]ingest.Entry{{Kind: "xtream", Endpoint: "https://fixture.example", Username: "fixture", Password: "fixture"}}, "fixture"); err != nil {
		t.Fatal(err)
	}
	var id int64
	s.DB.QueryRow("SELECT id FROM accounts WHERE kind='xtream'").Scan(&id)
	s.CatalogueChecked(id, 12345)
	v := Lineup{Version: 1, Channels: []LineupChannel{{ID: "gb-bbc-one", Number: "101", Name: "BBC One", Region: "GB", Enabled: true}}}
	if err := s.ImportLineup(v); err != nil {
		t.Fatal(err)
	}
	var checked int64
	if err := s.DB.QueryRow("SELECT catalogue_checked FROM accounts WHERE id=?", id).Scan(&checked); err != nil || checked != 0 {
		t.Fatal("catalogue not invalidated", checked, err)
	}
	if _, err := s.PruneCatalogue(nil); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=?", id).Scan(&n)
	if n != 1 {
		t.Fatal("valid account deleted before new channel discovery")
	}
}

func TestOnlyLegacyLocalCatalogueStrikesAreRepairedOnce(t *testing.T) {
	s, _, cs := rosterFixture(t, 2)
	s.DB.Exec("DELETE FROM settings WHERE name='catalogue_limit_strikes_repaired_v1'")
	s.DB.Exec("UPDATE accounts SET failures=1,state='suspect',last_failure_kind='oversized_catalogue',last_failure_phase='catalogue' WHERE id=?", cs[0].AccountID)
	s.DB.Exec("UPDATE accounts SET failures=1,state='suspect',last_failure_kind='timeout',last_failure_phase='catalogue' WHERE id=?", cs[1].AccountID)
	if err := s.repairCatalogueLimitStrikes(); err != nil {
		t.Fatal(err)
	}
	var a, b int
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", cs[0].AccountID).Scan(&a)
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", cs[1].AccountID).Scan(&b)
	if a != 0 || b != 1 {
		t.Fatal("wrong streak repaired", a, b)
	}
	s.DB.Exec("UPDATE accounts SET failures=1 WHERE id=?", cs[0].AccountID)
	if err := s.repairCatalogueLimitStrikes(); err != nil {
		t.Fatal(err)
	}
	s.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", cs[0].AccountID).Scan(&a)
	if a != 1 {
		t.Fatal("repair repeated")
	}
}
