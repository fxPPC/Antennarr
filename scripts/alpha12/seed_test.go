// The fixture supplies XMLTV over an in-memory transport. The release server
// itself runs normally using its existing generated-media failover fixture.
package alpha12

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"antennarr/internal/catalog"
	"antennarr/internal/guide"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSeedJellyfinDVR(t *testing.T) {
	root := os.Getenv("ANTENNARR_ALPHA_SEED")
	if root == "" {
		t.Skip("requires disposable acceptance directory")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatalf("fixture directory must be new: %v", err)
	}
	store, err := catalog.OpenWithDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	// Give the real DVR time to ingest both tuners, schedule a future recording,
	// and refresh its guide after a channel edit before the timer fires.
	const stamp = "20060102150405 -0700"
	start := now.Add(-time.Minute).Format(stamp)
	handoff := now.Add(2 * time.Minute).Format(stamp)
	end := now.Add(210 * time.Second).Format(stamp)
	data := map[string]string{}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body, ok := data[r.URL.String()]
		if !ok {
			return nil, fmt.Errorf("unexpected fixture URL")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	service, err := guide.New(store, client)
	if err != nil {
		t.Fatal(err)
	}
	for i, market := range []struct{ preset, name, station, region string }{
		{"us-nyc", "5 New York Fixture", "NYFIX", "US"},
		{"uk-london", "News, London Fixture", "LONFIX", "GB"},
	} {
		profile, err := store.AddProfilePreset(market.preset)
		if err != nil {
			t.Fatal(err)
		}
		profile.Enabled = true
		if _, err = store.SaveProfile(profile); err != nil {
			t.Fatal(err)
		}
		ch := catalog.LineupChannel{ID: fmt.Sprintf("test-%d", i+1), Number: "7", Name: market.name, Region: market.region, Enabled: true}
		if err = store.ImportLineupForProfile(profile.ID, catalog.Lineup{Version: 1, Channels: []catalog.LineupChannel{ch}}); err != nil {
			t.Fatal(err)
		}
		source := fmt.Sprintf("https://guide%d.example/listings.xml", i)
		data[source] = fmt.Sprintf(`<tv>
            <channel id="%[1]s"><display-name>%[2]s</display-name></channel>
            <programme start="%[3]s" stop="%[4]s" channel="%[1]s"><title>%[2]s Preview</title></programme>
            <programme start="%[4]s" stop="%[5]s" channel="%[1]s"><title>%[2]s on Air</title></programme>
        </tv>`, market.station, market.name, start, handoff, end)
		if err = service.SetFeed(ctx, profile.ID, source); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	profiles, err := store.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		data, err := service.Programmes(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, list := range data {
			for _, programme := range list {
				if strings.Contains(programme.Title, "Fixture on Air") {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("missing scheduled programme for %s: %+v", p.ID, data)
		}
	}
	t.Log("Seeded disposable catalogue", filepath.Base(root))
}
