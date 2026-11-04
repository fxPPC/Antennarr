package catalog

import (
	"antennarr/internal/ingest"
	"fmt"
	"testing"
	"time"
)

func TestPersistentPacingProviderPortsAndCooldown(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	now := int64(2000000000000)
	ready, e := s.ReserveCheck("tv.example:80", "api", now, 2*time.Second, 10*time.Second)
	if e != nil || ready != now {
		t.Fatal(ready, e)
	}
	ready, e = s.ReserveCheck("tv.example:8080", "api", now+2000, 2*time.Second, 10*time.Second)
	if e != nil || ready != now+10000 {
		t.Fatal("ports bypassed provider rate", ready, e)
	}
	ready, e = s.ReserveCheck("other.example", "api", now+1000, 2*time.Second, 10*time.Second)
	if e != nil || ready != now+2000 {
		t.Fatal("global rate ignored", ready, e)
	}
	s.ProviderTrouble("tv.example", now, now+7200000, true)
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if !s.ProviderDeferred("tv.example:443", now+3600000) {
		t.Fatal("lost Retry-After on restart")
	}
	ready, e = s.ReserveCheck("tv.example", "api", now+3600000, 2*time.Second, 10*time.Second)
	if e != nil || ready != now+7200000 {
		t.Fatal(ready, e)
	}
	s.ProviderTrouble("tv.example", now+7200000, 0, true)
	var until int64
	s.DB.QueryRow("SELECT blocked_until FROM provider_rates WHERE provider='tv.example'").Scan(&until)
	if until != now+7200000+3600000 {
		t.Fatal("cooldown did not grow", until)
	}
}
func TestProbeSpacingAndCycleEligibility(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	entry, _ := ingest.FromURL("https://tv.example/live/u/p/1.ts", "CNN HD", "")
	s.Import([]ingest.Entry{entry}, "fixture")
	s.SaveChannel(Channel{Number: "1", Name: "CNN", Enabled: true})
	now := time.Now().Unix()
	ready, e := s.ReserveCheck("tv.example", "probe", now*1000, 10*time.Second, time.Minute)
	if e != nil || ready != now*1000 {
		t.Fatal(ready, e)
	}
	ready, e = s.ReserveCheck("tv.example", "probe", now*1000+10000, 10*time.Second, time.Minute)
	if e != nil || ready != now*1000+60000 {
		t.Fatal(ready, e)
	}
}

func TestRealTimeReservationSamplesClockAfterWriteLock(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		ready, e := s.ReserveCheck("clock.example", "api", 0, 2*time.Second, 10*time.Second)
		if ready != 0 && e == nil {
			e = fmt.Errorf("reservation not granted")
		}
		done <- e
	}()
	time.Sleep(25 * time.Millisecond)
	released := time.Now().UnixMilli()
	tx.Commit()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	var next int64
	s.DB.QueryRow("SELECT next_api FROM provider_rates WHERE provider='clock.example'").Scan(&next)
	if next < released+10000 {
		t.Fatal("pacing clock was sampled before the lock", next, released)
	}
}
