package roster

import (
	"context"
	"sort"

	"antennarr/internal/catalog"
	"antennarr/internal/config"
)

// Try the least-covered channels first. Cycle through equally thin channels so
// a failing source cannot consume every recruitment turn for its group.
func (s *Service) recruit(ctx context.Context, channels []catalog.ChannelRoster, v config.Settings) bool {
	sort.Slice(channels, func(i, j int) bool {
		a, b := channels[i], channels[j]
		if a.Count != b.Count {
			return a.Count < b.Count
		}
		afterA, afterB := a.ChannelID > s.lastRecruitChannel, b.ChannelID > s.lastRecruitChannel
		if afterA != afterB {
			return afterA
		}
		return a.ChannelID < b.ChannelID
	})
	for _, ch := range channels {
		if ch.Count >= ch.Policy.Target {
			continue
		}
		pool, err := s.Store.QueueCandidates(ch.ChannelID, 1)
		if err != nil {
			s.report("Select recruitment candidate", err)
			return false
		}
		if len(pool) > 0 {
			s.lastRecruitChannel = ch.ChannelID
			s.probe(ctx, pool[0], v)
			return true
		}
	}
	return false
}
