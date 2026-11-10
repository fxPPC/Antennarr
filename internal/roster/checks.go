// Account/catalogue requests and media probes share cached authentication and
// persistent provider budgets. Cancellation never becomes a source failure.
package roster

import (
	"antennarr/internal/activity"
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"antennarr/internal/moderation"
	"antennarr/internal/netguard"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func (s *Service) discover(parent context.Context, v config.Settings, urgent bool) (started bool) {
	now := time.Now().Unix()
	a, e := s.Store.QueueAccount(now, time.Duration(v.CatalogueHours)*time.Hour)
	if e != nil {
		if e != sql.ErrNoRows {
			s.report("Select queued account", e)
		}
		return
	}
	if !s.Store.Claim("account", a.ID, now) {
		return
	}
	defer s.Store.ReleaseClaim("account", a.ID)
	reserve := s.Budget.Reserve
	if s.replenishing {
		reserve = func(id int64, p string, l int, _ bool) (func(), bool) { return s.Budget.ReserveReplenishment(id, p, l) }
	}
	release, ok := reserve(a.ID, catalog.ProviderKey(a.Provider), a.Limit, urgent)
	if !ok {
		return
	}
	defer release()
	ctx, done := s.operation(parent)
	defer done()
	s.state("discovering", 0)
	started = true
	job := s.Activity.Begin("account_discovery", fmt.Sprintf("Discovering selected channels on account #%d", a.ID), a.ID, 0)
	outcome := "failed"
	defer func() { job.Finish(outcome) }()
	entry, e := s.Store.AccountSecret(a.ID)
	if e != nil {
		job.Detail("access", "credential_unavailable", 0)
		s.accountFailure(a.ID, false)
		return
	}
	job.Stage(fmt.Sprintf("Checking account access on account #%d", a.ID))
	if ok, result := s.checkAccount(ctx, a.ID, true, false, job); !ok {
		outcome = result
		return
	}
	var cached int64
	if s.Store.DB.QueryRow("SELECT catalogue_checked FROM accounts WHERE id=?", a.ID).Scan(&cached) != nil {
		return
	}
	if cached > now-int64(v.CatalogueHours)*3600 {
		if e = s.Store.AccountUsable(a.ID); e != nil {
			s.report("Save account recovery", e)
			return
		}
		s.recruitAccount = a.ID
		s.recruitVisited = map[string]bool{}
		outcome = "healthy"
		return
	}
	var deferred moderation.DeferredError
	job.Stage(fmt.Sprintf("Fetching channel catalogue on account #%d", a.ID))
	rows, e := (ingest.Xtream{Client: s.Client}).Live(ctx, entry)
	if ctx.Err() == context.Canceled {
		outcome = "paused"
		return
	}
	if errors.As(e, &deferred) && !deferred.Failure {
		job.Detail("catalogue", ingest.FailureKind(e), ingest.FailureStatus(e))
		outcome = "deferred"
		return
	}
	if e != nil {
		kind, status := ingest.FailureKind(e), ingest.FailureStatus(e)
		job.Detail("catalogue", kind, status)
		if kind == "oversized_catalogue" || kind == "oversized_response" {
			s.report("Save catalogue limit", s.Store.DeferCatalogue(a.ID, kind))
			outcome = "deferred"
			return
		}
		s.accountFailureReason(a.ID, false, kind, "catalogue", status, true)
		if netguard.IsTimeout(e) {
			outcome = "timeout"
		}
		return
	}
	relevant, e := s.Store.RelevantEntries(rows)
	if e != nil {
		job.Detail("catalogue", "local_matching_error", 0)
		s.report("Match provider catalogue", e)
		return
	}
	if len(relevant) > 0 {
		if _, e = s.Store.Import(relevant, "Provider catalogue"); e != nil {
			job.Detail("catalogue", "local_catalogue_error", 0)
			s.report("Import provider catalogue", e)
			return
		}
	}
	if err := s.Store.CatalogueChecked(a.ID, now); err != nil {
		s.report("Save catalogue discovery", err)
		return
	}
	s.report("Save provider response", s.Store.ProviderResponsive(a.Provider))
	outcome = "discovered"
	s.recruitAccount = a.ID
	s.recruitVisited = map[string]bool{}
	s.mu.Lock()
	s.emergency = map[string]bool{}
	s.mu.Unlock()
	return
}
func (s *Service) probe(parent context.Context, c catalog.Candidate, v config.Settings) {
	if c.UHD && !s.UHDAllowed() {
		return
	}
	releaseLive, ok := s.ClaimPlayback(c.StreamID)
	if !ok {
		return
	}
	defer releaseLive()
	now := time.Now().Unix()
	if !s.Store.Claim("stream", c.StreamID, now) {
		return
	}
	defer s.Store.ReleaseClaim("stream", c.StreamID)
	reserve := s.Budget.Reserve
	if s.replenishing {
		reserve = func(id int64, p string, l int, _ bool) (func(), bool) { return s.Budget.ReserveReplenishment(id, p, l) }
	}
	release, ok := reserve(c.AccountID, catalog.ProviderKey(c.Provider), c.Limit, false)
	if !ok {
		return
	}
	defer release()
	ctx, done := s.operation(parent)
	defer done()
	s.state("checking", 0)
	job := s.Activity.Begin("stream_check", fmt.Sprintf("Auditioning or checking stream #%d", c.StreamID), c.AccountID, c.StreamID)
	outcome := "failed"
	defer func() { job.Finish(outcome) }()
	if ok, result := s.checkAccount(ctx, c.AccountID, false, true, job); !ok {
		outcome = result
		return
	}
	ready, e := moderation.WaitProbe(ctx, s.Store, c.Provider, v)
	if e != nil || !ready {
		outcome = "deferred"
		if netguard.IsTimeout(e) {
			outcome = "timeout"
		}
		// Provider pacing leaves this pass item pending.
		return
	}
	ctx = moderation.WithProvider(ctx, c.Provider)
	result := s.Gateway.Probe(ctx, s.Binary, c.URL, time.Duration(v.ProbeSeconds)*time.Second)
	if ctx.Err() != nil {
		outcome = "paused"
		if netguard.IsTimeout(ctx.Err()) {
			outcome = "timeout"
		}
		return
	}
	if moderation.ProbeDeferred(ctx) || s.Store.ProviderDeferred(c.Provider, time.Now().UnixMilli()) {
		outcome = "deferred"
		// Provider pacing leaves this pass item pending.
		return
	}
	if v.AllowUHD != s.UHDAllowed() {
		outcome = "paused"
		return
	}
	if !result.Success {
		kind := "media_invalid"
		if result.TimedOut {
			kind = "timeout"
		}
		job.Detail("media", kind, 0)
		slog.Debug("media probe result", "account_id", c.AccountID, "stream_id", c.StreamID, "failure_kind", kind, "duration_ms", result.DurationMS, "startup_ms", result.StartupMS, "reason", result.Reason)
	}
	if result.TimedOut {
		outcome = "timeout"
	}
	if result.Success {
		outcome = "healthy"
		s.Store.ProviderResponsive(c.Provider)
	}
	if e = s.Record(c.StreamID, catalog.Result{Success: result.Success, StartupMS: result.StartupMS, DurationMS: result.DurationMS, Reason: result.Reason, Codec: result.Codec, Width: result.Width, Height: result.Height}); e != nil {
		outcome = "failed"
	}
	s.report("Save stream result", e)
	if e == nil {
		s.report("Complete roster check", s.Store.FinishRosterCheck(c.StreamID))
	}
}

// Login metadata is cached for one traversal, not for a wall-clock interval.
// Discovery deliberately preserves the streak until the catalogue also succeeds.
func (s *Service) checkAccount(ctx context.Context, id int64, force, reset bool, job *activity.Job) (bool, string) {
	var kind, state string
	var seen, cached int
	if e := s.Store.DB.QueryRow("SELECT kind,state,catalogue_checked FROM accounts WHERE id=?", id).Scan(&kind, &state, &cached); e != nil {
		s.report("Read account metadata", e)
		return false, "failed"
	}
	if state == "retired" || state == "manually_blocked" {
		return false, "deferred"
	}
	if kind != "xtream" {
		if e := s.Store.MarkPassAccount(id); e != nil {
			s.report("Save pass access state", e)
			return false, "failed"
		}
		return true, "healthy"
	}
	if e := s.Store.DB.QueryRow("SELECT count(*) FROM roster_pass_accounts WHERE account_id=?", id).Scan(&seen); e != nil {
		s.report("Read pass access state", e)
		return false, "failed"
	}
	if !force && seen > 0 {
		return true, "healthy"
	}
	entry, e := s.Store.AccountSecret(id)
	if e != nil {
		job.Detail("access", "credential_unavailable", 0)
		s.accountFailure(id, false)
		return false, "failed"
	}
	info, e := (ingest.Xtream{Client: s.Client}).Validate(ctx, entry)
	var deferred moderation.DeferredError
	if ctx.Err() == context.Canceled {
		return false, "paused"
	}
	if errors.As(e, &deferred) && !deferred.Failure {
		job.Detail("access", ingest.FailureKind(e), ingest.FailureStatus(e))
		return false, "deferred"
	}
	if e != nil || !info.Active {
		kind, status := ingest.FailureKind(e), ingest.FailureStatus(e)
		if e == nil {
			kind = info.FailureKind
		}
		job.Detail("access", kind, status)
		s.accountFailureReason(id, e == nil && !info.Active, kind, "access", status)
		if netguard.IsTimeout(e) {
			return false, "timeout"
		}
		return false, "failed"
	}
	// The provider includes our existing live feeds in active_cons. They are
	// already budgeted locally and must not be counted again as external use.
	owned := max(0, s.Budget.AccountUsage(id)-1)
	reported := info.ActiveConnections
	info.ActiveConnections = max(0, reported-owned)
	slog.Debug("account capacity checked", "account_id", id, "reported_connections", reported, "local_connections", owned, "external_connections", info.ActiveConnections, "connection_limit", info.Limit)
	if e = s.Store.AccountMetadata(id, info.Limit, info.Expires, info.ActiveConnections); e != nil {
		s.report("Save account metadata", e)
		return false, "failed"
	}
	// A provider may report our own primary/backup as occupying its last slot.
	// Defer extra probing without treating our playback as an external failure.
	if reported >= max(1, info.Limit) && owned > 0 {
		job.Detail("access", "local_capacity", 0)
		return false, "deferred"
	}
	// An authenticated account without a free connection cannot supply a fallback.
	// Release memberships without a strike so vacancies can recruit usable sources.
	// Ordinary cancellation and our own live connections are handled above.
	if info.ActiveConnections >= max(1, info.Limit) {
		job.Detail("access", "full_connections", 0)
		s.report("Return full account to queue", s.Store.AccountFull(id))
		return false, "unavailable"
	}
	// Cached catalogue plus refreshed access is a complete usable account check.
	if reset && cached > 0 {
		if e = s.Store.AccountUsable(id); e != nil {
			s.report("Save account success", e)
			return false, "failed"
		}
	}
	if e = s.Store.MarkPassAccount(id); e != nil {
		s.report("Save pass access state", e)
		return false, "failed"
	}
	return true, "healthy"
}
func (s *Service) accountFailure(id int64, inactive bool, catalogue ...bool) {
	s.accountFailureReason(id, inactive, "credential_unavailable", "access", 0, catalogue...)
}
func (s *Service) accountFailureReason(id int64, inactive bool, kind, phase string, status int, catalogue ...bool) {
	e := s.Store.DiscoveryFailedReason(id, inactive, kind, phase, status, catalogue...)
	slog.Debug("account check failed", "account_id", id, "failure_kind", kind, "phase", phase, "http_status", status)
	s.report("Save failed account attempt", e)
	if e == nil {
		s.cleanupNeeded = true
	}
}
func (s *Service) pruneFailedAccounts() {
	if !s.cleanupNeeded {
		return
	}
	s.cleanupNeeded = false
	s.report("Reconcile failed accounts", s.Store.ReconcileRosters())
	e := s.Budget.WithProtectedAccounts(func(ids []int64) error { _, e := s.Store.PruneCatalogue(ids); return e })
	s.report("Delete failed accounts", e)
}

// Check access once per pass even when cached metadata says all slots are busy.
func (s *Service) checkRosterAccess(ctx context.Context, c catalog.Candidate) {
	release, ok := s.Budget.Reserve(c.AccountID, catalog.ProviderKey(c.Provider), 20, false)
	if !ok {
		return
	}
	defer release()
	op, done := s.operation(ctx)
	defer done()
	s.state("discovering", 0)
	job := s.Activity.Begin("account_check", fmt.Sprintf("Refreshing access for roster account #%d", c.AccountID), c.AccountID, 0)
	ok, outcome := s.checkAccount(op, c.AccountID, true, true, job)
	job.Finish(outcome)
	if !ok {
		return
	}
}
