// Package activity reports operational work without source credentials.
package activity

import (
	"log/slog"
	"sort"
	"sync"
	"time"
)

type Item struct {
	FailureKind string `json:"failure_kind,omitempty"`
	Phase       string `json:"phase,omitempty"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	ID          uint64 `json:"id"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	AccountID   int64  `json:"account_id,omitempty"`
	StreamID    int64  `json:"stream_id,omitempty"`
	Started     int64  `json:"started"`
	Finished    int64  `json:"finished,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
}
type Snapshot struct {
	Active    []Item         `json:"active"`
	Recent    []Item         `json:"recent"`
	Completed map[string]int `json:"completed"`
}
type Tracker struct {
	mu        sync.Mutex
	next      uint64
	active    map[uint64]Item
	recent    []Item
	completed map[string]int
}
type Job struct {
	tracker *Tracker
	id      uint64
	started time.Time
}

func New() *Tracker { return &Tracker{active: map[uint64]Item{}, completed: map[string]int{}} }
func (t *Tracker) Begin(kind, message string, account, stream int64) *Job {
	if t == nil {
		return &Job{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	t.active[t.next] = Item{ID: t.next, Kind: kind, Message: message, AccountID: account, StreamID: stream, Started: time.Now().Unix()}
	if kind == "account_discovery" || kind == "stream_check" || kind == "account_check" || kind == "manual_import" {
		slog.Debug("task started", "task", kind, "account_id", account, "stream_id", stream)
	}
	return &Job{tracker: t, id: t.next, started: time.Now()}
}
func (j *Job) Stage(message string) {
	if j.tracker == nil {
		return
	}
	t := j.tracker
	t.mu.Lock()
	defer t.mu.Unlock()
	if v, ok := t.active[j.id]; ok {
		v.Message = message
		t.active[j.id] = v
	}
}

// Detail accepts fixed diagnostic categories only, never provider response text.
func (j *Job) Detail(phase, kind string, status int) {
	if j == nil || j.tracker == nil {
		return
	}
	t := j.tracker
	t.mu.Lock()
	defer t.mu.Unlock()
	if v, ok := t.active[j.id]; ok {
		v.Phase = phase
		v.FailureKind = kind
		v.HTTPStatus = status
		t.active[j.id] = v
	}
}
func (j *Job) Finish(outcome string) {
	if j.tracker == nil {
		return
	}
	t := j.tracker
	t.mu.Lock()
	v, ok := t.active[j.id]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.active, j.id)
	v.Finished = time.Now().Unix()
	v.Outcome = outcome
	t.recent = append([]Item{v}, t.recent...)
	if len(t.recent) > 12 {
		t.recent = t.recent[:12]
	}
	if outcome != "cancelled" && outcome != "paused" && outcome != "deferred" {
		t.completed[v.Kind]++
	}
	t.mu.Unlock()
	// Do not log free-form messages: imported material can contain credentials.
	// Frequent probes/cooldowns stay at DEBUG; manual imports are INFO. Recovery events are logged by the asynchronous
	// catalogue writer, keeping normal stdout writes off the media event loop.
	attrs := []any{"task", v.Kind, "outcome", outcome, "account_id", v.AccountID, "stream_id", v.StreamID, "duration_seconds", v.Finished - v.Started, "duration_ms", time.Since(j.started).Milliseconds(), "failure_kind", v.FailureKind, "phase", v.Phase, "http_status", v.HTTPStatus}
	if outcome == "timeout" {
		slog.Warn("task timed out", attrs...)
	} else if v.Kind == "manual_import" {
		slog.Info("task finished", attrs...)
	} else {
		slog.Debug("task finished", attrs...)
	}
}
func (t *Tracker) Snapshot() Snapshot {
	out := Snapshot{Active: []Item{}, Recent: []Item{}, Completed: map[string]int{}}
	if t == nil {
		return out
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, v := range t.active {
		out.Active = append(out.Active, v)
	}
	sort.Slice(out.Active, func(i, j int) bool { return out.Active[i].ID < out.Active[j].ID })
	out.Recent = append(out.Recent, t.recent...)
	for k, v := range t.completed {
		out.Completed[k] = v
	}
	return out
}
