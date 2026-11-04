package admin

import (
	"antennarr/internal/activity"
	"antennarr/internal/catalog"
	"antennarr/internal/ingest"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestActivityReportsRealQueueTotals(t *testing.T) {
	s, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.SaveChannel(catalog.Channel{Number: "1", Name: "CNN", Region: "US", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	entry, _ := ingest.FromURL("https://fixture.example/live/user/pass/1.ts", "CNN", "")
	if _, err = s.Import([]ingest.Entry{entry}, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableRosters(catalog.RosterPolicy{Target: 2, Preference: "stability", Race: 5}); err != nil {
		t.Fatal(err)
	}
	a := API{Store: s, Activity: &activity.Tracker{}}
	check := func(accounts, streams, discovery int) {
		t.Helper()
		w := httptest.NewRecorder()
		a.rosterSummary(w, httptest.NewRequest("GET", "/api/roster?queue=1", nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var got catalog.RosterSummary
		if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		r := got
		if r.QueuedAccounts != accounts || r.QueuedStreams != streams || r.AwaitingDiscovery != discovery {
			t.Fatalf("queue accounts/streams/discovery = %d/%d/%d; want %d/%d/%d", r.QueuedAccounts, r.QueuedStreams, r.AwaitingDiscovery, accounts, streams, discovery)
		}
	}
	check(1, 1, 1)
	s.DB.Exec("UPDATE accounts SET state='retired'")
	s.DB.Exec("UPDATE streams SET state='retired'")
	check(0, 0, 0)
}
