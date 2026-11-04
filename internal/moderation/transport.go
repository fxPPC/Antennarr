// Package moderation applies persistent background-check pacing, never live feeds.
package moderation

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/netguard"
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type ownerKey struct{}
type ownerState struct {
	key      string
	deferred atomic.Bool
}

func WithProvider(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, ownerKey{}, &ownerState{key: catalog.ProviderKey(provider)})
}
func ProbeDeferred(ctx context.Context) bool {
	o, ok := ctx.Value(ownerKey{}).(*ownerState)
	return ok && o.deferred.Load()
}

// Failure distinguishes a completed failed request from pacing or HTTP 429.
type DeferredError struct {
	Failure bool
	Kind    string
	Status  int
}

func (e DeferredError) FailureKind() string {
	if e.Kind != "" {
		return e.Kind
	}
	if e.Failure {
		return "transport_error"
	}
	return "provider_pacing"
}
func (e DeferredError) HTTPStatus() int { return e.Status }

func (DeferredError) Error() string        { return "provider checks deferred" }
func (DeferredError) TemporaryCheck() bool { return true }

type Transport struct {
	Store       *catalog.Store
	Base        http.RoundTripper
	Settings    func() config.Settings
	ObserveOnly bool
}

func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	key := catalog.ProviderKey(r.URL.Host)
	deferred := func(failed ...bool) (*http.Response, error) {
		if owner, ok := r.Context().Value(ownerKey{}).(*ownerState); ok {
			owner.deferred.Store(true)
		}
		return nil, DeferredError{Failure: len(failed) > 0 && failed[0]}
	}
	trouble := func(until int64, limited bool) {
		now := time.Now().UnixMilli()
		if err := t.Store.ProviderTrouble(key, now, until, limited); err != nil {
			slog.Warn("provider cooldown could not be saved")
		}
		if owner, ok := r.Context().Value(ownerKey{}).(*ownerState); ok && owner.key != key {
			if err := t.Store.ProviderTrouble(owner.key, now, until, limited); err != nil {
				slog.Warn("source cooldown could not be saved")
			}
		}
	}
	if !t.ObserveOnly {
		settings := t.Settings()
		for {
			now := time.Now()
			ready, e := t.Store.ReserveCheck(key, "api", 0, jitter(settings.CheckGlobalSeconds), jitter(settings.CheckAPISeconds))
			if e != nil {
				return deferred()
			}
			if ready == 0 {
				break
			}
			if t.Store.ProviderDeferred(key, now.UnixMilli()) {
				return deferred()
			}
			timer := time.NewTimer(time.Until(time.UnixMilli(ready)))
			select {
			case <-r.Context().Done():
				timer.Stop()
				return nil, r.Context().Err()
			case <-timer.C:
			}
		}
	} else if t.Store.ProviderDeferred(key, time.Now().UnixMilli()) {
		return deferred()
	}
	response, e := t.Base.RoundTrip(r)
	if e != nil {
		if r.Context().Err() != nil {
			return nil, e
		}
		trouble(0, false)
		if netguard.IsTimeout(e) {
			_, failed := deferred(true)
			return nil, errors.Join(failed, context.DeadlineExceeded)
		}
		return deferred(true)
	}
	if response.StatusCode == 429 || response.StatusCode == 403 || response.StatusCode >= 500 {
		until := RetryAfter(response.Header.Get("Retry-After"), time.Now())
		response.Body.Close()
		trouble(until.UnixMilli(), response.StatusCode == 429)
		_, err := deferred(response.StatusCode != 429)
		marked := err.(DeferredError)
		marked.Status = response.StatusCode
		marked.Kind = "http_error"
		if marked.Status == 429 {
			marked.Kind = "rate_limited"
		}
		if marked.Status == 403 {
			marked.Kind = "access_denied"
		}
		return nil, marked
	}

	return response, nil
}
func jitter(seconds int) time.Duration {
	d := time.Duration(seconds) * time.Second
	return d + time.Duration(rand.Float64()*float64(d)/10)
}
func RetryAfter(value string, now time.Time) time.Time {
	if seconds, e := strconv.ParseInt(value, 10, 64); e == nil && seconds >= 0 && seconds <= int64((365*24*time.Hour)/time.Second) {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if date, e := http.ParseTime(value); e == nil && date.After(now) {
		return date
	}
	return now
}
func WaitProbe(ctx context.Context, s *catalog.Store, provider string, settings config.Settings) (bool, error) {
	ready, e := s.ReserveCheck(provider, "probe", 0, jitter(settings.CheckStreamGlobalSeconds), jitter(settings.CheckStreamSeconds))
	return ready == 0, e
}
