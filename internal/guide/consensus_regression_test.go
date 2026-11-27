package guide

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"antennarr/internal/catalog"
)

func TestDividedOriginAbstainsFromEntireBallot(t *testing.T) {
	for _, alternatives := range [][]string{{"News", "Film"}, {"Film", "Sport"}} {
		t.Run(strings.Join(alternatives, "-"), func(t *testing.T) {
			candidates := []Candidate{evidence("first.example", "News"), evidence("second.example", "News"), evidence("third.example", "Film"), evidence("divided.example", alternatives[0]), evidence("divided.example", alternatives[1])}
			for range 2 {
				result, conflict := resolve(candidates, nil, "", 1200)
				if conflict || len(result) != 1 || result[0].Title != "News" || result[0].State != "confirmed" || result[0].Origins != 2 {
					t.Fatalf("expected two votes for News, one for Film, and one abstention; got %+v, conflict=%v", result, conflict)
				}
				slices.Reverse(candidates)
			}
		})
	}
}

func TestIncompleteListingsRespectEpisodeEvidence(t *testing.T) {
	const firstID, replacementID = "0.2.", "1.5."
	detailed := func(origin, ep string) Candidate {
		c := evidence(origin, "Detectives")
		p := &c.programmes[0]
		p.Episode = ep
		p.EpisodeSystem = "xmltv_ns"
		if ep == replacementID {
			p.Subtitle = "The Stowaway"
		} else {
			p.Subtitle = "The Reminder"
		}
		return c
	}
	sparse := evidence("listings.example", "Detectives")
	previous := detailed("old", firstID).programmes
	for _, tc := range []struct {
		name  string
		votes []Candidate
		old   []Programme
		want  string
	}{
		{"two corroborating episode records", []Candidate{sparse, detailed("b", replacementID), detailed("c", replacementID)}, nil, replacementID},
		{"two sparse refreshes retain the published episode", []Candidate{sparse, evidence("b", "Detectives")}, previous, firstID},
		{"one specific challenger cannot revise the published episode", []Candidate{sparse, detailed("b", replacementID)}, previous, firstID},
		{"two corroborating corrections revise the episode", []Candidate{sparse, detailed("b", replacementID), detailed("c", replacementID)}, previous, replacementID},
		{"conflicting episode IDs with a sparse tie breaker have no specific winner", []Candidate{sparse, detailed("b", firstID), detailed("c", replacementID)}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 2 {
				out, _ := resolve(tc.votes, tc.old, "", 1200)
				if len(out) != 1 || out[0].Episode != tc.want {
					t.Fatalf("wanted episode %q, got %+v", tc.want, out)
				}
				if tc.want != "" && out[0].Subtitle == "" {
					t.Fatalf("corroborated/published subtitle lost: %+v", out[0])
				}
				slices.Reverse(tc.votes)
			}
		})
	}
}

func TestEpisodeSystemsAreDistinctNamespaces(t *testing.T) {
	a := evidence("a", "Series")
	a.programmes[0].Episode = "1.5."
	a.programmes[0].EpisodeSystem = "xmltv_ns"
	b := evidence("b", "Series")
	b.programmes[0].Episode = "S02E06"
	b.programmes[0].EpisodeSystem = "onscreen"
	cs := []Candidate{a, b}
	for range 2 {
		got, conflict := resolve(cs, nil, "", 1200)
		if conflict || len(got) != 1 || got[0].Episode != "1.5." || got[0].EpisodeSystem != "xmltv_ns" {
			t.Fatalf("different XMLTV syntaxes counted as conflicting episode IDs: %v %v", got, conflict)
		}
		slices.Reverse(cs)
	}
}
func TestPinCorrectionDropsListingsFromPreviousStation(t *testing.T) {
	db, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id, err := db.SaveChannel(catalog.Channel{ID: "guide-pin-fixture", Name: "News", Number: "500", Enabled: true, ProfileID: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	tm := func(d time.Duration) string { return now.Add(d).Format("20060102150405 -0700") }
	raw := fmt.Sprintf(`<tv><channel id="news"><display-name>News</display-name></channel><channel id="sports"><display-name>Sports</display-name></channel><programme channel="news" start="%[1]s" stop="%[2]s"><title>Original bulletin</title></programme><programme channel="news" start="%[2]s" stop="%[3]s"><title>Wrong station's late bulletin</title></programme><programme channel="sports" start="%[1]s" stop="%[2]s"><title>Sport</title></programme></tv>`, tm(-time.Hour), tm(time.Hour), tm(3*time.Hour))
	code := 200
	svc, _ := New(db, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{Request: r, StatusCode: code, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})})
	ctx := context.Background()
	if err = svc.SetFeed(ctx, "primary", "https://guide.example/feed"); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err = svc.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}
	get := func() []Programme {
		t.Helper()
		v, e := svc.Programmes(ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		return v[id]
	}
	refresh()
	if p := get(); len(p) != 2 {
		t.Fatalf("fixture not seeded: %+v", p)
	}
	pin := stationID("guide.example", "sports")
	if err = svc.SetPin(ctx, "primary", id, pin); err != nil {
		t.Fatal(err)
	}
	refresh()
	if p := get(); len(p) != 1 || p[0].Title != "Sport" {
		t.Fatalf("selected sports guide includes foreign listings: %+v", p)
	}
	code = 503
	if err = svc.SetPin(ctx, "primary", id, pin); err != nil {
		t.Fatal(err)
	}
	refresh()
	if p := get(); len(p) != 1 || p[0].Title != "Sport" {
		t.Fatalf("same-pin outage erased listings: %+v", p)
	}
}

