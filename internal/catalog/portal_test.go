package catalog

import (
	"antennarr/internal/ingest"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type stationFixtureTransport struct{ station string }

func (f *stationFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	const mac = "02:00:00:00:00:01"
	var body string
	switch r.URL.Path {
	case "/api/stb/" + mac:
		body = `{"status":"OK","results":[{"mac":"` + mac + `","status":1}]}`
	case "/api/itv_subscription/" + mac:
		body = `{"status":"OK","results":[{"mac":"` + mac + `","sub_ch":["17"]}]}`
	case "/api/itv":
		body = fmt.Sprintf(`{"status":"OK","results":[{"id":"17","number":7,"name":%q}]}`, f.station)
	case "/server/tools/m3u.php":
		body = "#EXTM3U\n#EXTINF:0,7. " + f.station + "\nhttps://cdn.example/" + f.station + ".ts"
	default:
		return nil, fmt.Errorf("unexpected request")
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}
func TestPortalProviderReusesStationID(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ch, err := s.SaveChannel(Channel{Number: "7", Name: "One", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := ingest.PortalAccount{PortalURL: "https://provider.example", MAC: "02:00:00:00:00:01", Username: "scope", Password: "private"}
	upstream := &stationFixtureTransport{"One"}
	client := &http.Client{Transport: upstream}
	rows, err := cfg.Channels(ctx, client)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	account, err := s.SavePortalCatalogue(cfg, rows)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := s.Candidates(ch, false)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	id := candidates[0].StreamID
	// Seed the reference encoding saved by alpha 1 and beta 1, independently
	// of the identifier format now used by SavePortalCatalogue.
	ref := "portal:" + strconv.FormatInt(account, 10) + "/" + fingerprint("station\x0017")
	record, _ := json.Marshal(ingest.Entry{Kind: "portal", Name: "One", URL: ref, Endpoint: cfg.PortalURL})
	if _, err = s.DB.Exec("UPDATE streams SET identity=?,secret=? WHERE id=?", fingerprint(ref), record, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePortalCatalogue(cfg, rows); err != nil {
		t.Fatal(err)
	}
	candidates, err = s.Candidates(ch, false)
	if err != nil || len(candidates) != 1 || candidates[0].StreamID != id {
		t.Fatal("existing subscription lost its stream", candidates, err)
	}
	if got, err := s.ResolvePortalStream(ctx, client, ref); err != nil || got != "https://cdn.example/One.ts" {
		t.Fatal("existing subscription cannot play", got, err)
	}
	if err = s.EnableRosters(RosterPolicy{3, "stability", 5}); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(id, Result{Success: true}); err != nil {
		t.Fatal(err)
	}
	current, err := s.RosterMembers(ch)
	if err != nil || len(current) != 1 {
		t.Fatal("original station was not rostered", current, err)
	}
	upstream.station = "Two"
	if got, err := s.ResolvePortalStream(ctx, client, ref); err == nil {
		t.Fatalf("old channel resolved reassigned station %q before refresh", got)
	}
	rows, err = cfg.Channels(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePortalCatalogue(cfg, rows); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ResolvePortalStream(ctx, client, ref); err == nil {
		t.Fatalf("old channel resolved reassigned station %q after refresh", got)
	}
	var enabled int
	if err = s.DB.QueryRow("SELECT enabled FROM streams WHERE id=?", id).Scan(&enabled); err != nil || enabled != 0 {
		t.Fatal("reassigned station retained the old playable identity", enabled, err)
	}
	var newID int64
	var raw []byte
	if err = s.DB.QueryRow("SELECT id,secret FROM streams WHERE account_id=? AND enabled=1", account).Scan(&newID, &raw); err != nil || newID == id {
		t.Fatal("expected a new playable station", newID, err)
	}
	var source ingest.Entry
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ResolvePortalStream(ctx, client, source.URL); err != nil || got != "https://cdn.example/Two.ts" {
		t.Fatal(got, err)
	}
	if err = s.DB.QueryRow("SELECT stream_id FROM roster WHERE stream_id=?", newID).Scan(&newID); err != sql.ErrNoRows {
		t.Fatal("newly reassigned station must earn its own roster verification", err)
	}
}

func TestPortalReinstatementAfterLateValidation(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprintf("success=%t", success), func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			channel, err := s.SaveChannel(Channel{Number: "7", Name: "One", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			a := ingest.PortalAccount{PortalURL: "https://provider.example", PlaylistURL: "https://provider.example/subscriber/list", MAC: "02:00:00:00:00:01"}
			rows := []ingest.PortalChannel{{Key: fingerprint("fixture"), Name: "One", URL: "https://media.example/one"}}
			account, err := s.SavePortalCatalogue(a, rows)
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := s.Candidates(channel, false)
			if err != nil || len(candidates) != 1 {
				t.Fatal(candidates, err)
			}
			stream := candidates[0].StreamID
			if err = s.EnableRosters(RosterPolicy{3, "stability", 5}); err != nil {
				t.Fatal(err)
			}
			if err = s.Record(stream, Result{Success: true}); err != nil {
				t.Fatal(err)
			}
			// A result can arrive after the subscription changes, for example
			// when the last viewer finishes a previously admitted stream.
			if _, err = s.SavePortalCatalogue(a, nil); err != nil {
				t.Fatal(err)
			}
			if err = s.Record(stream, Result{Success: success}); err != nil {
				t.Fatal(err)
			}
			var state string
			if err = s.DB.QueryRow("SELECT state FROM streams WHERE id=?", stream).Scan(&state); err != nil || state != "portal_unsubscribed" {
				t.Fatal("stale observation overwrote withdrawal", state, err)
			}
			roster, err := s.RosterMembers(channel)
			if err != nil || len(roster) != 0 {
				t.Fatal("withdrawn stream returned to roster", roster, err)
			}
			if _, err = s.SavePortalCatalogue(a, rows); err != nil {
				t.Fatal(err)
			}
			candidates, err = s.Candidates(channel, false)
			if err != nil || len(candidates) != 1 || candidates[0].AccountID != account || candidates[0].StreamID != stream {
				t.Fatalf("restored subscription lost its original stream: %v %v", candidates, err)
			}
			healthy, err := s.Candidates(channel, true)
			if err != nil || len(healthy) != 0 {
				t.Fatalf("a restored stream must be validated again: %v %v", healthy, err)
			}
			if err = s.Record(stream, Result{Success: true}); err != nil {
				t.Fatal(err)
			}
			roster, err = s.RosterMembers(channel)
			if err != nil || len(roster) != 1 || roster[0].StreamID != stream {
				t.Fatal("fresh validation did not readmit renewed station", roster, err)
			}
			// Alpha 1 snapshots may already have a late result's state on a disabled row.
			legacyState := HealthState(1)
			if success {
				legacyState = HealthState(0)
			}
			if _, err = s.DB.Exec("UPDATE streams SET enabled=0,state=? WHERE id=?", legacyState, stream); err != nil {
				t.Fatal(err)
			}
			if _, err = s.SavePortalCatalogue(a, rows); err != nil {
				t.Fatal(err)
			}
			if err = s.DB.QueryRow("SELECT state FROM streams WHERE id=? AND enabled=1", stream).Scan(&state); err != nil || state != "unverified" {
				t.Fatal("legacy withdrawal was not repaired", state, err)
			}
		})
	}
}
