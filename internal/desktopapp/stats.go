//go:build darwin || windows

package desktopapp

import (
	"antennarr/internal/desktopstats"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func (s *session) readStats(reader *os.File) {
	defer reader.Close()
	d := json.NewDecoder(reader)
	for {
		var counts desktopstats.Counters
		if d.Decode(&counts) != nil {
			return
		}
		s.recordStats(counts)
	}
}

func (s *session) recordStats(counts desktopstats.Counters) error {
	s.mu.Lock()
	s.counters, s.countedAt = counts, time.Now()
	s.mu.Unlock()
	return nil
}

func (s *session) stats() (viewers int, channels int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters.Viewers, s.counters.WithFeeds, s.state == ready && time.Since(s.countedAt) < 15*time.Second
}

func (s *session) quickStats() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != ready || time.Since(s.countedAt) >= 15*time.Second {
		return "Viewers: —", "Channels with feeds: —"
	}
	return fmt.Sprintf("Viewers: %d", s.counters.Viewers),
		fmt.Sprintf("Channels with feeds: %d / %d", s.counters.WithFeeds, s.counters.Channels)
}
