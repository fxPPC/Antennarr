package roster

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"antennarr/internal/media"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProbe struct {
	calls   atomic.Int32
	success bool
	cancel  bool
}

func (p *fakeProbe) Probe(ctx context.Context, _ string, _ string, _ time.Duration) media.ProbeResult {
	p.calls.Add(1)
	if p.cancel {
		<-ctx.Done()
	}
	return media.ProbeResult{Success: p.success, Reason: "fixture"}
}

type countTransport struct{ calls atomic.Int32 }

func (t *countTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, fmt.Errorf("unexpected network request")
}
func fixture(t *testing.T) (*Service, string, []catalog.Candidate, *fakeProbe, *countTransport) {
	t.Helper()
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	ch, e := store.SaveChannel(catalog.Channel{Number: "1", Name: "MTV", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		v, _ := ingest.FromURL(fmt.Sprintf("https://p%d.example/1.ts", i), "MTV", "")
		store.Import([]ingest.Entry{v}, "fixture")
	}
	cs, e := store.Candidates(ch, false)
	if e != nil {
		t.Fatal(e)
	}
	store.EnableRosters(catalog.RosterPolicy{Target: 2, Preference: "stability", Race: 5})
	p := &fakeProbe{success: true}
	tr := &countTransport{}
	s := &Service{Store: store, Budget: budget.New(10, 5), Gateway: p, Client: &http.Client{Transport: tr}}
	return s, ch, cs, p, tr
}
func TestFullRosterMaintainsPassesWithoutAuditioningQueue(t *testing.T) {
	s, ch, cs, p, tr := fixture(t)
	for _, c := range cs[:2] {
		if e := s.Store.Record(c.StreamID, catalog.Result{Success: true}); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 4; i++ {
		s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,next_api=0,blocked_until=0")
		s.step(context.Background())
	}
	if p.calls.Load() != 4 || tr.calls.Load() != 0 {
		t.Fatal("continuous maintenance failed", p.calls.Load(), tr.calls.Load())
	}
	var extra int
	s.Store.DB.QueryRow("SELECT count(*) FROM validations WHERE stream_id NOT IN (?,?)", cs[0].StreamID, cs[1].StreamID).Scan(&extra)
	if extra != 0 {
		t.Fatal("full roster auditioned input queue")
	}
	s.Invalidate(cs[0].StreamID)
	s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,next_api=0,blocked_until=0")
	s.step(context.Background())
	rows, e := s.Store.RosterMembers(ch)
	if e != nil || len(rows) != 2 || p.calls.Load() != 5 {
		t.Fatal(len(rows), p.calls.Load(), e)
	}
}
func TestDueCheckFailureEvictsAndLiveWorkPauses(t *testing.T) {
	s, ch, cs, p, _ := fixture(t)
	for _, c := range cs[:2] {
		s.Store.Record(c.StreamID, catalog.Result{Success: true})
	}
	s.Store.DB.Exec("UPDATE roster SET due=0")
	s.Live = func() bool { return true }
	s.step(context.Background())
	if p.calls.Load() != 0 {
		t.Fatal("background competed with viewer")
	}
	s.Live = func() bool { return false }
	p.success = false
	s.step(context.Background())
	rows, e := s.Store.RosterMembers(ch)
	if e != nil || len(rows) != 1 {
		t.Fatal(len(rows), e)
	}
}
func TestCancelledProbeKeepsRoster(t *testing.T) {
	s, ch, cs, p, _ := fixture(t)
	s.Store.Record(cs[0].StreamID, catalog.Result{Success: true})
	p.cancel = true
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for p.calls.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	var v config.Settings
	v.Defaults()
	s.probe(ctx, cs[0], v)
	rows, e := s.Store.RosterMembers(ch)
	if e != nil || len(rows) != 1 {
		t.Fatal("cancellation evicted healthy stream", e)
	}
}
func TestNewestRosterBeforeEmergencyQueue(t *testing.T) {
	s, ch, cs, _, _ := fixture(t)
	s.Store.Record(cs[0].StreamID, catalog.Result{Success: true})
	s.Store.Record(cs[1].StreamID, catalog.Result{Success: true})
	s.Store.DB.Exec("UPDATE roster SET verified=100 WHERE stream_id=?", cs[0].StreamID)
	s.Store.DB.Exec("UPDATE roster SET verified=200 WHERE stream_id=?", cs[1].StreamID)
	s.Store.DB.Exec("UPDATE streams SET score=1 WHERE id=?", cs[0].StreamID)
	rows, e := s.Candidates(ch, false)
	if e != nil || len(rows) != 2 || rows[0].StreamID != cs[1].StreamID {
		t.Fatal(rows, e)
	}
}

func TestLiveClaimsDoNotWaitForDatabaseWriter(t *testing.T) {
	s, _, cs, _, _ := fixture(t)
	tx, e := s.Store.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	start := time.Now()
	release, ok := s.ClaimPlayback(cs[0].StreamID)
	if !ok {
		t.Fatal("claim failed")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("live claim waited for database")
	}
	if _, ok = s.ClaimPlayback(cs[0].StreamID); ok {
		t.Fatal("duplicate live claim")
	}
	release()
	release()
	if _, ok = s.ClaimPlayback(cs[0].StreamID); !ok {
		t.Fatal("claim not released")
	}
}

func TestUrgentFillingCannotStarveOverdueMaintenance(t *testing.T) {
	s, _, cs, p, _ := fixture(t)
	for _, c := range cs[:2] {
		s.Store.Record(c.StreamID, catalog.Result{Success: true})
	}
	s.Store.DB.Exec("UPDATE roster SET due=1")
	other, _ := s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Region: "US", Enabled: true})
	entry, _ := ingest.FromURL("https://new.example/cnn.ts", "CNN", "")
	s.Store.Import([]ingest.Entry{entry}, "fixture")
	s.turn = 2 // The third turn is reserved for maintenance, even with an empty channel.
	s.step(context.Background())
	rows, e := s.Store.RosterMembers(other)
	if e != nil || len(rows) != 0 || p.calls.Load() != 1 {
		t.Fatal("maintenance turn consumed by filling", e)
	}
	var completed int
	s.Store.DB.QueryRow("SELECT count(*) FROM roster WHERE due>1").Scan(&completed)
	if completed != 1 {
		t.Fatal("overdue member was not checked")
	}
}

