package catalog

import (
	"testing"
	"time"
)

// Realistic catalogue size catches quadratic reverse-mapping schedule scans.
func TestLargeCatalogueScheduleRemainsResponsive(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, e = s.DB.Exec(`INSERT INTO accounts(id,identity,provider,kind,secret,created,state) VALUES(1,'fixture','fixture','direct','{}',0,'healthy');
 INSERT INTO channels(id,number,name) VALUES('cnn','101','CNN');
 WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<12000)
 INSERT INTO streams(id,account_id,identity,name,secret) SELECT x,1,'stream-'||x,'CNN','{}' FROM n;
 INSERT INTO mappings(channel_id,stream_id) SELECT 'cnn',id FROM streams;`)
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	summary, e := s.RosterSummary()
	if e != nil {
		t.Fatal(e)
	}
	if summary.QueuedStreams != 12000 {
		t.Fatal(summary)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("schedule blocks administration: %s", elapsed)
	}
	t.Logf("12000-stream schedule: %s", time.Since(start))
}
