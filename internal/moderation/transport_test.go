package moderation

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func Test429DefersWholeProviderWithoutCredentialFailure(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	settings := config.Settings{}
	settings.Defaults()
	calls := 0
	tr := &Transport{Store: s, Settings: func() config.Settings { return settings }, Base: rt(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"7200"}}, Body: io.NopCloser(strings.NewReader("limited"))}, nil
	})}
	x := ingest.Xtream{Client: &http.Client{Transport: tr}}
	a := ingest.Entry{Endpoint: "https://tv.example", Username: "u", Password: "p"}
	for i := 0; i < 2; i++ {
		_, e = x.Validate(context.Background(), a)
		var deferred DeferredError
		if !errors.As(e, &deferred) || deferred.Failure {
			t.Fatal("defer signal lost", e)
		}
	}
	if calls != 1 {
		t.Fatal("repeated request during cooldown", calls)
	}
	if !s.ProviderDeferred("tv.example", time.Now().Add(time.Hour).UnixMilli()) {
		t.Fatal("ignored Retry-After")
	}
}
func TestWaitCancellationDoesNotSendOrPenalise(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	settings := config.Settings{}
	settings.Defaults()
	s.ReserveCheck("tv.example", "api", time.Now().UnixMilli(), 2*time.Second, 10*time.Second)
	calls := 0
	tr := &Transport{Store: s, Settings: func() config.Settings { return settings }, Base: rt(func(r *http.Request) (*http.Response, error) { calls++; return nil, nil })}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", "https://tv.example/player_api.php", nil)
	_, e = tr.RoundTrip(r)
	if e == nil || calls != 0 || s.ProviderDeferred("tv.example", time.Now().UnixMilli()) {
		t.Fatal("cancelled pacing sent traffic or marked trouble", e, calls)
	}
}
func TestCDNLimitPropagatesToProbeOwner(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := WithProvider(context.Background(), "provider.example")
	r, _ := http.NewRequestWithContext(ctx, "GET", "https://cdn.example/segment.ts", nil)
	tr := &Transport{Store: s, ObserveOnly: true, Base: rt(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("busy"))}, nil
	})}
	_, e = tr.RoundTrip(r)
	if e == nil || !ProbeDeferred(ctx) || !s.ProviderDeferred("provider.example", time.Now().UnixMilli()) {
		t.Fatal("CDN deferral not propagated")
	}
}
func TestRetryAfterFormats(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		value string
		want  time.Time
	}{{"120", now.Add(2 * time.Minute)}, {now.Add(time.Hour).Format(http.TimeFormat), now.Add(time.Hour)}, {"nonsense", now}, {"-1", now}} {
		if got := RetryAfter(c.value, now); !got.Equal(c.want) {
			t.Fatal(c.value, got, c.want)
		}
	}
}

func TestCompletedProviderErrorsDifferFromPacing(t *testing.T) {
	for _, code := range []int{403, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			s, e := catalog.Open(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			tr := &Transport{Store: s, ObserveOnly: true, Base: rt(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: code, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader("fixture"))}, nil
			})}
			r, _ := http.NewRequest("GET", "https://fixture.example/api", nil)
			_, e = tr.RoundTrip(r)
			var deferred DeferredError
			if !errors.As(e, &deferred) || deferred.Failure != (code != 429) {
				t.Fatal(code, e, deferred)
			}
		})
	}
}

func TestNetworkTimeoutPreservesFailureAndSafeDeadline(t *testing.T) {
	s, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	v := config.Settings{}
	v.Defaults()
	tr := &Transport{Store: s, Settings: func() config.Settings { return v }, Base: rt(func(*http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "fixture-secret", IsTimeout: true}
	})}
	r, _ := http.NewRequest("GET", "https://timeout.example/player_api.php", nil)
	_, e = tr.RoundTrip(r)
	var deferred DeferredError
	if !errors.Is(e, context.DeadlineExceeded) || !errors.As(e, &deferred) || !deferred.Failure || strings.Contains(e.Error(), "fixture-secret") {
		t.Fatal(e)
	}
}
