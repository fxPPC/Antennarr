package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type providerTestTransport func(*http.Request) (*http.Response, error)

func (f providerTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestProviderFailureCategoriesAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, kind string
	}{{"unauthorised", 401, "secret", "access_denied"}, {"limited", 429, "secret", "rate_limited"}, {"server", 503, "secret", "http_error"}, {"malformed", 200, "secret", "malformed_response"}, {"oversized", 200, strings.Repeat("x", (8<<20)+1), "oversized_response"}} {
		t.Run(tc.name, func(t *testing.T) {
			x := Xtream{Client: &http.Client{Transport: providerTestTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}}
			_, e := x.Validate(context.Background(), Entry{Endpoint: "https://provider.example", Username: "private-user", Password: "private-password"})
			if FailureKind(e) != tc.kind {
				t.Fatal(e, FailureKind(e))
			}
			if tc.status != 200 && FailureStatus(e) != tc.status {
				t.Fatal(FailureStatus(e))
			}
			if strings.Contains(e.Error(), "secret") || strings.Contains(e.Error(), "private-") {
				t.Fatal("sensitive error text")
			}
		})
	}
}
func TestAccountStatusDistinguishesExpiredAndInvalid(t *testing.T) {
	for _, tc := range []struct {
		auth         int
		status, kind string
	}{{0, "Disabled", "invalid_credentials"}, {1, "Expired", "expired_account"}, {1, "Disabled", "inactive_account"}, {1, "Active", ""}} {
		x := Xtream{Client: &http.Client{Transport: providerTestTransport(func(r *http.Request) (*http.Response, error) {
			b := fmt.Sprintf(`{"user_info":{"auth":%d,"status":%q,"max_connections":2}}`, tc.auth, tc.status)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: make(http.Header)}, nil
		})}}
		v, e := x.Validate(context.Background(), Entry{Endpoint: "https://provider.example"})
		if e != nil || v.FailureKind != tc.kind {
			t.Fatal(v, e)
		}
	}
}

func TestTransportCauseCannotLeakThroughJSON(t *testing.T) {
	x := Xtream{Client: &http.Client{Transport: providerTestTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("private-password") })}}
	_, e := x.Validate(context.Background(), Entry{Endpoint: "https://provider.example", Username: "private-user", Password: "private-password"})
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-") || strings.Contains(e.Error(), "private-") {
		t.Fatal("transport cause exposed")
	}
}
