package stream

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharingLimitsAndCleanup(t *testing.T) {
	var starts atomic.Int32
	runner := func(ctx context.Context, _ string, emit func([]byte)) error {
		starts.Add(1)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-tick.C:
				emit([]byte{0x47, 1, 2})
			}
		}
	}
	m := New(3, 5*time.Millisecond, runner)
	defer m.Close()
	a, ra, e := m.Subscribe("a", "720")
	if e != nil {
		t.Fatal(e)
	}
	defer ra()
	b, rb, e := m.Subscribe("a", "720")
	if e != nil {
		t.Fatal(e)
	}
	c, rc, e := m.Subscribe("b", "720")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = m.Subscribe("c", "720"); e != ErrBusy {
		t.Fatal("fourth viewer admitted")
	}
	for _, sub := range []<-chan []byte{a, b, c} {
		select {
		case <-sub:
		case <-time.After(time.Second):
			t.Fatal("no media")
		}
	}
	snapshot := m.Status()
	if len(snapshot.Outgoing) != 2 || snapshot.Outgoing[0].Channel != "a" || snapshot.Outgoing[0].Viewers != 2 || snapshot.Outgoing[1].Viewers != 1 {
		t.Fatal("outgoing counts do not reflect shared viewers", snapshot)
	}
	if starts.Load() != 2 {
		t.Fatal("same channel did not share pipeline")
	}
	rb()
	rc()
	ra()
	ra()
	deadline := time.Now().Add(time.Second)
	for m.Status().Channels != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := m.Status(); got.Viewers != 0 || got.Channels != 0 {
		t.Fatal(got)
	}
}
func TestSlowSubscriberIsolation(t *testing.T) {
	step := make(chan struct{})
	runner := func(ctx context.Context, _ string, emit func([]byte)) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-step:
				emit([]byte{0x47})
			}
		}
	}
	m := New(3, time.Millisecond, runner)
	defer m.Close()
	slow, release, _ := m.Subscribe("a", "720")
	defer release()
	fast, rf, _ := m.Subscribe("a", "720")
	defer rf()
	for i := 0; i < cap(slow)+4; i++ {
		step <- struct{}{}
		select {
		case <-fast:
		case <-time.After(time.Second):
			t.Fatal("fast client blocked")
		}
	}
	for range slow {
	}
	if m.Status().Viewers != 1 {
		t.Fatal("slow viewer not removed")
	}
}

func TestStartupReplayBurstDoesNotEvictReadyViewer(t *testing.T) {
	burstDone := make(chan struct{})
	m := New(3, time.Millisecond, func(ctx context.Context, _ string, emit func([]byte)) error {
		packet := make([]byte, 188*7)
		packet[0] = 0x47
		for i := 0; i < 2500; i++ {
			emit(packet)
		}
		close(burstDone)
		<-ctx.Done()
		return nil
	})
	defer m.Close()
	sub, release, e := m.Subscribe("burst", "test")
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	select {
	case <-burstDone:
	case <-time.After(3 * time.Second):
		t.Fatal("replay blocked")
	}
	if m.Status().Viewers != 1 {
		t.Fatal("viewer evicted during bounded startup replay")
	}
	for i := 0; i < 2500; i++ {
		select {
		case b, ok := <-sub:
			if !ok || len(b) != 188*7 {
				t.Fatal("replay truncated")
			}
		case <-time.After(time.Second):
			t.Fatal("replay missing")
		}
	}
}

func TestChannelChangeReleasesUnusedGraceFeeds(t *testing.T) {
	stopped := make(chan string, 10)
	m := New(6, time.Minute, func(ctx context.Context, id string, emit func([]byte)) error {
		emit([]byte{0x47})
		<-ctx.Done()
		stopped <- id
		return nil
	})
	defer m.Close()
	a, ra, _ := m.Subscribe("a", "a")
	<-a
	b, rb, _ := m.Subscribe("b", "b")
	defer rb()
	<-b
	ra()
	c, rc, e := m.Subscribe("c", "c")
	if e != nil {
		t.Fatal(e)
	}
	defer rc()
	<-c
	select {
	case id := <-stopped:
		if id != "a" {
			t.Fatal("active channel cancelled", id)
		}
	case <-time.After(time.Second):
		t.Fatal("unused feeds retained during channel change")
	}
	if s := m.Status(); s.Viewers != 2 || s.Channels != 2 {
		t.Fatal(s)
	}
}
func TestConfigurableOutgoingLimitSharesChannels(t *testing.T) {
	var starts atomic.Int32
	m := New(6, time.Millisecond, func(ctx context.Context, _ string, emit func([]byte)) error {
		starts.Add(1)
		emit([]byte{0x47})
		<-ctx.Done()
		return nil
	})
	defer m.Close()
	var releases []func()
	defer func() {
		for _, r := range releases {
			r()
		}
	}()
	for i := 0; i < 6; i++ {
		sub, r, e := m.Subscribe("same", "same")
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, r)
		if i == 0 {
			<-sub
		}
	}
	if starts.Load() != 1 {
		t.Fatal("upstream pipeline duplicated")
	}
	if _, _, e := m.Subscribe("other", "other"); e != ErrBusy {
		t.Fatal("outgoing limit bypassed")
	}
	m.SetLimit(4)
	if m.Status().Viewers != 6 {
		t.Fatal("lowering limit interrupted viewers")
	}
	m.SetLimit(8)
	sub, r, e := m.Subscribe("other", "other")
	if e != nil {
		t.Fatal(e)
	}
	defer r()
	<-sub
	if m.Status().Limit != 8 || starts.Load() != 2 {
		t.Fatal(m.Status(), starts.Load())
	}
}

