package roster

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/ingest"
	"context"
	"testing"
)

func TestLiveReplenishmentOnlyAuditionsPlayingVacancies(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			s, ch, cs, p, tr := fixture(t)
			s.Store.Record(cs[0].StreamID, catalog.Result{Success: true})
			idle, err := s.Store.SaveChannel(catalog.Channel{Number: "2", Name: "CNN", Region: "US", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			e, _ := ingest.FromURL("https://idle.example/2.ts", "CNN", "")
			s.Store.Import([]ingest.Entry{e}, "fixture")
			// The same source may map to two outputs; active recruitment must not
			// silently fill the idle output as a side effect of a successful probe.
			s.Store.Map(idle, cs[1].StreamID, true)
			s.Store.Map(idle, cs[2].StreamID, true)
			s.Store.Map(idle, cs[3].StreamID, true)
			s.Live = func() bool { return true }
			s.ActiveChannels = func() map[string]bool { return map[string]bool{ch: true} }
			s.Settings = func() config.Settings {
				v := config.Settings{}
				v.Defaults()
				v.ReplenishWhilePlaying = enabled
				return v
			}
			release, ok := s.Budget.Reserve(cs[0].AccountID, cs[0].Provider, 1, true)
			if !ok {
				t.Fatal("live reservation failed")
			}
			defer release()
			s.step(context.Background())
			rows, _ := s.Store.RosterMembers(ch)
			other, _ := s.Store.RosterMembers(idle)
			expected := int32(0)
			count := 1
			if enabled {
				expected = 1
				count = 2
			}
			if p.calls.Load() != expected || len(rows) != count || len(other) != 0 || tr.calls.Load() != 0 {
				t.Fatal("live scope violated", p.calls.Load(), len(rows), len(other), tr.calls.Load())
			}
			s.Store.DB.Exec("UPDATE provider_rates SET next_probe=0,next_api=0")
			s.step(context.Background())
			if p.calls.Load() != expected {
				t.Fatal("full playing roster received a routine check")
			}
		})
	}
}

func TestTemporarilyFullRostersDoNotBlockQueuedPlaybackReserves(t *testing.T) {
	s, ch, cs, _, _ := fixture(t)
	for _, c := range cs[:2] {
		s.Store.Record(c.StreamID, catalog.Result{Success: true})
		s.Store.AccountMetadata(c.AccountID, 1, 0, 1)
	}
	members, _ := s.Store.RosterMembers(ch)
	if len(members) != 2 {
		t.Fatal(members)
	}
	playable, err := s.Candidates(ch, false)
	if err != nil || len(playable) != 2 {
		t.Fatal("queue fallback blocked by full roster", playable, err)
	}
	for _, v := range playable {
		if v.StreamID == cs[0].StreamID || v.StreamID == cs[1].StreamID {
			t.Fatal("selected full account")
		}
	}
	members, _ = s.Store.RosterMembers(ch)
	if len(members) != 2 {
		t.Fatal("selection removed verified memberships")
	}
}

func TestReadyLivePairStopsEmergencyMetadataWork(t *testing.T) {
	s, ch, cs, p, tr := fixture(t)
	for _, c := range cs[:2] {
		s.Store.Record(c.StreamID, catalog.Result{Success: true})
		s.Store.AccountMetadata(c.AccountID, 1, 0, 1)
	}
	s.RequestDiscovery(ch)
	s.Live = func() bool { return true }
	s.ActiveChannels = func() map[string]bool { return map[string]bool{ch: true} }
	s.LivePairReady = func(string) bool { return true }
	s.step(context.Background())
	if tr.calls.Load() != 0 || p.calls.Load() != 0 {
		t.Fatal("routine work continued after backup recovery", tr.calls.Load(), p.calls.Load())
	}
}
