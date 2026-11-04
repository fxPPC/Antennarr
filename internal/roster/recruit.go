package roster

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"context"
	"time"
)

// Keep account locality without holding a network connection or blocking other
// providers during cooldown. Every turn rechecks vacancies and eligibility.
func (s *Service) startRecruitment(ctx context.Context, channel string, c catalog.Candidate, v config.Settings) {
	if s.recruitAccount == 0 {
		s.recruitAccount = c.AccountID
		s.recruitVisited = map[string]bool{}
	}
	if s.recruitAccount == c.AccountID {
		s.recruitVisited[channel] = true
	}
	s.probe(ctx, c, v)
}

func (s *Service) recruitNext(ctx context.Context, channels []catalog.ChannelRoster, v config.Settings) bool {
	var state string
	var blocked int64
	err := s.Store.DB.QueryRow(`SELECT a.state,coalesce((SELECT max(p.next_probe,p.blocked_until) FROM provider_rates p WHERE p.provider=a.rate_key),0) FROM accounts a WHERE a.id=?`, s.recruitAccount).Scan(&state, &blocked)
	now := time.Now().Unix()
	if err != nil || state == "inactive" || state == "retired" || state == "manually_blocked" {
		s.recruitAccount = 0
		return false
	}
	if blocked > now*1000 {
		return false
	}
	for _, ch := range channels {
		if ch.Count >= ch.Policy.Target || s.recruitVisited[ch.ChannelID] {
			continue
		}
		rows, err := s.Store.QueueAccountCandidates(ch.ChannelID, s.recruitAccount)
		if err != nil {
			return false
		}
		if len(rows) == 0 {
			continue
		}
		s.recruitVisited[ch.ChannelID] = true
		s.probe(ctx, rows[0], v)
		return true
	}
	s.recruitAccount = 0
	return false
}
