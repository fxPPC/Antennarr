package ingest

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func portalResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestOperatorPortalEntitlementsAndFreshLinks(t *testing.T) {
	const mac = "02:00:00:00:00:01"
	a := PortalAccount{PortalURL: "https://tv.example/stalker_portal/c/", MAC: mac, Username: "subscriber-api", Password: "unit-test"}
	version := "one"
	inactive := false
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		user, password, ok := r.BasicAuth()
		if !ok || user != a.Username || password != a.Password || r.Method != "GET" || r.URL.Host != "tv.example" {
			t.Fatal("unexpected request")
		}
		switch r.URL.Path {
		case "/stalker_portal/api/stb/" + mac:
			if inactive {
				return portalResponse(200, `{"status":"OK","results":[{"mac":"`+mac+`","status":0}]}`), nil
			}
			return portalResponse(200, `{"status":"OK","results":[{"mac":"`+mac+`","status":"1"}]}`), nil
		case "/stalker_portal/api/itv_subscription/" + mac:
			return portalResponse(200, `{"status":"OK","results":[{"mac":"`+mac+`","sub_ch":["45","51"]}]}`), nil
		case "/stalker_portal/api/itv":
			return portalResponse(200, `{"status":"OK","results":[{"id":"45","number":"1","name":"Local One"},{"id":"45","number":"1","name":"Local One"},{"id":50,"number":2,"name":"Local Two"},{"id":51,"number":3,"name":"Ambiguous"},{"id":52,"number":3,"name":"Ambiguous"}]}`), nil
		case "/stalker_portal/server/tools/m3u.php":
			return portalResponse(200, "#EXTM3U x-tvg-url=\"https://guide.example/lineup.xml\"\n#EXTINF:0 tvg-id=\"local\",1. Local One\nhttps://streams.example/live?key="+version+"\n#EXTINF:0,2. Local Two\nhttps://streams.example/two\n#EXTINF:0,3. Ambiguous\nhttps://streams.example/three\n"), nil
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		return nil, nil
	})}
	rows, e := a.Channels(context.Background(), client)
	if e != nil || len(rows) != 1 || rows[0].Name != "Local One" || rows[0].TVGID != "local" || !strings.HasSuffix(rows[0].URL, "one") || a.XMLTV() != "https://guide.example/lineup.xml" {
		t.Fatalf("first import %+v %v", rows, e)
	}
	key := rows[0].Key
	version = "two"
	rows, e = a.Channels(context.Background(), client)
	if e != nil || len(rows) != 1 || rows[0].Key != key || !strings.HasSuffix(rows[0].URL, "two") {
		t.Fatalf("refresh %+v %v", rows, e)
	}
	inactive = true
	before := calls
	if _, e = a.Channels(context.Background(), client); e == nil || calls != before+1 {
		t.Fatal("inactive subscription reached the export")
	}
}

func TestPortalExportValidationsAndCredentialScope(t *testing.T) {
	base := PortalAccount{PortalURL: "https://tv.example/root", MAC: "02:01:02:03:04:05", PlaylistURL: "https://tv.example/subscriber/export?signature=private"}
	for _, f := range []func(*PortalAccount){
		func(a *PortalAccount) { a.MAC = "ff:ff:ff:ff:ff:ff" },
		func(a *PortalAccount) { a.PortalURL = "http://tv.example/root" },
		func(a *PortalAccount) { a.PlaylistURL = "https://127.0.0.1/sub" },
		func(a *PortalAccount) { a.GuideURL = "https://user:pw@tv.example/guide" },
		func(a *PortalAccount) { a.PlaylistURL = "https://tv.example/server/tools/m3u.php" },
		func(a *PortalAccount) {
			a.PlaylistURL = "https://elsewhere.example/playlist"
			a.Username = "subscriber"
			a.Password = "secret"
		},
	} {
		a := base
		f(&a)
		if a.Normalise() == nil {
			t.Fatal("invalid portal accepted")
		}
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "tv.example" {
			t.Fatal("followed cross-origin redirect")
		}
		resp := portalResponse(302, "")
		resp.Header.Set("Location", "https://cdn.example/export")
		return resp, nil
	})}
	_, err := base.Channels(context.Background(), client)
	if err == nil || strings.Contains(err.Error(), "signature") {
		t.Fatal("unredacted or accepted redirect")
	}
}

// Repetition is common when the operator combines multiple package lists.
func TestPortalDuplicateAndConflictingPlaylistEntries(t *testing.T) {
	a := PortalAccount{PortalURL: "https://provider.example", MAC: "02:00:00:00:00:01", PlaylistURL: "https://provider.example/subscriber/list"}
	row := func(station, link string) string {
		return "#EXTINF:0," + station + "\nhttps://feeds.example/" + link + "\n"
	}
	for _, tc := range []struct {
		name, rows string
		want       int
	}{
		{"identical", row("One", "one") + row("One", "one"), 1},
		{"different URLs", row("One", "one") + row("One", "alternate") + row("Two", "two"), 1},
		{"ambiguous then repeated", row("One", "one") + row("One", "alternate") + row("One", "one") + row("Two", "two"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return portalResponse(200, "#EXTM3U\n"+tc.rows), nil })}
			rows, err := a.Channels(context.Background(), c)
			if err != nil || len(rows) != tc.want {
				t.Fatalf("found %d playable channels, want %d; error %v", len(rows), tc.want, err)
			}
			if tc.name != "identical" && rows[0].Name != "Two" {
				t.Fatal("conflict was accepted")
			}
		})
	}
}

func TestPortalIncompleteSnapshot(t *testing.T) {
	cfg := PortalAccount{PortalURL: "https://provider.example", MAC: "02:00:00:00:00:01", PlaylistURL: "https://provider.example/registered/playlist"}
	for _, tail := range []string{"#EXTINF:0,Upcoming", "not an HTTP URL", "#EXTINF:0,Upcoming\n#EXTINF:0,Next\nhttps://media.example/n.ts", "#EXTINF:0,Upcoming\nhttps://"} {
		body := "#EXTM3U\n#EXTINF:0,One\nhttps://media.example/one.ts\n" + tail
		c := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return portalResponse(200, body), nil })}
		if rows, err := cfg.Channels(context.Background(), c); err == nil {
			t.Fatalf("accepted an incomplete replacement with %d channels", len(rows))
		}
	}
}
