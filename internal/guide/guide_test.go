package guide

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"antennarr/internal/budget"
	"antennarr/internal/catalog"
)

func listing(title string) Programme { return Programme{Start: 1000, Stop: 2000, Title: title} }
func evidence(origin, title string) Candidate {
	return Candidate{ID: stationID(origin, title), Source: origin, programmes: []Programme{listing(title)}}
}
func TestIndependentOriginsAndTurnover(t *testing.T) {
	now := int64(1200)
	a, b, c := evidence("guide-a", "Wrong"), evidence("guide-b", "Correct"), evidence("guide-c", "Correct")
	votes := []Candidate{b, c}
	for range 30 {
		votes = append(votes, a)
	}
	result, _ := resolve(votes, nil, "", now)
	if len(result) != 1 || result[0].Title != "Correct" || result[0].Origins != 2 || result[0].State != "confirmed" {
		t.Fatal(result)
	}
	tied, conflict := resolve([]Candidate{a, b}, result, "", now)
	if !conflict || len(tied) != 1 || tied[0].Title != "Correct" {
		t.Fatal(tied)
	}
	cached, _ := resolve(nil, tied, "", now)
	if len(cached) != 1 || cached[0].State != "cached" {
		t.Fatal(cached)
	}
	empty, _ := resolve(nil, cached, "", 2100)
	if len(empty) != 0 {
		t.Fatal(empty)
	}
	single, _ := resolve([]Candidate{a}, nil, "", now)
	if single[0].State != "single" {
		t.Fatal(single)
	}
	pinned, _ := resolve(votes, result, a.ID, now)
	if pinned[0].Title != "Wrong" || pinned[0].State != "pinned" {
		t.Fatal(pinned)
	}
	a2 := evidence("guide-a", "Other")
	blocked, conflict := resolve([]Candidate{a, a2}, nil, "", now)
	if len(blocked) != 0 || !conflict {
		t.Fatal(blocked)
	}
}
func TestProgrammeCorrectionsDoNotCreateOverlap(t *testing.T) {
	old := []Programme{{Start: 1000, Stop: 2000, Title: "Scheduled match"}}
	alt := Candidate{ID: "A", Source: "a", programmes: []Programme{{Start: 1000, Stop: 1400, Title: "Weather delay"}, {Start: 1400, Stop: 2100, Title: "Match"}}}
	copy := alt
	copy.Source = "b"
	copy.ID = "B"
	p, _ := resolve([]Candidate{alt, copy}, old, "", 1100)
	if len(p) != 2 || p[0].Title != "Weather delay" || p[1].Start != p[0].Stop {
		t.Fatal(p)
	}
}
func TestXMLTVGzipDSTAndIncompleteListing(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	fmt.Fprint(gz, `<tv><channel id="A"><display-name>Alpha</display-name></channel><programme channel="A" start="20261101013000 -0400"><title>Early</title><episode-num system="xmltv_ns">1.2.</episode-num></programme><programme channel="A" start="20261101013000 -0500" stop="20261101020000 -0500"><title>Late</title></programme><programme channel="A" start="20261101020000 -0500"><title>Unknown end</title></programme></tv>`)
	gz.Close()
	now, _ := xmlTime("20261101010000 -0400")
	stations, e := parseXMLTV(&buf, now)
	if e != nil {
		t.Fatal(e)
	}
	p := stations["A"].programmes
	if len(p) != 2 || p[0].Stop-p[0].Start != 3600 || p[0].EpisodeSystem != "xmltv_ns" || p[1].Start != p[0].Stop {
		t.Fatal(p)
	}
	for _, bad := range []string{"", "<other/>", "<tv/><tv/>", "<tv><programme"} {
		if _, e := parseXMLTV(strings.NewReader(bad), now); e == nil {
			t.Fatal("accepted", bad)
		}
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestExternalGuideCachePinAndScopedOutput(t *testing.T) {
	dir := t.TempDir()
	db, e := catalog.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	id, e := db.SaveChannel(catalog.Channel{ID: "fixture", Name: "News", Number: "500", Enabled: true, ProfileID: "primary"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	tm := func(d time.Duration) string { return now.Add(d).UTC().Format("20060102150405 -0700") }
	xml := fmt.Sprintf(`<tv><channel id="n"><display-name>News</display-name></channel><channel id="other"><display-name>An unrelated local edition</display-name></channel><programme channel="n" start="%s" stop="%s"><title>Daily bulletin</title></programme><programme channel="other" start="%s" stop="%s"><title>Local sport</title></programme></tv>`, tm(-time.Minute), tm(time.Hour), tm(-time.Minute), tm(time.Hour))
	code := 200
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{Request: r, StatusCode: code, Body: io.NopCloser(strings.NewReader(xml))}, nil
	})}
	s, _ := New(db, client)
	ctx := context.Background()
	if e = s.SetFeed(ctx, "primary", "https://provider.example/feed?private=secret"); e != nil {
		t.Fatal(e)
	}
	refresh := func() {
		t.Helper()
		if e := s.Refresh(ctx); e != nil {
			t.Fatal(e)
		}
	}
	schedule := func() Programme {
		t.Helper()
		out, e := s.Programmes(ctx, "primary")
		if e != nil || len(out[id]) != 1 {
			t.Fatalf("result=%v error=%v", out[id], e)
		}
		return out[id][0]
	}
	refresh()
	if p := schedule(); p.Title != "Daily bulletin" || p.State != "single" {
		t.Fatal(p)
	}
	pin := stationID("provider.example", "other")
	if e = s.SetPin(ctx, "primary", id, pin); e != nil {
		t.Fatal(e)
	}
	refresh()
	if p := schedule(); p.Title != "Local sport" || p.State != "pinned" {
		t.Fatal(p)
	}
	code = 503
	refresh()
	other, _ := New(db, client)
	s = other
	if p := schedule(); p.Title != "Local sport" || p.State != "cached" {
		t.Fatal(p)
	}
	if e = s.SetPin(ctx, "wrong-tuner", id, pin); e == nil {
		t.Fatal("cross tuner pin")
	}
	if e = s.SetFeed(ctx, "primary", "http://127.0.0.1/guide"); e == nil {
		t.Fatal("local guide")
	}
	for _, path := range []string{"/api/guide?profile_id=primary", "/api/guide/channel?profile_id=primary&id=" + id, "/api/guide/stations?profile_id=primary&q=local"} {
		w := httptest.NewRecorder()
		s.Admin(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestLiveTunePreemptsGuideReservation(t *testing.T) {
	db, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	started := make(chan struct{})
	var once sync.Once
	c := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(started) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	s, _ := New(db, c)
	s.Budget = budget.New(1, 1)
	if e = s.SetFeed(context.Background(), "primary", "https://provider.example/guide"); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- s.Refresh(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetch did not start")
	}
	s.Preempt()
	if e = <-done; e == nil {
		t.Fatal("refresh was not cancelled")
	}
	release, ok := s.Budget.Reserve(5, "provider.example", 1, true)
	if !ok {
		t.Fatal("live stream could not take slot")
	}
	defer release()
}

func TestGuideBudgetDuringContinuousPlayback(t *testing.T) {
	b := budget.New(4, 3)
	stop, ok := b.Reserve(11, "portal.example", 1, true)
	if !ok {
		t.Fatal("live reservation")
	}
	defer stop()
	if _, ok = b.ReserveGuide(11, "portal.example", 1); ok {
		t.Fatal("single-connection account reused")
	}
	if _, ok = b.ReserveGuide(-1, "portal.example", 1); ok {
		t.Fatal("unattributed guide reused busy portal")
	}
	free, ok := b.ReserveGuide(12, "other.example", 1)
	if !ok {
		t.Fatal("independent listings starved by continuous playback")
	}
	free()
}

func TestRosterGuideCollectionAndIndependentOrigins(t *testing.T) {
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	ctx := context.Background()
	id, e := store.SaveChannel(catalog.Channel{ID: "rostered", ProfileID: "primary", Name: "Test Service", Region: "US", Number: "501", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	hosts := []string{"a.example", "a.example", "a.example", "b.example", "c.example"}
	for i, host := range hosts {
		key := fmt.Sprint(i + 10)
		secret := fmt.Sprintf(`{"Kind":"xtream","Endpoint":"https://%s","Username":"user%s","Password":"test"}`, host, key)
		_, e = store.DB.Exec(`INSERT INTO accounts(id,identity,provider,kind,secret,state,created) VALUES(?,?,?,'xtream',?,'healthy',0)`, i+10, key, host, secret)
		if e != nil {
			t.Fatal(e)
		}
		_, e = store.DB.Exec(`INSERT INTO streams(id,account_id,identity,name,tvg_id,secret) VALUES(?,?,?,'Test Service','1','{}')`, i+10, i+10, key)
		if e != nil {
			t.Fatal(e)
		}
		_, e = store.DB.Exec(`INSERT INTO roster VALUES(?,?,?,0,1,1)`, id, i+10, i+10)
		if e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now().UTC()
	start, stop := now.Add(-time.Hour).Format("20060102150405 -0700"), now.Add(time.Hour).Format("20060102150405 -0700")
	counts := map[string]int{}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		h := r.URL.Host
		counts[h]++
		title, name := "Live now", "Test Service"
		if h == "a.example" {
			title = "Wrong"
		}
		if h == "external.example" {
			name = "Unrelated station"
			title = "Other"
		}
		xml := fmt.Sprintf(`<tv><channel id="1"><display-name>%s</display-name></channel><programme channel="1" start="%s" stop="%s"><title>%s</title></programme></tv>`, name, start, stop, title)
		return &http.Response{Request: r, StatusCode: 200, Body: io.NopCloser(strings.NewReader(xml))}, nil
	})}
	s, _ := New(store, client)
	if e = s.SetFeed(ctx, "primary", "https://external.example/guide"); e != nil {
		t.Fatal(e)
	}
	if e = s.Refresh(ctx); e != nil {
		t.Fatal(e)
	}
	out, e := s.Programmes(ctx, "primary")
	if e != nil || len(out[id]) != 1 || out[id][0].Title != "Live now" || out[id][0].Origins != 2 {
		t.Fatalf("out %v err %v", out[id], e)
	}
	if len(counts) != 4 || counts["a.example"] != 1 {
		t.Fatal(counts)
	}
	w := httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/api/guide/channel?profile_id=primary&id="+id, nil))
	if strings.Contains(w.Body.String(), "Unrelated station") || !strings.Contains(w.Body.String(), `"state":"confirmed"`) {
		t.Fatal(w.Body.String())
	}
}

func TestDeclaredGuideOriginAndLegacyCharset(t *testing.T) {
	raw := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><tv source-info-url=\"https://publisher.example/\" source-data-url=\"https://listings.example/f.xml?private=1\"><channel id=\"test\"><display-name>Caf\xe9</display-name></channel></tv>")
	origin := "mirror.example"
	rows, e := parseSource(bytes.NewReader(raw), 0, &origin)
	if e != nil || origin != "listings.example" || rows["test"].displayName() != "Café" {
		t.Fatal(origin, rows, e)
	}
}
func TestUnknownGuideReservesItsWholeOrigin(t *testing.T) {
	b := budget.New(4, 3)
	release, ok := b.ReserveGuide(-1, "portal.example", 1)
	if !ok {
		t.Fatal("guide did not reserve")
	}
	if _, ok = b.Reserve(9, "portal.example", 1, false); ok {
		t.Fatal("unattributed guide and account check overlapped")
	}
	release()
	closeLive, ok := b.Reserve(9, "portal.example", 1, true)
	if !ok {
		t.Fatal("guide reservation leaked")
	}
	closeLive()
}
