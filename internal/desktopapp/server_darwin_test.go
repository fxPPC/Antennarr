package desktopapp

import (
	"antennarr/internal/desktopstats"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestMacMenuStopsItsServerAndReceivesItsCounters(t *testing.T) {
	s := &session{state: ready}
	started := make(chan struct{})
	want := errors.New("server has returned")
	wait, err := s.startServer("", io.Discard, func(ctx context.Context, _ io.Writer, publish func(desktopstats.Counters) error) error {
		if err := publish(desktopstats.Counters{Viewers: 2, WithFeeds: 3, Channels: 5}); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return want
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- wait() }()
	defer s.stop()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the menu did not start its server")
	}
	if viewers, channels, ok := s.stats(); !ok || viewers != 2 || channels != 3 {
		t.Fatalf("unexpected menu counters %d, %d (ready %t)", viewers, channels, ok)
	}
	s.stop()
	select {
	case got := <-done:
		if !errors.Is(got, want) {
			t.Fatalf("lost server result: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the server did not stop when the menu quit")
	}
}
