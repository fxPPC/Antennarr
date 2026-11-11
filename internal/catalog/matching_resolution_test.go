package catalog

import (
	"antennarr/internal/ingest"
	"testing"
)

// Providers can reuse a TVG ID for unrelated services. A learned ID and a
// different recognised name must not put the same feed into both channels.
func TestLearnedIDConflictingWithChannelNameStaysUnassigned(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, c := range []Channel{
		{ID: "nbc", Name: "NBC", Number: "004", Region: "US", Enabled: true},
		{ID: "cbs", Name: "CBS", Number: "002", Region: "US", Enabled: true},
	} {
		if _, err = s.SaveChannel(c); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ingest.FromURL("https://fixture.example/nbc.ts", "US NBC HD", "")
	if err != nil {
		t.Fatal(err)
	}
	first.TVGID = "shared.us"
	if _, err = s.Import([]ingest.Entry{first}, "fixture"); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err = s.DB.QueryRow("SELECT id FROM streams WHERE name=?", first.Name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = s.Map("nbc", id, true); err != nil {
		t.Fatal(err)
	}
	conflict, err := ingest.FromURL("https://fixture.example/cbs.ts", "US CBS HD", "")
	if err != nil {
		t.Fatal(err)
	}
	conflict.TVGID = first.TVGID
	if got, err := s.RelevantEntries([]ingest.Entry{conflict}); err != nil || len(got) != 0 {
		t.Errorf("discovery admitted conflicting identity: %d, %v", len(got), err)
	}
	if _, err = s.Import([]ingest.Entry{conflict}, "fixture"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM mappings m JOIN streams s ON s.id=m.stream_id WHERE s.name=?", conflict.Name).Scan(&n); err != nil || n != 0 {
		t.Errorf("conflicting identity produced %d mappings: %v", n, err)
	}
	if got, err := s.Candidates("nbc", false); err != nil || len(got) != 1 || got[0].StreamID != id {
		t.Errorf("explicit assignment changed: %+v, %v", got, err)
	}
	// A different label with no competing recognised service can still use the
	// learned ID. The conflict safeguard must not disable that intended feature.
	alternate, err := ingest.FromURL("https://fixture.example/alternate.ts", "Provider label", "")
	if err != nil {
		t.Fatal(err)
	}
	alternate.TVGID = first.TVGID
	if got, err := s.RelevantEntries([]ingest.Entry{alternate}); err != nil || len(got) != 1 {
		t.Fatalf("valid learned identity rejected by discovery: %d, %v", len(got), err)
	}
	if _, err = s.Import([]ingest.Entry{alternate}, "fixture"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Candidates("nbc", false); err != nil || len(got) != 2 {
		t.Fatalf("valid learned identity rejected by assignment: %d, %v", len(got), err)
	}
	// Teaching the same ID to CBS makes it unsuitable for automatic reuse.
	if err = s.DB.QueryRow("SELECT id FROM streams WHERE name=?", conflict.Name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = s.Map("cbs", id, true); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RelevantEntries([]ingest.Entry{alternate}); err != nil || len(got) != 0 {
		t.Errorf("discovery reused a non-unique learned ID: %d, %v", len(got), err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM mappings m JOIN streams s ON s.id=m.stream_id WHERE s.name=?", alternate.Name).Scan(&n); err != nil || n != 0 {
		t.Errorf("non-unique learned ID retained %d mappings: %v", n, err)
	}
	if got, err := s.Candidates("cbs", false); err != nil || len(got) != 1 || got[0].StreamID != id {
		t.Errorf("explicit CBS assignment lost: %+v, %v", got, err)
	}
}

func TestDiscoveryDoesNotQueueAmbiguousChannelNames(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, c := range []Channel{
		{ID: "network", Name: "NBC", Number: "004", Region: "US", Enabled: true},
		{ID: "local", Name: "Local NBC", Number: "204", Region: "US", Aliases: []string{"NBC"}, Enabled: true},
	} {
		if _, err = s.SaveChannel(c); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := ingest.FromURL("https://fixture.example/nbc.ts", "US NBC HD", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.RelevantEntries([]ingest.Entry{entry}); err != nil || len(got) != 0 {
		t.Fatalf("discovery queued a stream that cannot be uniquely assigned: %d, %v", len(got), err)
	}
}
