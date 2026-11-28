// This fixture runs against the previous release's source using a Go overlay.
package alpha12

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"antennarr/internal/catalog"
	"antennarr/internal/ingest"
)

func TestSeedPreviousAlpha(t *testing.T) {
	root := os.Getenv("ANTENNARR_UPGRADE_SEED")
	if root == "" {
		t.Skip("requires disposable previous-alpha appdata")
	}
	t.Setenv("ANTENNARR_ALPHA_SEED", root)
	TestSeedJellyfinDVR(t)
	store, err := catalog.OpenWithDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := ingest.PortalAccount{
		PortalURL:   "https://upgrade.example.invalid/c",
		PlaylistURL: "https://upgrade.example.invalid/subscriber.m3u",
		MAC:         "02:00:00:00:00:42",
	}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		const playlist = "#EXTM3U\n#EXTINF:-1 tvg-id=\"UPGRADE\",Subscription fixture\nhttps://upgrade.example.invalid/stream.ts\n"
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(playlist)), Request: r}, nil
	})}
	channels, err := cfg.Channels(context.Background(), client)
	if err != nil || len(channels) != 1 {
		t.Fatalf("fixture subscription: %d channels, %v", len(channels), err)
	}
	if _, err = store.SavePortalCatalogue(cfg, channels); err != nil {
		t.Fatal(err)
	}
}
