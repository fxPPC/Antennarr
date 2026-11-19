package catalog

import (
	"fmt"
	"maps"
	"strconv"
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

// Several markets can reuse guide numbers. The overview counts saved channels,
// including disabled ones, and must still work when every tuner is removed.
func TestMarketCountsAcrossTunerLifecycles(t *testing.T) {
	root := t.TempDir()
	s, err := OpenWithDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	profiles, err := s.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if err := s.DeleteProfile(p.ID); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want map[string]int) {
		t.Helper()
		got, err := s.ChannelCountsByProfile()
		if err != nil || !maps.Equal(got, want) {
			t.Fatalf("counts %v, want %v, error %v", got, want, err)
		}
	}
	check(nil)
	want := map[string]int{}
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("market-%d", i)
		_, err := s.DB.Exec("INSERT INTO output_profiles(id,kind,name,port_offset) VALUES(?,'hdhomerun',?,?)", id, id, i)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.DB.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<200)
		INSERT INTO channels(id,profile_id,number,name,enabled) SELECT ?||'-'||x,?,x,'Fixture '||x,x%2 FROM n`, id, id)
		if err != nil {
			t.Fatal(err)
		}
		lineup, err := s.TunerLineup(id)
		if err != nil || len(lineup) != 100 {
			t.Fatalf("enabled lineup length %d, err %v", len(lineup), err)
		}
		for j, c := range lineup {
			number := strconv.Itoa(2*j + 1)
			if c.Number != number || c.ID != id+"-"+number {
				t.Fatalf("wrong lineup row: %+v", c)
			}
		}
		want[id] = 200
		check(want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenWithDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	check(want)
}
