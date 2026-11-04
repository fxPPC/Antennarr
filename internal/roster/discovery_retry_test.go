package roster

import (
	"antennarr/internal/activity"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"antennarr/internal/moderation"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type discoveryTransport struct {
	mode        string
	auth, lists int
}

func (t *discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.mode == "defer" {
		return nil, moderation.DeferredError{}
	}
	if t.mode == "network" {
		return nil, moderation.DeferredError{Failure: true}
	}
	b := `{"user_info":{"auth":1,"status":"Active","max_connections":2}}`
	if r.URL.Query().Get("action") == "get_live_streams" {
		t.lists++
		b = "not a catalogue"
		if t.mode == "success" {
			b = `[{"name":"MTV","stream_id":1}]`
		}
	} else {
		t.auth++
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(b))}, nil
}
func TestBrokenCatalogueCountsTwiceDespiteSuccessfulLogins(t *testing.T) {
	s, _, _, _, _ := fixture(t)
	s.Store.SaveRemovalPolicy(catalog.RemovalPolicy{AccountFailures: 2, StreamFailures: 2})
	v, _ := ingest.FromURL("https://broken.example/get.php?username=fixture&password=fixture", "", "")
	s.Store.Import([]ingest.Entry{v}, "fixture")
	var id int64
	s.Store.DB.QueryRow("SELECT id FROM accounts WHERE kind='xtream'").Scan(&id)
	tr := &discoveryTransport{}
	s.Client = &http.Client{Transport: tr}
	settings := config.Settings{}
	settings.Defaults()
	for i := 1; i <= 2; i++ {
		s.discover(context.Background(), settings, false)
		s.pruneFailedAccounts()
		var n int
		s.Store.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=?", id).Scan(&n)
		if i == 1 && n != 1 {
			t.Fatal("removed too early")
		}
		if i == 2 && n != 0 {
			t.Fatal("not deleted after two complete failures")
		}
	}
	if tr.auth != 2 || tr.lists != 2 {
		t.Fatal(tr.auth, tr.lists)
	}
}
func TestProviderPacingAndCancellationAreNotAccountFailures(t *testing.T) {
	for _, mode := range []string{"defer", "network", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, _, _ := fixture(t)
			v, _ := ingest.FromURL("https://a.example/get.php?username=fixture&password=fixture", "", "")
			s.Store.Import([]ingest.Entry{v}, "fixture")
			tr := &discoveryTransport{mode: mode}
			s.Client = &http.Client{Transport: tr}
			settings := config.Settings{}
			settings.Defaults()
			ctx := context.Background()
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			s.discover(ctx, settings, false)
			var n int
			s.Store.DB.QueryRow("SELECT failures FROM accounts WHERE kind='xtream'").Scan(&n)
			want := 0
			if mode == "network" {
				want = 1
			}
			if n != want {
				t.Fatal(n, want)
			}
		})
	}
}

func TestVacanciesGetDiscoveryTurnsDuringContinuousMaintenance(t *testing.T) {
	s, _, cs, p, _ := fixture(t)
	for _, c := range cs[:2] {
		s.Store.Record(c.StreamID, catalog.Result{Success: true})
	}
	s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Enabled: true})
	entry, _ := ingest.FromURL("https://new.example/get.php?username=fixture&password=fixture", "", "")
	s.Store.Import([]ingest.Entry{entry}, "fixture")
	tr := &discoveryTransport{mode: "success"}
	s.Client = &http.Client{Transport: tr}
	s.step(context.Background())
	if tr.auth != 1 || tr.lists != 1 {
		t.Fatal("pending roster pass starved input discovery", tr.auth, tr.lists)
	}
	for i := 0; i < 2; i++ {
		s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,next_api=0,blocked_until=0")
		s.step(context.Background())
	}
	if p.calls.Load() == 0 {
		t.Fatal("discovery starved roster maintenance")
	}
}