func TestPauseCancelsProbeAndBlocksNewChecks(t *testing.T) {
	s, _, _, p, _ := fixture(t)
	p.cancel = true
	finished := make(chan struct{})
	go func() { s.step(context.Background()); close(finished) }()
	deadline := time.Now().Add(time.Second)
	for p.calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("probe not started")
		}
		time.Sleep(time.Millisecond)
	}
	if e := s.WithPaused(func() error {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("pause returned before check stopped")
		}
		before := p.calls.Load()
		s.step(context.Background())
		if p.calls.Load() != before {
			t.Fatal("new check during pause")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if s.paused.Load() {
		t.Fatal("checker did not resume")
	}
}

func TestRecruitmentPrioritisesThinRostersAndSpreadsAccountUse(t *testing.T) {
	s, _, cs, _, _ := fixture(t)
	other, err := s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Region: "US", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// The first account is already a verified source on the thicker channel.
	if err = s.Store.Record(cs[0].StreamID, catalog.Result{Success: true}); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{cs[0].Provider, "independent.example"} {
		entry, err := ingest.FromURL("https://"+host+"/cnn.ts", "CNN", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Store.Import([]ingest.Entry{entry}, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := s.Store.QueueCandidates(other, 10)
	if err != nil || len(queue) != 2 || queue[1].AccountID != cs[0].AccountID {
		t.Fatal("expected independent account first and shared account as fallback", queue, err)
	}
	// Force the old random queue order to prefer the shared account.
	if _, err = s.Store.DB.Exec("UPDATE queue_positions SET position=CASE WHEN source_id=? THEN 0 ELSE 1 END WHERE kind='stream'", queue[1].StreamID); err != nil {
		t.Fatal(err)
	}
	s.step(context.Background())
	members, err := s.Store.RosterMembers(other)
	if err != nil || len(members) != 1 || members[0].AccountID != queue[0].AccountID {
		t.Fatal("thinnest channel should get the independent account", members, err)
	}
	queue, err = s.Store.QueueCandidates(other, 10)
	if err != nil || len(queue) != 1 || queue[0].AccountID != cs[0].AccountID {
		t.Fatal("shared account must remain eligible", queue, err)
	}
}

func TestRecruitmentRotatesTiesAndSkipsUnfillableChannels(t *testing.T) {
	s, ch, _, p, _ := fixture(t)
	other, err := s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Region: "US", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := ingest.FromURL("https://cnn.example/channel.ts", "CNN", "")
	if _, err = s.Store.Import([]ingest.Entry{entry}, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.SaveChannel(catalog.Channel{Number: "3", Name: "Empty", Region: "US", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	p.success = false // Failed auditions leave both rosters at the same depth.
	attempts := map[string]int{}
	for i := 0; i < 2; i++ {
		if _, err = s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,next_api=0,blocked_until=0"); err != nil {
			t.Fatal(err)
		}
		s.step(context.Background())
		attempts[s.lastRecruitChannel]++
	}
	if p.calls.Load() != 2 || attempts[ch] != 1 || attempts[other] != 1 {
		t.Fatal("expected one audition per fillable channel", attempts, p.calls.Load())
	}
}

type authTransport struct{ calls atomic.Int32 }

func (t *authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"user_info":{"auth":1,"status":"Active","max_connections":"5","active_cons":"0"}}`)), Request: r}, nil
}
func TestCachedAccessSurvivesFailedChannelAudition(t *testing.T) {
	s, ch, _, p, _ := fixture(t)
	other, _ := s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Region: "US", Enabled: true})
	for i, name := range []string{"MTV", "CNN"} {
		entry, _ := ingest.FromURL(fmt.Sprintf("https://batch.example/live/user/pass/%d.ts", i), name, "")
		if _, err := s.Store.Import([]ingest.Entry{entry}, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	tr := &authTransport{}
	s.Client = &http.Client{Transport: tr}
	next, err := s.Store.QueueCandidates(other, 1)
	if err != nil || len(next) != 1 {
		t.Fatal(next, err)
	}
	pool, err := s.Store.QueueCandidates(ch, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range pool {
		if c.AccountID == next[0].AccountID {
			p.success = false
			s.probe(context.Background(), c, s.settings())
			break
		}
	}
	if _, err = s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,blocked_until=0"); err != nil {
		t.Fatal(err)
	}
	p.success = true
	s.probe(context.Background(), next[0], s.settings())
	if tr.calls.Load() != 1 || p.calls.Load() != 2 {
		t.Fatalf("auth=%d probes=%d", tr.calls.Load(), p.calls.Load())
	}
}

func TestGlobalProbeCooldownDoesNotCreateDeferredAuditions(t *testing.T) {
	s, _, _, p, _ := fixture(t)
	until := time.Now().Add(time.Minute).UnixMilli()
	s.Store.DB.Exec("INSERT INTO provider_rates(provider,next_probe) VALUES('*',?)", until)
	s.step(context.Background())
	if p.calls.Load() != 0 || s.CheckerState().State != "probe_cooldown" {
		t.Fatal("selected media work during global cooldown", s.CheckerState())
	}
	var n int
	s.Store.DB.QueryRow("SELECT count(*) FROM work_leases WHERE kind='deferred'").Scan(&n)
	if n != 0 {
		t.Fatal("cooldown needlessly deferred individual candidates")
	}
}

func TestPausedScansSkipAllBackgroundWork(t *testing.T) {
	// With no catalogue or gateway attached, reaching either would fail the test.
	s := Service{Settings: func() config.Settings { return config.Settings{ScansPaused: true, ReplenishWhilePlaying: true} }, Live: func() bool { return true }}
	s.step(context.Background())
	if state := s.CheckerState().State; state != "scans_off" {
		t.Fatal(state)
	}
	s.Prepare(context.Background(), "empty-channel")
}
