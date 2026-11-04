package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundtrip func(*http.Request) (*http.Response, error)

func (f roundtrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestXtreamMetadataOnlyAndSecretEncoding(t *testing.T) {
	calls := 0
	x := Xtream{Client: &http.Client{Transport: roundtrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("username") != "u&x" || r.URL.Query().Get("password") != "p/x" {
			t.Fatal("credentials not encoded")
		}
		body := `{"user_info":{"auth":1,"status":"Active","max_connections":"2","exp_date":"0","active_cons":"0"}}`
		if r.URL.Query().Get("action") == "get_live_streams" {
			body = `[{"name":"CNN US","stream_id":42,"epg_channel_id":"CNN.us"},{"name":"Unselected","stream_id":43}]`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	a := Entry{Kind: "xtream", Endpoint: "https://tv.example", Username: "u&x", Password: "p/x"}
	info, e := x.Validate(context.Background(), a)
	if e != nil || !info.Active || info.Limit != 2 {
		t.Fatal(info, e)
	}
	rows, e := x.Live(context.Background(), a)
	if e != nil || len(rows) != 2 || !strings.Contains(rows[0].URL, "u&x/p%2Fx/42.ts") {
		t.Fatal(rows, e)
	}
	if calls != 2 {
		t.Fatal("unexpected requests")
	}
}

type timeoutBody struct{}

func (timeoutBody) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (timeoutBody) Close() error             { return nil }
func TestCatalogueBodyTimeoutIsNotReportedAsSizeLimit(t *testing.T) {
	x := Xtream{Client: &http.Client{Transport: roundtrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: timeoutBody{}}, nil
	})}}
	_, e := x.Live(context.Background(), Entry{Endpoint: "https://fixture.example", Username: "fixture", Password: "fixture"})
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}

func TestCatalogueEntryLimit(t *testing.T) {
	for _, count := range []int{20001, 40000, 40001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			body := "[" + strings.Repeat(`{"name":"CNN","stream_id":42},`, count-1) + `{"name":"CNN","stream_id":43}]`
			x := Xtream{Client: &http.Client{Transport: roundtrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			entries, err := x.Live(context.Background(), Entry{Endpoint: "https://fixture.example"})
			if count > maxCatalogueEntries {
				if FailureKind(err) != "oversized_catalogue" {
					t.Fatalf("expected size rejection, got %v", err)
				}
			} else if err != nil || len(entries) != count {
				t.Fatalf("count=%d error=%v", len(entries), err)
			}
		})
	}
}
