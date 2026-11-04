package catalog

import (
	"antennarr/internal/ingest"
	"testing"
	"time"
)

func cleanupFixture(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.Exec(`INSERT INTO accounts(id,identity,provider,rate_key,kind,secret,state,last_check,created) VALUES
 (1,'keep','mixed.example','mixed.example','direct','{}','healthy',123,0),
 (2,'keep-dead','mixed.example:80','mixed.example','direct','{}','inactive',123,0),
 (3,'unchecked','unchecked.example','unchecked.example','direct','{}','unverified',0,0),
 (4,'suspect','suspect.example','suspect.example','direct','{}','suspect',123,0),
 (5,'blocked','blocked.example','blocked.example','direct','{}','manually_blocked',123,0),
 (99,'expired','dead.example:80','dead.example','direct','{}','inactive',123,0),
 (100,'retired','dead.example:8080','dead.example','direct','{}','retired',123,0);
 INSERT INTO channels(id,number,name) VALUES('permanent-channel','102','MTV');
 INSERT INTO streams(id,account_id,identity,name,secret) VALUES(100,100,'dead-stream','MTV','{}');
 INSERT INTO mappings(channel_id,stream_id) VALUES('permanent-channel',100);
 INSERT INTO provenance VALUES(100,'fixture','fixture',0,0);
 INSERT INTO validations(stream_id,checked,success,reason,startup_ms,duration_ms) VALUES(100,1,0,'fixture',0,0);
 INSERT INTO events(happened,kind,source_id) VALUES(1,'source_failure',100);
 INSERT INTO provider_rates(provider) VALUES('dead.example'),('mixed.example'),('*');`)
	if e != nil {
		s.Close()
		t.Fatal(e)
	}
	return s, root
}
func TestPruneOnlyConfirmedDeadProvidersAndPreserveIDs(t *testing.T) {
	s, root := cleanupFixture(t)
	n, e := s.PruneDeadProviders()
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	for table, want := range map[string]int{"accounts": 5, "streams": 0, "provenance": 0, "mappings": 0, "validations": 0, "events": 2, "channels": 1, "provider_rates": 2} {
		var got int
		if e = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&got); e != nil || got != want {
			t.Fatalf("%s: %d != %d (%v)", table, got, want, e)
		}
	}
	rows, e := s.DB.Query("PRAGMA foreign_key_check")
	if e != nil {
		t.Fatal(e)
	}
	if rows.Next() {
		t.Fatal("broken foreign key")
	}
	rows.Close()
	if n, e = s.PruneDeadProviders(); e != nil || n != 0 {
		t.Fatal("not idempotent", n, e)
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, e = s.Import([]ingest.Entry{{Kind: "direct", Endpoint: "https://new.example", URL: "https://new.example/live.ts", Name: "MTV"}}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	var aid, sid int64
	s.DB.QueryRow("SELECT max(id) FROM accounts").Scan(&aid)
	s.DB.QueryRow("SELECT max(id) FROM streams").Scan(&sid)
	if aid <= 100 || sid <= 100 {
		t.Fatal("deleted identifiers reused", aid, sid)
	}
}
func TestProviderCleanupRollsBackRelatedDeletion(t *testing.T) {
	s, _ := cleanupFixture(t)
	defer s.Close()
	_, e := s.DB.Exec(`CREATE TRIGGER prevent_cleanup BEFORE DELETE ON accounts BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PruneDeadProviders(); e == nil {
		t.Fatal("expected transaction failure")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM mappings WHERE stream_id=100").Scan(&n)
	if n != 1 {
		t.Fatal("partial deletion escaped rollback")
	}
	s.DB.Exec("DROP TRIGGER prevent_cleanup")
	if n, e := s.PruneDeadProviders(); e != nil || n != 1 {
		t.Fatal("retry failed", n, e)
	}
}

func TestDeadProviderCleanupHonoursStreamLease(t *testing.T) {
	s, _ := cleanupFixture(t)
	defer s.Close()
	if _, err := s.DB.Exec("INSERT INTO work_leases VALUES('stream',100,?)", time.Now().Unix()+3600); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PruneDeadProviders(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	s.DB.Exec("DELETE FROM work_leases WHERE kind='stream' AND resource=100")
	if n, err := s.PruneDeadProviders(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
