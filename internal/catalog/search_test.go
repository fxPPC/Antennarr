package catalog

import (
	"antennarr/internal/ingest"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSearchExactIDsMetadataAndDeletedEntries(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ch, e := s.SaveChannel(Channel{Number: "101", Name: "CNN", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	// Two streams in account 1 make stream 2 distinct from account 2.
	for i, host := range []string{"first.example", "first.example", "second.example"} {
		v, _ := ingest.FromURL(fmt.Sprintf("https://%s/live/fixture/secret-password/%d.ts", host, i+1), "CNN HD", "")
		if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	s.EnableRosters(RosterPolicy{Target: 2, Preference: "stability", Race: 5})
	s.Record(2, Result{Success: true})
	for _, q := range []string{"CNN", "101", "FIRST.EXAMPLE", "stream:2", "STREAM #2", "account:1"} {
		v, e := s.SearchOutput(q)
		if e != nil || len(v.Channels) != 1 || v.Channels[0] != ch {
			t.Fatal(q, v, e)
		}
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "secret-password") {
			t.Fatal("search leaked credentials")
		}
	}
	v, e := s.SearchOutput("stream:2")
	if e != nil || len(v.Streams) != 1 || v.Streams[0].AccountID != 1 || !v.Streams[0].InRoster {
		t.Fatal(v, e)
	}
	v, e = s.SearchOutput("account:2")
	if e != nil || len(v.Streams) != 1 || v.Streams[0].ID != 3 {
		t.Fatal(v, e)
	}
	p, e := s.InputPage("stream:2", 0)
	if e != nil || p.Total != 1 || p.Items[0].ID != 1 {
		t.Fatal(p, e)
	}
	p, e = s.InputPage("account:2", 0)
	if e != nil || p.Total != 1 || p.Items[0].ID != 2 {
		t.Fatal(p, e)
	}
	p, e = s.InputPage("2", 0)
	if e != nil || p.Total != 2 {
		t.Fatal("plain ID should match either kind", p, e)
	}
	v, e = s.SearchOutput("stream:999")
	if e != nil || v.Exists || len(v.Streams) != 0 || v.ExactKind != "stream" {
		t.Fatal(v, e)
	}
	v, e = s.SearchOutput("account:2")
	if e != nil || !v.Exists {
		t.Fatal(v, e)
	}
	if _, e = s.DB.Exec("DELETE FROM mappings WHERE stream_id=3; DELETE FROM streams WHERE id=3"); e != nil {
		t.Fatal(e)
	}
	v, e = s.SearchOutput("stream:3")
	if e != nil || v.Exists {
		t.Fatal("deleted stream still found", v, e)
	}
	p, e = s.InputPage("stream:3", 0)
	if e != nil || p.Total != 0 {
		t.Fatal("deleted stream resolved to an account", p, e)
	}
	for _, q := range []string{"%", "_", "' OR 1=1 --"} {
		v, e = s.SearchOutput(q)
		if e != nil || len(v.Streams) != 0 || len(v.Channels) != 0 {
			t.Fatal(q, v, e)
		}
	}
	for _, q := range []string{strings.Repeat("x", 121), "stream:9999999999999999999999999999999"} {
		if _, e = s.SearchOutput(q); e == nil {
			t.Fatal("invalid query accepted")
		}
	}
}

func TestOutputSearchCapsStreamDetailsWithoutLosingMatchingChannels(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SaveChannel(Channel{Number: "1", Name: "CNN", Enabled: true})
	for i := 0; i < 105; i++ {
		v, _ := ingest.FromURL(fmt.Sprintf("https://fixture.example/%d.ts", i), "CNN", "")
		if _, e = s.Import([]ingest.Entry{v}, "fixture"); e != nil {
			t.Fatal(e)
		}
	}
	v, e := s.SearchOutput("CNN")
	if e != nil || len(v.Streams) != 100 || !v.Truncated || len(v.Channels) != 1 {
		t.Fatal(len(v.Streams), v.Truncated, e)
	}
}