func TestRapidReturnWaitsForPreviousChannelCleanup(t *testing.T) {
	cleanup := make(chan struct{})
	var starts atomic.Int32
	m := New(3, time.Minute, func(ctx context.Context, id string, emit func([]byte)) error {
		first := id == "a" && starts.Add(1) == 1
		emit([]byte{0x47})
		<-ctx.Done()
		if first {
			<-cleanup
		}
		return nil
	})
	defer m.Close()
	a, ra, _ := m.Subscribe("a", "a")
	<-a
	ra()
	b, rb, _ := m.Subscribe("b", "b")
	<-b
	rb()
	again, release, e := m.Subscribe("a", "a")
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	select {
	case <-again:
		close(cleanup)
		t.Fatal("channel restarted before previous feeds were closed")
	case <-time.After(30 * time.Millisecond):
	}
	close(cleanup)
	select {
	case <-again:
	case <-time.After(time.Second):
		t.Fatal("return to channel did not resume")
	}
}

func TestSlowViewerEvictionStopsUnusedPipeline(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var emit func([]byte)
	m := New(1, 10*time.Millisecond, func(ctx context.Context, _ string, push func([]byte)) error {
		emit = push
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil
	})
	defer m.Close()
	data, release, e := m.Subscribe("a", "a")
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	<-started
	for i := 0; i <= subscriberSlots; i++ {
		emit([]byte{1})
	}
	if len(data) != 0 {
		t.Fatal("evicted viewer still draining buffered media")
	}
	if m.Status().Viewers != 0 {
		t.Fatal("slot not released")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("empty pipeline kept upstreams alive")
	}
}
func TestOutputLimitChangesAcrossSharedAndDistinctChannels(t *testing.T) {
	m := New(3, time.Millisecond, func(ctx context.Context, _ string, _ func([]byte)) error { <-ctx.Done(); return nil })
	defer m.Close()
	releases := []func(){}
	for _, id := range []string{"a", "a", "b"} {
		_, r, e := m.Subscribe(id, id)
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, r)
	}
	if _, _, e := m.Subscribe("c", "c"); e != ErrBusy {
		t.Fatal("limit not enforced")
	}
	m.SetLimit(6)
	for _, id := range []string{"b", "c", "c"} {
		_, r, e := m.Subscribe(id, id)
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, r)
	}
	s := m.Status()
	if s.Viewers != 6 || s.Channels != 3 {
		t.Fatal(s)
	}
	m.SetLimit(2)
	if m.Status().Viewers != 6 {
		t.Fatal("limit change evicted viewers")
	}
	if _, _, e := m.Subscribe("d", "d"); e != ErrBusy {
		t.Fatal("lower limit not enforced")
	}
	for _, r := range releases {
		r()
		r()
	}
	if m.Status().Viewers != 0 {
		t.Fatal("double release affected accounting")
	}
}

func TestStopSharedChannelLeavesOthersAndAllowsRetune(t *testing.T) {
	m := New(3, time.Hour, func(ctx context.Context, _ string, emit func([]byte)) error {
		emit([]byte{1})
		<-ctx.Done()
		return ctx.Err()
	})
	defer m.Close()
	a, ra, _ := m.Subscribe("a", "a")
	b, rb, _ := m.Subscribe("a", "a")
	_, rc, _ := m.Subscribe("b", "b")
	defer rc()
	if !m.Stop("a") || m.Stop("a") {
		t.Fatal("stop not idempotent")
	}
	for _, sub := range []<-chan []byte{a, b} {
		select {
		case _, ok := <-sub:
			if ok {
				t.Fatal("buffer not discarded")
			}
		case <-time.After(time.Second):
			t.Fatal("viewer not closed")
		}
	}
	ra()
	rb()
	if m.Status().Viewers != 1 {
		t.Fatal("other channel affected or count corrupt")
	}
	_, release, err := m.Subscribe("a", "a")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if m.Status().Viewers != 1 {
		t.Fatal("retune count corrupt")
	}
}

func TestLineupOperationBlocksNewTunesAndReleasesGate(t *testing.T) {
	m := New(3, 0, func(ctx context.Context, _ string, _ func([]byte)) error { <-ctx.Done(); return ctx.Err() })
	defer m.Close()
	if err := m.WithIdle(func() error {
		if _, _, err := m.Subscribe("blocked", ""); err != ErrBusy {
			t.Fatal("tune raced replacement", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, release, err := m.Subscribe("allowed", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err = m.WithIdle(func() error { t.Fatal("entered during playback"); return nil }); err != ErrBusy {
		t.Fatal(err)
	}
}
