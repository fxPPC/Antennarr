// Package guide resolves independent listings into a schedule for each permanent output.
package guide

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type Programme struct {
	Start         int64  `json:"start"`
	Stop          int64  `json:"stop"`
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle,omitempty"`
	Description   string `json:"description,omitempty"`
	Episode       string `json:"episode,omitempty"`
	EpisodeSystem string `json:"episode_system,omitempty"`
	State         string `json:"state,omitempty"`
	Origins       int    `json:"origins,omitempty"`
}

type Candidate struct {
	ID         string     `json:"id"`
	Source     string     `json:"source"`
	Station    string     `json:"station"`
	Name       string     `json:"name"`
	Now        *Programme `json:"now,omitempty"`
	Next       *Programme `json:"next,omitempty"`
	programmes []Programme
}

func stationID(origin, station string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(origin+"\x00"+station)))
}

func nowNext(rows []Programme, now int64) (*Programme, *Programme) {
	i := sort.Search(len(rows), func(i int) bool { return rows[i].Stop > now })
	if i == len(rows) {
		return nil, nil
	}
	if rows[i].Start > now {
		return nil, &rows[i]
	}
	if i+1 < len(rows) {
		return &rows[i], &rows[i+1]
	}
	return &rows[i], nil
}

type slotKey struct {
	start, stop                      int64
	title, subtitle, episode, system string
}

func key(p Programme) slotKey {
	return slotKey{p.Start, p.Stop, strings.ToLower(strings.Join(strings.Fields(p.Title), " ")), strings.ToLower(p.Subtitle), p.Episode, p.EpisodeSystem}
}
func agrees(a, b slotKey) bool {
	const tolerance = 90
	return a.title == b.title && (a.subtitle == "" || b.subtitle == "" || a.subtitle == b.subtitle) &&
		(a.episode == "" || b.episode == "" || a.system != b.system || a.episode == b.episode) &&
		a.start <= b.start+tolerance && b.start <= a.start+tolerance && a.stop <= b.stop+tolerance && b.stop <= a.stop+tolerance
}

type proposal struct {
	p        Programme
	priority int
}

func programmeOrder(a, b Programme) bool {
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	if a.Stop != b.Stop {
		return a.Stop < b.Stop
	}
	if a.Title != b.Title {
		return a.Title < b.Title
	}
	if a.Subtitle != b.Subtitle {
		return a.Subtitle < b.Subtitle
	}
	return a.Episode < b.Episode
}

// Votes are evaluated per broadcast. Multiple stations/qualities on an origin
// contribute at most one vote, and an internally divided origin abstains.
func resolve(candidates []Candidate, old []Programme, pin string, now int64) ([]Programme, bool) {
	return resolveContext(context.Background(), candidates, old, pin, now)
}
func resolveContext(ctx context.Context, candidates []Candidate, old []Programme, pin string, now int64) ([]Programme, bool) {
	var proposed []proposal
	var disputed []int64
	seen := map[slotKey]bool{}
	for _, c := range candidates {
		if pin != "" && c.ID != pin {
			continue
		}
		for _, p := range c.programmes {
			if ctx.Err() != nil {
				return nil, false
			}
			if p.Stop <= now {
				continue
			}
			if pin != "" {
				p.State = "pinned"
				p.Origins = 1
				proposed = append(proposed, proposal{p, 4})
				continue
			}
			k := key(p)
			if seen[k] {
				continue
			}
			seen[k] = true
			midpoint := p.Start + (p.Stop-p.Start)/2
			votes := map[string]bool{}
			for _, other := range candidates {
				on, _ := nowNext(other.programmes, midpoint)
				if on == nil {
					continue
				}
				same := agrees(k, key(*on))
				if previous, exists := votes[other.Source]; exists {
					votes[other.Source] = previous && same
				} else {
					votes[other.Source] = same
				}
			}
			n := 0
			for _, v := range votes {
				if v {
					n++
				}
			}
			p.Origins = n
			switch {
			case n >= 2 && n*2 > len(votes):
				p.State = "confirmed"
				proposed = append(proposed, proposal{p, 3})
			case len(votes) == 1 && n == 1:
				p.State = "single"
				proposed = append(proposed, proposal{p, 1})
			default:
				disputed = append(disputed, midpoint)
			}
		}
	}
	// Existing slots survive disagreement, provider failure and roster turnover.
	// A single source may refresh matching listings; corrections to published
	// slots require independent corroboration or a user pin.
	for _, p := range old {
		if p.Stop <= now {
			continue
		}
		p.State = "cached"
		p.Origins = 0
		proposed = append(proposed, proposal{p, 2})
	}
	sort.SliceStable(proposed, func(i, j int) bool {
		a, b := proposed[i], proposed[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.p.Origins != b.p.Origins {
			return a.p.Origins > b.p.Origins
		}
		return programmeOrder(a.p, b.p)
	})
	var out []Programme
	for _, item := range proposed {
		p := item.p
		i := sort.Search(len(out), func(i int) bool { return out[i].Stop > p.Start })
		if i < len(out) && out[i].Start < p.Stop {
			if p.State == "single" && out[i].State == "cached" {
				if agrees(key(out[i]), key(p)) {
					out[i].State = "single"
					out[i].Origins = 1
				} else {
					disputed = append(disputed, p.Start+(p.Stop-p.Start)/2)
				}
			}
			continue
		}
		out = append(out, Programme{})
		copy(out[i+1:], out[i:])
		out[i] = p
	}
	for _, midpoint := range disputed {
		on, _ := nowNext(out, midpoint)
		if on == nil || (on.State != "confirmed" && on.State != "pinned") {
			return out, true
		}
	}
	return out, false
}
