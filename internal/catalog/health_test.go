package catalog

import (
	"antennarr/internal/ingest"
	"testing"
)

func TestFailureRecoveryAndSelectedCandidates(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows, _ := ingest.Parse([]byte("https://tv.example/channel.ts"))
	s.Import(rows, "test")
	streams, _ := s.Streams("")
	sid := streams[0].ID
	c, _ := s.SaveChannel(Channel{Number: "1", Name: "Test", Enabled: true})
	s.Map(c, sid, true)
	for i := 1; i <= 4; i++ {
		if e = s.Record(sid, Result{Success: false, Reason: "media stalled"}); e != nil {
			t.Fatal(e)
		}
	}
	streams, _ = s.Streams("")
	if streams[0].State != "suspect" {
		t.Fatal(streams)
	}
	if candidates, _ := s.Candidates(c, true); len(candidates) != 0 {
		t.Fatal("quarantined source eligible")
	}
	s.Record(sid, Result{Success: true, StartupMS: 500, Reason: "progressing video and audio"})
	candidates, e := s.Candidates(c, true)
	if e != nil || len(candidates) != 1 {
		t.Fatal(candidates, e)
	}
	s.Quarantine(streams[0].AccountID, true)
	candidates, _ = s.Candidates(c, true)
	if len(candidates) != 0 {
		t.Fatal("blocked account eligible")
	}
}
