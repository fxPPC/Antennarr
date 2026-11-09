package catalog

import (
	"antennarr/internal/ingest"
	"encoding/json"
	"testing"
	"time"
)

func pruneFixture(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_, err = s.DB.Exec(`INSERT INTO channels(id,number,name,enabled) VALUES('cnn','1','CNN',0);
 INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,created,catalogue_checked) VALUES
 (1,'mixed','mixed.example','mixed.example','xtream','{}','healthy',1,1),
 (2,'retired','mixed.example','mixed.example','xtream','{}','retired',1,1),
 (3,'empty-checked','empty.example','empty.example','xtream','{}','healthy',1,1),
 (4,'unexamined','pending.example','pending.example','xtream','{}','unverified',1,0),
 (5,'uhd','uhd.example','uhd.example','xtream','{}','healthy',1,1),
 (6,'protected','protected.example','protected.example','xtream','{}','retired',1,1),
 (7,'leased','leased.example','leased.example','xtream','{}','retired',1,1),
 (8,'unmatched-direct','direct.example','direct.example','direct','{}','healthy',1,0),
 (9,'stranded','stranded.example','stranded.example','xtream','{}','healthy',1,1);
 INSERT INTO streams(id,account_id,identity,name,secret,state) VALUES
 (1,1,'keep','CNN HD','{}','healthy'),(2,1,'retired-stream','CNN HD','{}','retired'),
 (3,2,'retired-account-stream','CNN HD','{}','healthy'),(4,5,'uhd-only','CNN UHD','{}','healthy'),
 (5,6,'protected-stream','CNN','{}','retired'),(6,7,'leased-stream','CNN','{}','retired'),
 (7,8,'unmatched','Other Channel','{}','healthy'),(8,9,'last-retired','CNN','{}','retired');
 INSERT INTO mappings(channel_id,stream_id) SELECT 'cnn',id FROM streams WHERE id!=7;
 INSERT INTO provenance SELECT id,'fixture','fixture',0,0 FROM accounts;
 INSERT INTO validations(stream_id,checked,success,reason,startup_ms,duration_ms) SELECT id,1,0,'fixture',0,0 FROM streams;
 INSERT INTO events(happened,kind,source_id) SELECT 1,'source_failure',id FROM streams;
 INSERT INTO roster VALUES('cnn',2,1,1,1,1);
 INSERT INTO provider_rates(provider) SELECT rate_key FROM accounts GROUP BY rate_key;
 INSERT INTO provider_rates(provider) VALUES('*');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec("INSERT INTO work_leases VALUES('stream',6,?),('cooldown',2,?),('deferred',8,?),('quality_invalid',2,?),('account',999,?)", time.Now().Unix()+3600, time.Now().Unix()+3600, time.Now().Unix()+3600, time.Now().Unix()+3600, time.Now().Unix()+3600)
	if err != nil {
		t.Fatal(err)
	}
	cache := qualityRosterCache{SavedAt: time.Now().Unix(), Entries: []cachedRosterEntry{{Channel: "cnn", Stream: 1, Account: 1}, {Channel: "cnn", Stream: 2, Account: 1}, {Channel: "cnn", Stream: 4, Account: 5}, {Channel: "cnn", Stream: 8, Account: 9}}}
	b, _ := json.Marshal(cache)
	_, err = s.DB.Exec("INSERT INTO settings VALUES(?,?)", qualityCacheKey(false), string(b))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestPruneCatalogueRetirementMatchingAndReferences(t *testing.T) {
	s := pruneFixture(t)
	result, err := s.PruneCatalogue([]int64{6})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accounts != 4 || result.Streams != 4 {
		t.Fatal(result)
	}
	for _, id := range []int{1, 4, 5, 6, 7} {
		var n int
		if err = s.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=?", id).Scan(&n); err != nil || n != 1 {
			t.Fatal(id, n, err)
		}
	}
	for _, id := range []int{2, 3, 8, 9} {
		var n int
		s.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=?", id).Scan(&n)
		if n != 0 {
			t.Fatal("account not removed", id)
		}
	}
	for _, table := range []string{"validations", "mappings", "events", "roster"} {
		var n int
		key := "stream_id"
		if table == "events" {
			key = "source_id"
		}
		if err = s.DB.QueryRow("SELECT count(*) FROM " + table + " WHERE " + key + " IN (2,3,7,8)").Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM work_leases WHERE resource IN(2,8,999)").Scan(&n)
	if n != 0 {
		t.Fatal("orphan leases", n)
	}
	s.DB.QueryRow("SELECT count(*) FROM provider_rates WHERE provider IN('empty.example','direct.example','stranded.example')").Scan(&n)
	if n != 0 {
		t.Fatal("orphan providers", n)
	}
	var raw string
	s.DB.QueryRow("SELECT value FROM settings WHERE name=?", qualityCacheKey(false)).Scan(&raw)
	var cache qualityRosterCache
	if err = json.Unmarshal([]byte(raw), &cache); err != nil || len(cache.Entries) != 2 {
		t.Fatal(cache, err)
	}
	rows, err := s.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
	rows.Close()
	if result, err = s.PruneCatalogue([]int64{6}); err != nil || result.Accounts+result.Streams != 0 {
		t.Fatal("not idempotent", result, err)
	}
	// After a check lease ends, the retired account can finally be removed.
	s.DB.Exec("DELETE FROM work_leases WHERE resource=6")
	if result, err = s.PruneCatalogue(nil); err != nil || result.Accounts != 2 || result.Streams != 2 {
		t.Fatal(result, err)
	}
	_, err = s.Import([]ingest.Entry{{Kind: "direct", Endpoint: "https://new.example", URL: "https://new.example/live.ts", Name: "CNN"}}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var aid, sid int64
	s.DB.QueryRow("SELECT max(id) FROM accounts").Scan(&aid)
	s.DB.QueryRow("SELECT max(id) FROM streams").Scan(&sid)
	if aid <= 9 || sid <= 8 {
		t.Fatal("deleted IDs reused", aid, sid)
	}
}
func TestPruneCatalogueRollback(t *testing.T) {
	s := pruneFixture(t)
	s.DB.Exec("CREATE TRIGGER prevent_prune BEFORE DELETE ON accounts BEGIN SELECT RAISE(ABORT,'fixture'); END")
	if _, err := s.PruneCatalogue(nil); err == nil {
		t.Fatal("expected failure")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM streams").Scan(&n)
	if n != 8 {
		t.Fatal("partial deletion", n)
	}
	s.DB.QueryRow("SELECT count(*) FROM validations").Scan(&n)
	if n != 8 {
		t.Fatal("lost history on rollback", n)
	}
	s.DB.Exec("DROP TRIGGER prevent_prune")
	if _, err := s.PruneCatalogue(nil); err != nil {
		t.Fatal(err)
	}
}

func TestLargeCataloguePruningRemainsResponsive(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`INSERT INTO channels(id,number,name) VALUES('cnn','1','CNN');
 WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<2000)
 INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,created,catalogue_checked) SELECT x,'account-'||x,'provider.example','provider.example','xtream','{}','healthy',1,1 FROM n;
 WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<20000)
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT x,1+(x%2000),'stream-'||x,'CNN','{}' FROM n;
 INSERT INTO mappings(channel_id,stream_id) SELECT 'cnn',id FROM streams;`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	result, err := s.PruneCatalogue(nil)
	if err != nil || result.Accounts+result.Streams != 0 {
		t.Fatal(result, err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("cleanup holds reservations too long: %s", elapsed)
	}
	t.Logf("2000 accounts / 20000 streams: %s", time.Since(start))
}

func TestAccountLeaseProtectsRetiredAccountAndItsStreams(t *testing.T) {
	s := pruneFixture(t)
	if _, err := s.DB.Exec("INSERT INTO work_leases VALUES('account',2,?)", time.Now().Unix()+3600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneCatalogue(nil); err != nil {
		t.Fatal(err)
	}
	for table, where := range map[string]string{"accounts": "id=2", "streams": "id=3", "provenance": "account_id=2"} {
		var n int
		if err := s.DB.QueryRow("SELECT count(*) FROM " + table + " WHERE " + where).Scan(&n); err != nil || n != 1 {
			t.Fatal(table, n, err)
		}
	}
	s.DB.Exec("DELETE FROM work_leases WHERE kind='account' AND resource=2")
	if _, err := s.PruneCatalogue(nil); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=2").Scan(&n)
	if n != 0 {
		t.Fatal("lease never released")
	}
}

func TestReserveBoundPreservesRosterAndManualSources(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`INSERT INTO channels(id,number,name) VALUES('bounded','999','Bounded');
 INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,created,catalogue_checked) VALUES(901,'bounded-account','fixture.invalid','fixture.invalid','xtream','{}','healthy',1,1);
 WITH RECURSIVE n(x) AS(VALUES(1001) UNION ALL SELECT x+1 FROM n WHERE x<1100)
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT x,901,'bounded-'||x,'Bounded','{}' FROM n;
 INSERT INTO mappings(channel_id,stream_id,origin) SELECT 'bounded',id,'auto' FROM streams;
 UPDATE mappings SET origin='manual' WHERE stream_id=1100;
 INSERT INTO roster(channel_id,stream_id,account_id,admitted,verified,due) VALUES('bounded',1099,901,1,1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	s.rosterPolicy.Store(&RosterPolicy{Target: 20})
	result, err := s.PruneCatalogue(nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM streams WHERE account_id=901").Scan(&n)
	if n != 62 || result.Streams != 38 {
		t.Fatalf("retained=%d removed=%d", n, result.Streams)
	}
	for _, id := range []int{1099, 1100} {
		s.DB.QueryRow("SELECT count(*) FROM streams WHERE id=?", id).Scan(&n)
		if n != 1 {
			t.Fatal("protected source lost", id)
		}
	}
}

func TestAutomaticReserveCleanupUsesOneNotice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`INSERT INTO channels(id,number,name) VALUES('bulk','998','Bulk');
 INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,created,catalogue_checked) VALUES(900,'bulk-account','fixture.invalid','fixture.invalid','xtream','{}','healthy',1,1);
 WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<20000)
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT x,900,'bulk-'||x,'Bulk','{}' FROM n;
 INSERT INTO mappings(channel_id,stream_id,origin) SELECT 'bulk',id,'automatic' FROM streams;`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.PruneCatalogue(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Streams != 19910 {
		t.Fatal(result)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='catalogue_trimmed'").Scan(&n)
	if n != 1 {
		t.Fatal("expected one trim notice", n)
	}
	s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='stream_removed'").Scan(&n)
	if n != 0 {
		t.Fatal("reserve trim flooded failure events", n)
	}
}

func TestUHDReserveDoesNotBlockHDDiscovery(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`INSERT INTO channels(id,number,name) VALUES('quality','997','Quality');
 INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,created,catalogue_checked) VALUES(899,'quality-account','fixture.invalid','fixture.invalid','xtream','{}','healthy',1,1);
 WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<60)
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT x,899,'quality-'||x,'Quality UHD','{}' FROM n;
 INSERT INTO mappings(channel_id,stream_id,origin) SELECT 'quality',id,'automatic' FROM streams;`)
	if err != nil {
		t.Fatal(err)
	}
	s.rosterPolicy.Store(&RosterPolicy{Target: 20})
	entries, err := s.RelevantEntries([]ingest.Entry{{Name: "Quality HD"}})
	if err != nil || len(entries) != 1 {
		t.Fatal("UHD reserve blocked HD discovery", entries, err)
	}
	_, err = s.DB.Exec(`WITH RECURSIVE n(x) AS(VALUES(61) UNION ALL SELECT x+1 FROM n WHERE x<120)
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT x,899,'quality-'||x,'Quality HD','{}' FROM n;
 INSERT INTO mappings(channel_id,stream_id,origin) SELECT 'quality',id,'automatic' FROM streams WHERE id>60;`)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.PruneCatalogue(nil)
	if err != nil || v.Streams != 0 {
		t.Fatal("quality reserves competed for capacity", v, err)
	}
}

func TestBoundedReserveRetainsOneCandidatePerAccount(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`INSERT INTO channels(id,number,name) VALUES('anchor','996','Anchor');
 WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<64)
 INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,created,catalogue_checked) SELECT x,'anchor-account-'||x,'fixture.invalid','fixture.invalid','xtream','{}','healthy',1,1 FROM n;
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT id,id,'anchor-stream-'||id,'Anchor HD','{}' FROM accounts;
 INSERT INTO mappings(channel_id,stream_id,origin) SELECT 'anchor',id,'automatic' FROM streams;`)
	if err != nil {
		t.Fatal(err)
	}
	s.rosterPolicy.Store(&RosterPolicy{Target: 20})
	entries, err := s.RelevantEntries([]ingest.Entry{{Name: "Anchor HD"}})
	if err != nil || len(entries) != 1 {
		t.Fatal("new account lost matching evidence at reserve capacity", entries, err)
	}
	result, err := s.PruneCatalogue(nil)
	if err != nil || result.Accounts != 0 || result.Streams != 0 {
		t.Fatal("healthy queued account lost its final candidate", result, err)
	}
}
