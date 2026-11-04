// Package roster coordinates a bounded verified pool; it never scans the whole catalogue on a cycle.
package roster

import (
	"antennarr/internal/activity"
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/checkcpu"
	"antennarr/internal/config"
	"antennarr/internal/media"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Service struct {
	loggedWaitState string
	loggedWaitNext  int64
	health          CheckerState
	pauseMu         sync.Mutex
	workMu          sync.Mutex
	paused          atomic.Bool
	Store           *catalog.Store
	Settings        func() config.Settings
	Activity        *activity.Tracker
	Budget          *budget.Budget
	Client          *http.Client
	Binary          string
	Gateway         interface {
		Probe(context.Context, string, string, time.Duration) media.ProbeResult
	}
	Live           func() bool
	ActiveChannels func() map[string]bool
	LivePairReady  func(string) bool
	replenishing   bool
	mu             sync.Mutex
	cancel         context.CancelFunc
	claimed        map[int64]bool
	excluded       map[int64]int64
	dirty          map[int64]bool
	emergency      map[string]bool
	cleanupNeeded  bool
	lastCleanup    int64
	cursor         int
	turn           int
	recruitAccount int64
	recruitVisited map[string]bool
}

func (s *Service) Preempt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) settings() config.Settings {
	var v config.Settings
	v.Defaults()
	if s.Settings != nil {
		v = s.Settings()
	}
	return v
}
func (s *Service) RequestDiscovery(ch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emergency == nil {
		s.emergency = map[string]bool{}
	}
	if len(s.emergency) < 16 {
		s.emergency[ch] = true
	}
}
func (s *Service) Prepare(ctx context.Context, ch string) {
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		rows, e := s.Candidates(ch, false)
		if e != nil || len(rows) > 0 {
			return
		}
		s.RequestDiscovery(ch)
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
	}
}
func (s *Service) Invalidate(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.excluded == nil {
		s.excluded = map[int64]int64{}
	}
	if s.dirty == nil {
		s.dirty = map[int64]bool{}
	}
	s.excluded[id] = time.Now().Unix()
	s.dirty[id] = true
}
func (s *Service) Record(id int64, r catalog.Result) error {
	var e error
	if r.Success && s.Live != nil && s.Live() && s.ActiveChannels != nil {
		e = s.Store.RecordForChannels(id, r, s.ActiveChannels())
	} else {
		e = s.Store.Record(id, r)
	}
	if e == nil && !r.Success {
		s.mu.Lock()
		delete(s.dirty, id)
		delete(s.excluded, id)
		s.mu.Unlock()
	}
	return e
}
func (s *Service) Event(kind, ch string, id int64, reason string) {
	s.Store.Event(kind, ch, id, reason)
}
func (s *Service) RaceSize(ch string) int {
	p, e := s.Store.EffectivePolicy(ch)
	if e != nil {
		return 5
	}
	return p.Race
}
func (s *Service) UHDAllowed() bool { return s.Store.UHDAllowed() }
func (s *Service) PreferDiversity(ch string) bool {
	p, e := s.Store.EffectivePolicy(ch)
	return e == nil && p.Preference == "diversity"
}
func (s *Service) Candidates(ch string, healthy bool) ([]catalog.Candidate, error) {
	rows, e := s.Store.RosterMembers(ch)
	if e != nil {
		return nil, e
	}
	out := s.availableCandidates(rows)
	if !healthy && len(out) < 2 {
		extra, e := s.Store.QueueCandidates(ch, 20)
		if e != nil {
			return nil, e
		}
		out = append(out, s.availableCandidates(extra)...)
		if len(out) < 2 {
			s.RequestDiscovery(ch)
		}
	}
	return out, nil
}

// Temporary capacity or an existing local claim affects selection, not roster
// membership. Otherwise a full saved roster could block usable queued reserves.
func (s *Service) availableCandidates(rows []catalog.Candidate) []catalog.Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []catalog.Candidate{}
	now := time.Now().Unix()
	for _, c := range rows {
		bad, exists := s.excluded[c.StreamID]
		if exists && (c.LastSuccess > bad || now-bad > 3600) {
			delete(s.excluded, c.StreamID)
			exists = false
		}
		if c.Limit > 0 && !s.claimed[c.StreamID] && !exists {
			out = append(out, c)
		}
	}
	return out
}