func TestStationReassignmentDuringGuideFetch(t *testing.T) {
	db, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	channel := catalog.Channel{ID: "local-news", Name: "News", Region: "US", Number: "500", Enabled: true, ProfileID: "primary"}
	if _, err = db.SaveChannel(channel); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	raw := fmt.Sprintf(`<tv><channel id="news"><display-name>News</display-name></channel><programme channel="news" start="%s" stop="%s"><title>Previous region's bulletin</title></programme></tv>`, now.Add(-time.Hour).Format("20060102150405 -0700"), now.Add(time.Hour).Format("20060102150405 -0700"))
	started := make(chan struct{})
	finish := make(chan struct{})
	waiting := false
	svc, _ := New(db, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if waiting {
			close(started)
			<-finish
		}
		return &http.Response{Request: r, StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})})
	if err = svc.SetFeed(ctx, "primary", "https://guide.example/feed"); err != nil {
		t.Fatal(err)
	}
	if err = svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	get := func() []Programme {
		t.Helper()
		out, e := svc.Programmes(ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		return out[channel.ID]
	}
	channel.Number = "707"
	if _, err = db.SaveChannel(channel); err != nil {
		t.Fatal(err)
	}
	if len(get()) != 1 {
		t.Fatal("renumbered channel lost its published guide")
	}
	waiting = true
	done := make(chan error, 1)
	go func() { done <- svc.Refresh(ctx) }()
	<-started
	channel.Region = "GB"
	channel.Station = "LONDON"
	_, err = db.SaveChannel(channel)
	before := get()
	close(finish)
	refreshErr := <-done
	if err != nil || refreshErr != nil {
		t.Fatal(err, refreshErr)
	}
	after := get()
	if len(before) != 0 || len(after) != 0 {
		t.Fatalf("wrong station's schedule immediately after reassignment %v; after in-flight fetch returned %v", before, after)
	}
}

func TestXMLTVBritishClockChangeWithNamedOffsets(t *testing.T) {
	// XMLTV's DTD explicitly uses BST as +0100. During the autumn clock
	// change, the same local clock reading identifies two different broadcasts.
	const input = `<tv><channel id="london"/><programme channel="london" start="202610250130 BST" stop="202610250130 GMT"><title>First 01:30</title></programme><programme channel="london" start="202610250130 GMT" stop="202610250230 UTC"><title>Second 01:30</title></programme></tv>`
	start := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC).Unix()
	stations, err := parseXMLTV(strings.NewReader(input), start)
	if err != nil {
		t.Fatal(err)
	}
	p := stations["london"].programmes
	if len(p) != 2 || p[0].Start != start || p[0].Stop != p[1].Start || p[1].Start != start+3600 || p[1].Stop != start+7200 {
		t.Fatalf("expected successive UTC hours, got %+v", p)
	}
	if _, ok := xmlTime("202610250130 CST"); ok {
		t.Fatal("ambiguous zone was coerced to UTC")
	}
}
