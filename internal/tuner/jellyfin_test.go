package tuner

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"antennarr/internal/catalog"
	"antennarr/internal/guide"
	"antennarr/internal/stream"
)

type sampleSchedule func(context.Context, string) (map[string][]guide.Programme, error)

func (s sampleSchedule) Programmes(ctx context.Context, profile string) (map[string][]guide.Programme, error) {
	return s(ctx, profile)
}

func tunerRequest(h http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestJellyfinGuideUsesStableScopedChannelIDs(t *testing.T) {
	f, london := fleetFixture(t)
	calls := 0
	f.Template.Guide = sampleSchedule(func(ctx context.Context, profile string) (map[string][]guide.Programme, error) {
		calls++
		if profile != london.ID {
			t.Fatalf("read other tuner's guide: %s", profile)
		}
		return map[string][]guide.Programme{
			"uk-test": {{Start: 1609459200, Stop: 1609462800, Title: "News <Live> & sport", Subtitle: "Episode 3", Description: "A & B\x00</programme><programme>", EpisodeSystem: "xmltv_ns", Episode: "1.2."}},
			"us-test": {{Start: 1609459200, Stop: 1609462800, Title: "Other city"}},
		}, nil
	})
	uk := f.Handler(london.PortOffset)
	m3u := tunerRequest(uk, "GET", "/playlist.m3u")
	lines := strings.Split(strings.TrimSpace(m3u.Body.String()), "\n")
	if len(lines) != 3 || lines[0] != "#EXTM3U" || !strings.Contains(lines[1], `tvg-chno="101",UK fixture`) {
		t.Fatalf("bad playlist: %s", lines)
	}
	media, err := url.Parse(lines[2])
	if err != nil || media.Host != "172.17.0.8:9200" || !strings.HasPrefix(media.Path, "/stream/antennarr-") {
		t.Fatal(lines[2], err)
	}
	id := strings.TrimPrefix(media.Path, "/stream/")
	if !strings.Contains(lines[1], `tvg-id="`+id+`"`) {
		t.Fatal(lines[1])
	}

	var tv struct {
		Channels   []xmlChannel   `xml:"channel"`
		Programmes []xmlProgramme `xml:"programme"`
	}
	xmlout := tunerRequest(uk, "GET", "/guide.xml")
	if xmlout.Code != 200 || calls != 1 {
		t.Fatal(xmlout.Code, calls)
	}
	if err := xml.Unmarshal(xmlout.Body.Bytes(), &tv); err != nil {
		t.Fatal(err)
	}
	if len(tv.Channels) != 1 || tv.Channels[0].Name != "UK fixture" || tv.Channels[0].ID != id {
		t.Fatal(tv.Channels)
	}
	if len(tv.Programmes) != 1 {
		t.Fatal(tv.Programmes)
	}
	show := tv.Programmes[0]
	if show.Title != "News <Live> & sport" || show.Subtitle != "Episode 3" || show.Desc != "A & B\uFFFD</programme><programme>" || show.Channel != id || show.Start != "20210101000000 +0000" || show.Stop != "20210101010000 +0000" || show.Episode == nil || show.Episode.System != "xmltv_ns" || show.Episode.Value != "1.2." {
		t.Fatal(show)
	}

	_, err = f.Store.SaveChannel(catalog.Channel{ID: "uk-test", ProfileID: london.ID, Number: "202", Name: "London & region", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	after := tunerRequest(uk, "GET", "/playlist.m3u").Body.String()
	if !strings.Contains(after, lines[2]) || !strings.Contains(after, `tvg-chno="202",London & region`) {
		t.Fatal(after)
	}
	if w := tunerRequest(uk, "HEAD", media.Path); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body)
	}
	if w := tunerRequest(f.PrimaryHandler(), "HEAD", media.Path); w.Code != 404 {
		t.Fatal("cross-tuner playback", w.Code)
	}
	for _, path := range []string{"/playlist.m3u", "/guide.xml", media.Path} {
		if w := tunerRequest(f.Template.AdminHandler(), "GET", path); w.Code != 404 {
			t.Fatal("exposed through admin listener", path, w.Code)
		}
		if w := tunerRequest(f.FallbackHandler(), "GET", path); w.Code != 404 {
			t.Fatal("exposed through fallback listener", path, w.Code)
		}
	}
	london.Enabled = false
	if _, err := f.Store.SaveProfile(london); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/playlist.m3u", "/guide.xml", media.Path} {
		if w := tunerRequest(uk, "GET", path); w.Code != 503 {
			t.Fatal("disabled tuner", path, w.Code)
		}
	}
	if calls != 1 {
		t.Fatal("unexpected schedule reads", calls)
	}
}