// Live acquisition is in memory: a SQLite writer must never stall the transport
// controller. The serial checker takes this same claim before its persistent lease.
func (s *Service) ClaimPlayback(id int64) (func(), bool) {
	s.mu.Lock()
	if s.claimed == nil {
		s.claimed = map[int64]bool{}
	}
	if s.claimed[id] {
		s.mu.Unlock()
		return nil, false
	}
	s.claimed[id] = true
	s.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); delete(s.claimed, id); s.mu.Unlock() }) }, true
}
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.step(ctx)
		}
	}
}
func (s *Service) flushFailures() {
	s.mu.Lock()
	ids := []int64{}
	for id := range s.dirty {
		ids = append(ids, id)
		if len(ids) == 4 {
			break
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		err := s.Store.RosterObserved(id, false)
		s.report("Save observed source failure", err)
		if err == nil {
			s.mu.Lock()
			delete(s.dirty, id)
			delete(s.excluded, id)
			s.mu.Unlock()
		}
	}
}
func (s *Service) WithPaused(fn func() error) error {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.paused.Store(true)
	s.Preempt()
	s.workMu.Lock()
	defer func() { s.workMu.Unlock(); s.paused.Store(false) }()
	return fn()
}
func (s *Service) step(parent context.Context) {
	if s.paused.Load() {
		s.state("paused", 0)
		return
	}
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if s.paused.Load() {
		s.state("paused", 0)
		return
	}
	s.state("selecting", 0)
	ctx, release, e := checkcpu.Enter(parent)
	if e != nil {
		s.report("CPU affinity unavailable", e)
		return
	}
	defer release()
	defer s.pruneFailedAccounts()
	s.flushFailures()
	live := (s.Live != nil && s.Live()) || s.Budget.Snapshot().Live > 0
	v := s.settings()
	now := time.Now().Unix()
	var active map[string]bool
	if live && s.ActiveChannels != nil {
		active = s.ActiveChannels()
	}
	s.mu.Lock()
	emergency := false
	for ch := range s.emergency {
		if active != nil && !active[ch] {
			delete(s.emergency, ch)
			continue
		}
		rows, err := s.Store.RosterMembers(ch)
		available := 0
		for _, c := range rows {
			if c.Limit > 0 {
				available++
			}
		}
		if (s.LivePairReady != nil && s.LivePairReady(ch)) || (err == nil && available >= 2) {
			delete(s.emergency, ch)
		} else {
			emergency = true
		}
	}
	s.mu.Unlock()
	if live && v.ReplenishWhilePlaying && s.ActiveChannels != nil {
		s.replenishing = true
		defer func() { s.replenishing = false }()
		s.replenishActive(ctx, v)
		return
	}
	if live {
		s.state("playback_priority", 0)
		if emergency {
			s.discover(ctx, v, true)
		}
		return
	}
	if now-s.lastCleanup >= 60 {
		s.lastCleanup = now
		s.report("Roster reconciliation", s.Store.ReconcileRosters())
		cleanupErr := s.Budget.WithProtectedAccounts(func(protected []int64) error {
			// Provider-wide deletion predates per-account protection. Defer it
			// if a reservation appeared after the idle check above.
			if len(protected) == 0 {
				if _, err := s.Store.PruneDeadProviders(); err != nil {
					return err
				}
			}
			result, err := s.Store.PruneCatalogue(protected)
			if err == nil && result.Accounts+result.Streams > 0 {
				slog.Info("catalogue cleaned", "accounts_deleted", result.Accounts, "streams_deleted", result.Streams)
			}
			return err
		})
		s.report("Retired source cleanup", cleanupErr)
		s.report("Catalogue housekeeping", s.Store.Housekeep())
	}
	if e := s.Store.EnsureRosterPass(now); e != nil {
		s.report("Prepare roster pass", e)
		return
	}
	summary, e := s.Store.RosterState()
	if e != nil {
		s.report("Read roster state", e)
		return
	}
	// Global pacing applies before candidate selection. Previously a future slot
	// generated a deferred job every tick, obscuring real progress and deferring
	// streams unnecessarily. Catalogue discovery has its own independent budget.
	var nextProbe int64
	if err := s.Store.DB.QueryRow("SELECT coalesce((SELECT next_probe FROM provider_rates WHERE provider='*'),0)").Scan(&nextProbe); err != nil {
		s.report("Read probe schedule", err)
		return
	}
	if nextProbe > time.Now().UnixMilli() {
		s.state("probe_cooldown", (nextProbe+999)/1000)
		if summary.Deficit > 0 {
			s.discover(ctx, v, false)
		}
		return
	}
	s.state("waiting_candidates", 0)
	// Reserve every third turn for overdue maintenance, so a difficult empty
	// channel cannot monopolise the checker indefinitely.
	s.turn++
	if s.turn%3 == 0 {
		if s.maintainPass(ctx, now, v) {
			return
		}
	}
	// Continue the current account across vacancies, one paced probe per turn.
	// Maintenance above retains its reserved turn, and live playback preempts us.
	if s.recruitAccount != 0 && s.recruitNext(ctx, summary.Channels, v) {
		return
	}
	// Maintain the ready pool first; deficits below two get a fair urgent fill turn.
	channels := summary.Channels
	sort.SliceStable(channels, func(i, j int) bool { return (channels[i].Count < 2 && channels[j].Count >= 2) })
	if len(channels) > 0 {
		for n := 0; n < len(channels); n++ {
			i := (s.cursor + n) % len(channels)
			ch := channels[i]
			if ch.Count >= min(2, ch.Policy.Target) {
				continue
			}
			pool, e := s.Store.QueueCandidates(ch.ChannelID, 1)
			if e == nil && len(pool) > 0 {
				s.cursor = (i + 1) % len(channels)
				s.startRecruitment(ctx, ch.ChannelID, pool[0], v)
				return
			}
		}
	}
	if summary.Deficit == 0 || s.turn%3 == 0 {
		if s.maintainPass(ctx, now, v) {
			return
		}
	}
	if summary.Deficit == 0 {
		pass, err := s.Store.CheckPassStatus()
		if err != nil {
			s.report("Read waiting pass", err)
			return
		}
		if pass.Remaining > 0 {
			next, err := s.Store.NextRosterCheckAt(now)
			if err != nil {
				s.report("Read provider retry window", err)
				return
			}
			s.state("checks_waiting", next)
		} else {
			s.state("rosters_full", 0)
		}
		return
	}
	for n := 0; n < len(channels); n++ {
		i := (s.cursor + n) % len(channels)
		ch := channels[i]
		if ch.Count >= ch.Policy.Target {
			continue
		}
		pool, e := s.Store.QueueCandidates(ch.ChannelID, 1)
		if e == nil && len(pool) > 0 {
			s.cursor = (i + 1) % len(channels)
			s.startRecruitment(ctx, ch.ChannelID, pool[0], v)
			return
		}
	}
	if s.discover(ctx, v, false) {
		return
	}
	s.maintainPass(ctx, now, v)
}
func (s *Service) operation(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, 70*time.Second)
	s.mu.Lock()
	s.cancel = cancel
	if s.paused.Load() {
		cancel()
	}
	s.mu.Unlock()
	return ctx, func() { cancel(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }
}

// Pending pass work is independent of recruitment. One maintenance turn in three
// prevents hard-to-fill channels from starving existing roster checks.
func (s *Service) maintainPass(ctx context.Context, now int64, v config.Settings) bool {
	c, access, e := s.Store.NextRosterCheck(now)
	if e != nil {
		if e != sql.ErrNoRows {
			s.report("Select roster pass work", e)

		}
		return false
	}
	if access {
		s.checkRosterAccess(ctx, c)
	} else {
		s.probe(ctx, c, v)
	}
	return true
}

// Only vacancies of playing channels are auditioned during playback. Neither
// saved roster passes nor idle-channel recruitment run through this path.
func (s *Service) replenishActive(ctx context.Context, v config.Settings) {
	active := s.ActiveChannels()
	summary, err := s.Store.RosterState()
	if err != nil {
		s.report("Read live recruitment vacancies", err)
		return
	}
	var channels []catalog.ChannelRoster
	for _, ch := range summary.Channels {
		if active[ch.ChannelID] && ch.Count < ch.Policy.Target {
			channels = append(channels, ch)
		}
	}
	if len(channels) == 0 {
		s.state("playback_priority", 0)
		return
	}
	s.state("live_replenishment", 0)
	if s.recruitAccount != 0 && s.recruitNext(ctx, channels, v) {
		return
	}
	for n := 0; n < len(channels); n++ {
		i := (s.cursor + n) % len(channels)
		ch := channels[i]
		pool, err := s.Store.QueueCandidates(ch.ChannelID, 1)
		if err != nil {
			s.report("Select live replenishment candidate", err)
			return
		}
		if len(pool) > 0 {
			s.cursor = i + 1
			s.startRecruitment(ctx, ch.ChannelID, pool[0], v)
			return
		}
	}
	// Metadata discovery is necessary to find candidates for the playing channels.
	// Media auditions remain scoped to those channels on the next turn.
	s.discover(ctx, v, false)
}
