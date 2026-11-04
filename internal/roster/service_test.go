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

func TestRecruitmentUsesAccountAcrossVacanciesAndStopsAtFull(t *testing.T) {
	s, ch, cs, p, tr := fixture(t)
	other, e := s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Region: "US", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	entry, _ := ingest.FromURL("https://p0.example/cnn.ts", "CNN", "")
	if _, e = s.Store.Import([]ingest.Entry{entry}, "fixture"); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.Store.QueueCandidates(other, 1)
	if len(rows) != 1 || rows[0].AccountID != cs[0].AccountID {
		t.Fatal("fixture must share one account")
	}
	v := s.settings()
	s.startRecruitment(context.Background(), ch, cs[0], v)
	// Provider pacing must defer the next stream without losing account locality.
	s.Store.DB.Exec("INSERT INTO provider_rates(provider,next_probe) VALUES(?,?) ON CONFLICT(provider) DO UPDATE SET next_probe=excluded.next_probe", catalog.ProviderKey(cs[0].Provider), time.Now().Add(time.Hour).UnixMilli())
	summary, _ := s.Store.RosterState()
	if s.recruitNext(context.Background(), summary.Channels, v) || p.calls.Load() != 1 {
		t.Fatal("bypassed provider pacing")
	}
	s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,blocked_until=0")
	if !s.recruitNext(context.Background(), summary.Channels, v) || p.calls.Load() != 2 {
		t.Fatal("did not audition second channel on same account")
	}
	members, _ := s.Store.RosterMembers(other)
	if len(members) != 1 {
		t.Fatal("second channel not admitted")
	}
	if tr.calls.Load() != 0 {
		t.Fatal("unnecessary account/catalogue request")
	}
	// A full channel must never receive another audition.
	s.recruitAccount = cs[0].AccountID
	s.recruitVisited = map[string]bool{}
	for i := range summary.Channels {
		summary.Channels[i].Count = summary.Channels[i].Policy.Target
	}
	if s.recruitNext(context.Background(), summary.Channels, v) || p.calls.Load() != 2 {
		t.Fatal("probed full channel")
	}
}

func TestRecruitmentSkipsFailedAccount(t *testing.T) {
	s, _, cs, p, _ := fixture(t)
	s.recruitAccount = cs[0].AccountID
	s.recruitVisited = map[string]bool{}
	s.Store.DB.Exec("UPDATE accounts SET state='inactive' WHERE id=?", cs[0].AccountID)
	summary, _ := s.Store.RosterState()
	if s.recruitNext(context.Background(), summary.Channels, s.settings()) || s.recruitAccount != 0 || p.calls.Load() != 0 {
		t.Fatal("failed account was not abandoned")
	}
}

type authTransport struct{ calls atomic.Int32 }

func (t *authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"user_info":{"auth":1,"status":"Active","max_connections":"5","active_cons":"0"}}`)), Request: r}, nil
}
func TestAccountBatchAuthenticatesOnlyOnce(t *testing.T) {
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
	rows, _ := s.Store.Candidates(other, false)
	if len(rows) != 1 {
		t.Fatal("missing fixture")
	}
	aid := rows[0].AccountID
	first, _ := s.Store.QueueAccountCandidates(ch, aid)
	if len(first) != 1 {
		t.Fatal("missing first channel")
	}
	p.success = false // One bad channel must not discard the account.
	s.startRecruitment(context.Background(), ch, first[0], s.settings())
	p.success = true
	s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,blocked_until=0")
	summary, _ := s.Store.RosterState()
	if !s.recruitNext(context.Background(), summary.Channels, s.settings()) {
		t.Fatal("no second audition")
	}
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