func TestJellyfinGuideFailureAndPlaylistNames(t *testing.T) {
	s := testServer(t)
	s.Config.Channels[0].Name = "Inline\r\n#EXTM3U\u2028name"
	s.Guide = sampleSchedule(func(context.Context, string) (map[string][]guide.Programme, error) {
		return nil, errors.New("private upstream URL")
	})
	h := s.TunerHandler()
	out := tunerRequest(h, "GET", "/guide.xml")
	if out.Code != 503 || strings.Contains(out.Body.String(), "private upstream") {
		t.Fatal(out.Code, out.Body)
	}
	list := tunerRequest(h, "GET", "/playlist.m3u").Body.String()
	if strings.Contains(list, "\r") || strings.Count(list, "\n") != 7 || !strings.Contains(list, "Inline  #EXTM3U name") {
		t.Fatal(list)
	}
}

type packetRecorder struct {
	*httptest.ResponseRecorder
	first chan struct{}
	once  sync.Once
}

func (w *packetRecorder) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.once.Do(func() { close(w.first) })
	return n, err
}

func TestMixedPlaybackCancellationsReleaseExactlyTheirOwnViewer(t *testing.T) {
	s := testServer(t)
	var starts atomic.Int32
	manager := stream.New(2, time.Millisecond, func(ctx context.Context, _ string, emit func([]byte)) error {
		starts.Add(1)
		clock := time.NewTicker(5 * time.Millisecond)
		defer clock.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-clock.C:
				emit([]byte("generated fixture packet"))
			}
		}
	})
	t.Cleanup(manager.Close)
	s.Streams = manager
	h := s.TunerHandler()

	view := func(path string) func() {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		ctx, cancel := context.WithCancel(req.Context())
		t.Cleanup(cancel)
		w := &packetRecorder{ResponseRecorder: httptest.NewRecorder(), first: make(chan struct{})}
		done := make(chan struct{})
		go func() { defer close(done); h.ServeHTTP(w, req.WithContext(ctx)) }()
		select {
		case <-w.first:
		case <-time.After(2 * time.Second):
			t.Fatal("viewer did not receive media", path)
		}
		return func() {
			t.Helper()
			cancel()
			select {
			case <-done:
				if w.Code != 200 || !strings.Contains(w.Body.String(), "generated fixture packet") {
					t.Fatal(w.Code, w.Body)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("viewer did not release on client disconnect")
			}
		}
	}
	plex := view("/auto/v901")
	jellyfin := view("/stream/" + s.broadcastID("test-1"))
	if st := manager.Status(); st.Viewers != 2 || st.Channels != 1 || starts.Load() != 1 {
		t.Fatal(st, starts.Load())
	}
	if w := tunerRequest(h, "GET", "/stream/"+s.broadcastID("test-2")); w.Code != 503 {
		t.Fatal("global capacity exceeded", w.Code)
	}
	plex()
	if st := manager.Status(); st.Viewers != 1 || st.Channels != 1 {
		t.Fatal(st)
	}
	jellyfin()
	if st := manager.Status(); st.Viewers != 0 {
		t.Fatal(st)
	}
}

func TestJellyfinRetainsNumericBrandsAndRegionalCommas(t *testing.T) {
	s := testServer(t)
	s.Config.Channels[0].Name = "5 USA"
	s.Config.Channels[1].Name = "News, London"
	s.Config.Channels[2].Name = "Sky News"
	page := tunerRequest(s.TunerHandler(), "GET", "/playlist.m3u")
	lines := strings.Split(strings.TrimSpace(page.Body.String()), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected three channels, got %q", lines)
	}
	// Jellyfin's GetChannelName strips an initial SAT>IP number and splits
	// display text at commas. Use its tvg-name fallback for comma names.
	for _, tc := range []struct {
		index  int
		suffix string
	}{
		{1, `tvg-chno="901",901. 5 USA`},
		{3, `tvg-chno="902" tvg-name="News, London",`},
		{5, `tvg-chno="903",Sky News`},
	} {
		if !strings.HasSuffix(lines[tc.index], tc.suffix) {
			t.Errorf("playlist title on line %d: got %q, want suffix %q", tc.index+1, lines[tc.index], tc.suffix)
		}
	}
}