// Full upstream accounts are not playable reserves. A failed access attempt
// releases every membership, retries through the random queue, and obeys policy.
func TestOptInBusyAccountLeavesRostersAndIsDeletedAtThreshold(t *testing.T) {
	s, _, _, _, _ := fixture(t)
	s.Settings = func() config.Settings {
		v := config.Settings{}
		v.Defaults()
		v.CountFullConnectionsAsFailures = true
		return v
	}
	s.Store.SaveRemovalPolicy(catalog.RemovalPolicy{AccountFailures: 2, StreamFailures: 2})
	entry, _ := ingest.FromURL("https://busy.example/live/fixture/fixture/1.ts", "MTV", "")
	s.Store.Import([]ingest.Entry{entry}, "fixture")
	var id, sid int64
	s.Store.DB.QueryRow("SELECT id FROM accounts WHERE kind='xtream'").Scan(&id)
	s.Store.DB.QueryRow("SELECT id FROM streams WHERE account_id=?", id).Scan(&sid)
	s.Store.CatalogueChecked(id, time.Now().Unix())
	s.Store.Record(sid, catalog.Result{Success: true})
	s.Store.EnsureRosterPass(time.Now().Unix())
	s.Client = &http.Client{Transport: roundTripCheck(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"user_info":{"auth":1,"status":"Active","max_connections":1,"active_cons":1}}`))}, nil
	})}
	settings := config.Settings{}
	settings.Defaults()
	for attempt := 1; attempt <= 2; attempt++ {
		if ok, outcome := s.checkAccount(context.Background(), id, true, true, nil); ok || outcome != "unavailable" {
			t.Fatal(ok, outcome)
		}
		s.pruneFailedAccounts()
		var members, n int
		s.Store.DB.QueryRow("SELECT count(*) FROM roster WHERE account_id=?", id).Scan(&members)
		s.Store.DB.QueryRow("SELECT count(*) FROM accounts WHERE id=?", id).Scan(&n)
		if members != 0 || n != 2-attempt {
			t.Fatal(attempt, members, n)
		}
		if attempt == 1 {
			a, e := s.Store.QueueAccount(time.Now().Unix(), time.Hour)
			if e != nil || a.ID != id {
				t.Fatal("cached failed account not queued", a, e)
			}
		}
	}
}

type roundTripCheck func(*http.Request) (*http.Response, error)

func (f roundTripCheck) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBusyAccountCanRecoverWithoutRefetchingSavedCatalogue(t *testing.T) {
	s, _, _, _, _ := fixture(t)
	entry, _ := ingest.FromURL("https://recover.example/live/fixture/fixture/1.ts", "MTV", "")
	s.Store.Import([]ingest.Entry{entry}, "fixture")
	var id int64
	s.Store.DB.QueryRow("SELECT id FROM accounts WHERE kind='xtream'").Scan(&id)
	s.Store.CatalogueChecked(id, time.Now().Unix())
	s.Store.DiscoveryFailed(id, false)
	tr := &discoveryTransport{mode: "success"}
	s.Client = &http.Client{Transport: tr}
	settings := config.Settings{}
	settings.Defaults()
	if !s.discover(context.Background(), settings, false) {
		t.Fatal("retry not attempted")
	}
	var failures int
	var state string
	s.Store.DB.QueryRow("SELECT failures,state FROM accounts WHERE id=?", id).Scan(&failures, &state)
	if failures != 0 || state != "healthy" || tr.auth != 1 || tr.lists != 0 || s.recruitAccount != id {
		t.Fatal(failures, state, tr, s.recruitAccount)
	}
}

func TestAccessAndCatalogueTimeoutsReachStatus(t *testing.T) {
	for _, phase := range []string{"access", "catalogue"} {
		t.Run(phase, func(t *testing.T) {
			s, _, _, _, _ := fixture(t)
			s.Activity = activity.New()
			entry, _ := ingest.FromURL("https://timeout.example/get.php?username=fixture&password=fixture", "", "")
			s.Store.Import([]ingest.Entry{entry}, "fixture")
			s.Client = &http.Client{Transport: roundTripCheck(func(r *http.Request) (*http.Response, error) {
				if phase == "access" || r.URL.Query().Get("action") != "" {
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"user_info":{"auth":1,"status":"Active","max_connections":2}}`))}, nil
			})}
			v := config.Settings{}
			v.Defaults()
			s.discover(context.Background(), v, false)
			recent := s.Activity.Snapshot().Recent
			if len(recent) != 1 || recent[0].Outcome != "timeout" || recent[0].AccountID == 0 {
				t.Fatal(recent)
			}
			want := "account access"
			if phase == "catalogue" {
				want = "channel catalogue"
			}
			if !strings.Contains(recent[0].Message, want) {
				t.Fatal(recent)
			}
		})
	}
}

func TestFullAccountDefaultPreservesAccountAndFailureStreak(t *testing.T) {
	s, _, _, _, _ := fixture(t)
	s.Store.SaveRemovalPolicy(catalog.RemovalPolicy{AccountFailures: 2, StreamFailures: 2})
	entry, _ := ingest.FromURL("https://full.example/live/fixture/fixture/1.ts", "MTV", "")
	s.Store.Import([]ingest.Entry{entry}, "fixture")
	var id, sid int64
	s.Store.DB.QueryRow("SELECT id FROM accounts WHERE kind='xtream'").Scan(&id)
	s.Store.DB.QueryRow("SELECT id FROM streams WHERE account_id=?", id).Scan(&sid)
	s.Store.CatalogueChecked(id, time.Now().Unix())
	s.Store.Record(sid, catalog.Result{Success: true})
	s.Store.DB.Exec("UPDATE accounts SET failures=1 WHERE id=?", id)
	s.Client = &http.Client{Transport: roundTripCheck(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"user_info":{"auth":1,"status":"Active","max_connections":1,"active_cons":1}}`))}, nil
	})}
	for attempt := 0; attempt < 4; attempt++ {
		if ok, outcome := s.checkAccount(context.Background(), id, true, true, nil); ok || outcome != "unavailable" {
			t.Fatal(ok, outcome)
		}
		s.pruneFailedAccounts()
		var failures, members int
		var checked int64
		if e := s.Store.DB.QueryRow("SELECT failures,catalogue_checked FROM accounts WHERE id=?", id).Scan(&failures, &checked); e != nil || failures != 1 || checked == 0 {
			t.Fatal(failures, checked, e)
		}
		s.Store.DB.QueryRow("SELECT count(*) FROM roster WHERE account_id=?", id).Scan(&members)
		if members != 1 {
			t.Fatal("full account lost verified roster membership")
		}
		a, e := s.Store.QueueAccount(time.Now().Unix(), time.Hour)
		if e != nil || a.ID != id {
			t.Fatal("full account not queued", a, e)
		}
	}
	// Free slots allow recovery using the saved catalogue, without re-importing.
	tr := &discoveryTransport{mode: "success"}
	s.Client = &http.Client{Transport: tr}
	v := config.Settings{}
	v.Defaults()
	s.discover(context.Background(), v, false)
	var failures int
	s.Store.DB.QueryRow("SELECT failures FROM accounts WHERE id=?", id).Scan(&failures)
	if failures != 0 || tr.lists != 0 || s.recruitAccount != id {
		t.Fatal(failures, tr, s.recruitAccount)
	}
}
