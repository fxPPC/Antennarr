package catalog

import (
	"testing"
	"time"
)

func TestHistoryLimitsApplyWithoutIdleHousekeeping(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, e = s.DB.Exec(`WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10005) INSERT INTO events(happened,kind,reason) SELECT unixepoch(),'account_removed','fixture' FROM n`)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 10000 {
		t.Fatal("event history exceeded cap", n)
	}
	_, e = s.DB.Exec("INSERT INTO events(happened,kind,reason) VALUES(?,'account_removed','old fixture')", time.Now().Add(-8*24*time.Hour).Unix())
	if e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM events WHERE reason='old fixture'").Scan(&n)
	if n != 0 {
		t.Fatal("expired event retained")
	}
	_, e = s.DB.Exec(`INSERT INTO validations(id,checked,success,reason,startup_ms,duration_ms) VALUES(1,unixepoch(),1,'fixture',0,0),(50000,unixepoch(),1,'fixture',0,0)`)
	if e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM validations").Scan(&n)
	if n != 2 {
		t.Fatal("boundary record removed too early", n)
	}
	_, e = s.DB.Exec(`INSERT INTO validations(id,checked,success,reason,startup_ms,duration_ms) VALUES(50001,unixepoch(),1,'fixture',0,0)`)
	if e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM validations WHERE id=1").Scan(&n)
	if n != 0 {
		t.Fatal("validation retention did not run on insert")
	}
	s.DB.Exec("UPDATE events SET happened=0")
	if e = s.Housekeep(); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 0 {
		t.Fatal("idle housekeeping did not expire events", n)
	}
}
