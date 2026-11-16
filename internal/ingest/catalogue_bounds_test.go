package ingest

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func catalogueReader(body string) Xtream {
	return Xtream{Client: &http.Client{Transport: roundtrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
}

func TestCatalogueLargeMetadataWithinEntryLimit(t *testing.T) {
	// Real catalogues include logos and fields Antennarr never stores. This exceeds
	// the old account-response ceiling while remaining well within the catalogue cap.
	row := `{"name":"CNN US","stream_id":"42","unused":"` + strings.Repeat("x", 1024) + `"}`
	body := "[" + strings.Repeat(row+",", 8999) + row + "]"
	rows, err := catalogueReader(body).Live(context.Background(), Entry{Endpoint: "https://fixture.example"})
	if err != nil || len(rows) != 9000 {
		t.Fatalf("rows=%d error=%v", len(rows), err)
	}
}

func TestCatalogueRejectsIncompleteOrNonArrayResponse(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[{"stream_id":1}`, `[] {}`, `[] secret`, `[{"stream_id":1},`} {
		rows, err := catalogueReader(body).Live(context.Background(), Entry{Endpoint: "https://fixture.example"})
		if FailureKind(err) != "malformed_response" || rows != nil {
			t.Fatalf("accepted malformed catalogue: %v", err)
		}
	}
}

func TestCatalogueByteBoundIncludesTrailingPadding(t *testing.T) {
	for _, body := range []string{
		`[{"stream_id":1,"unused":"` + strings.Repeat("x", maxCatalogueBytes) + `"}]`,
		`[]` + strings.Repeat(" ", maxCatalogueBytes),
	} {
		rows, err := catalogueReader(body).Live(context.Background(), Entry{Endpoint: "https://fixture.example"})
		if FailureKind(err) != "oversized_response" || rows != nil {
			t.Fatalf("rows=%d error=%v", len(rows), err)
		}
	}
}
