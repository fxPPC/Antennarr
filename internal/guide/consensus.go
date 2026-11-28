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

const broadcastTolerance = 90

func sameBroadcast(a, b slotKey) bool {
	return a.title == b.title && a.start <= b.start+broadcastTolerance && b.start <= a.start+broadcastTolerance && a.stop <= b.stop+broadcastTolerance && b.stop <= a.stop+broadcastTolerance
}
func agrees(a, b slotKey) bool {
	return sameBroadcast(a, b) && (a.subtitle == "" || b.subtitle == "" || a.subtitle == b.subtitle) && (a.episode == "" || b.episode == "" || a.system != b.system || a.episode == b.episode)
}

type proposal struct {
	p              Programme
	priority       int
	exactTimes     int
	incumbentTime  bool
	detailConflict bool
}

// An origin must tell one consistent story about this time, even if none of
// its variants vote for the proposed title. Compare all variants so a sparse
// third row cannot bridge two contradictory episodes.
func matchingBallots(candidates []Candidate, midpoint int64, target slotKey) ([]Programme, int, int) {
	groups := make(map[string][]*Programme)
	for _, c := range candidates {
		if p, _ := nowNext(c.programmes, midpoint); p != nil {
			groups[c.Source] = append(groups[c.Source], p)
		}
	}
	var votes []Programme
	eligible, exactTimes := 0, 0
	for _, rows := range groups {
		var representative Programme
		divided := false
		supports, exact := true, true
		for i, row := range rows {
			k := key(*row)
			if k.start != target.start || k.stop != target.stop {
				exact = false
			}
			if !sameBroadcast(target, k) {
				supports = false
			}
			for _, other := range rows[:i] {
				if !agrees(k, key(*other)) {
					divided = true
					break
				}
			}
			if divided {
				break
			}
			if row.Episode != "" && (representative.Episode == "" || preferredScheme(row.EpisodeSystem, representative.EpisodeSystem)) {
				representative.Episode = row.Episode
				representative.EpisodeSystem = row.EpisodeSystem
			}
			if row.Subtitle != "" && (representative.Subtitle == "" || row.Subtitle < representative.Subtitle) {
				representative.Subtitle = row.Subtitle
			}
			if len(row.Description) > len(representative.Description) || (len(row.Description) == len(representative.Description) && row.Description < representative.Description) {
				representative.Description = row.Description
			}
		}
		if divided {
			continue
		}
		eligible++
		if supports {
			votes = append(votes, representative)
			if exact {
				exactTimes++
			}
		}
	}
	return votes, eligible, exactTimes
}

type episodeID struct{ number, system string }

func episodeOf(p Programme) episodeID {
	if p.Episode == "" {
		return episodeID{}
	}
	return episodeID{p.Episode, p.EpisodeSystem}
}

// Empty metadata abstains. Revising a known value needs two independent
// positive votes; a first, unopposed value may populate an empty guide.
func chooseDetail[T comparable](counts map[T]int, incumbent T) (T, bool) {
	var zero T
	total, best := 0, 0
	winner := incumbent
	for value, n := range counts {
		total += n
		if n > best {
			best = n
			winner = value
		}
	}
	if total == 0 {
		return incumbent, false
	}
	if best*2 <= total || (winner != incumbent && incumbent != zero && best < 2) {
		return incumbent, true
	}
	return winner, false
}

func preferredScheme(a, b string) bool {
	if a == b {
		return false
	}
	if a == "xmltv_ns" || b == "xmltv_ns" {
		return a == "xmltv_ns"
	}
	return a < b
}

// XMLTV episode identifiers from different systems are not directly
// comparable. Prefer an established namespace while it is still reported.
// Otherwise use the best represented one; chooseDetail requires two
// independent matching IDs to replace an incumbent from another namespace.
func episodeScheme(votes []Programme, prior Programme) string {
	counts := make(map[string]int)
	best, scheme := 0, ""
	for _, row := range votes {
		if row.Episode == "" {
			continue
		}
		name := row.EpisodeSystem
		if prior.Episode != "" && name == prior.EpisodeSystem {
			return name
		}
		counts[name]++
		if counts[name] > best || (counts[name] == best && preferredScheme(name, scheme)) {
			best = counts[name]
			scheme = name
		}
	}
	return scheme
}

func reconcileDetails(p Programme, votes []Programme, previous *Programme) (Programme, bool) {
	prior := Programme{}
	if previous != nil && sameBroadcast(key(p), key(*previous)) {
		prior = *previous
	}
	scheme := episodeScheme(votes, prior)
	counts := make(map[episodeID]int)
	for _, row := range votes {
		if row.Episode != "" && row.EpisodeSystem == scheme {
			counts[episodeOf(row)]++
		}
	}
	selected, conflict := chooseDetail(counts, episodeOf(prior))
	p.Episode, p.EpisodeSystem = selected.number, selected.system
	// A corrected episode must never inherit the previous episode's subtitle
	// or synopsis. Two disagreeing episode IDs cannot identify a new episode.
	if prior.Episode != "" && episodeOf(prior) != selected {
		prior = Programme{}
	}
	p.Subtitle, p.Description = prior.Subtitle, prior.Description
	// The attempted namespace switch lacked corroboration. The retained
	// ID still belongs to its previously accepted subtitle and description.
	if conflict && (selected.number == "" || selected.system != scheme) {
		return p, true
	}
	subtitles := make(map[string]int)
	display := map[string]string{norm(prior.Subtitle): prior.Subtitle}
	for _, row := range votes {
		if row.Episode != "" && episodeOf(row) != selected {
			continue
		}
		if name := norm(row.Subtitle); name != "" {
			subtitles[name]++
			if display[name] == "" || row.Subtitle < display[name] {
				display[name] = row.Subtitle
			}
		}
	}
	subtitle, split := chooseDetail(subtitles, norm(prior.Subtitle))
	p.Subtitle = display[subtitle]
	if norm(p.Subtitle) != norm(prior.Subtitle) {
		p.Description = ""
	}
	for _, row := range votes {
		if row.Episode != "" && episodeOf(row) != selected {
			continue
		}
		if row.Subtitle != "" && norm(row.Subtitle) != subtitle {
			continue
		}
		if len(row.Description) > len(p.Description) || (len(row.Description) == len(p.Description) && row.Description < p.Description) {
			p.Description = row.Description
		}
	}
	return p, conflict || split
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
				proposed = append(proposed, proposal{p: p, priority: 4})
				continue
			}
			k := key(p)
			// All episode and subtitle evidence for this broadcast is
			// reconciled together. Sparse metadata cannot nominate a duplicate.
			k.subtitle, k.episode, k.system = "", "", ""
			if seen[k] {
				continue
			}
			seen[k] = true
			midpoint := p.Start + (p.Stop-p.Start)/2
			votes, eligible, exactTimes := matchingBallots(candidates, midpoint, k)
			prior, _ := nowNext(old, midpoint)
			p, split := reconcileDetails(p, votes, prior)
			n := len(votes)
			p.Origins = n
			switch {
			case n >= 2 && n*2 > eligible:
				p.State = "confirmed"
				incumbentTime := prior != nil && p.Start == prior.Start && p.Stop == prior.Stop && key(*prior).title == k.title
				proposed = append(proposed, proposal{p: p, priority: 3, exactTimes: exactTimes, incumbentTime: incumbentTime, detailConflict: split})
			case eligible == 1 && n == 1:
				p.State = "single"
				proposed = append(proposed, proposal{p: p, priority: 1, detailConflict: split})
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
		proposed = append(proposed, proposal{p: p, priority: 2})
	}
	sort.SliceStable(proposed, func(i, j int) bool {
		a, b := proposed[i], proposed[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.p.Origins != b.p.Origins {
			return a.p.Origins > b.p.Origins
		}
		// Agreement on the programme allows clock skew. Pick the best-supported
		// actual interval before falling back to chronological order.
		if a.exactTimes != b.exactTimes {
			return a.exactTimes > b.exactTimes
		}
		if a.incumbentTime != b.incumbentTime {
			return a.incumbentTime
		}
		return programmeOrder(a.p, b.p)
	})
	var out []Programme
	var seams []proposal
	detailConflict := false
	for _, item := range proposed {
		p := item.p
		i := sort.Search(len(out), func(i int) bool { return out[i].Stop > p.Start })
		if i < len(out) && out[i].Start < p.Stop {
			if p.State == "confirmed" && out[i].State == "confirmed" &&
				(p.Stop-out[i].Start <= broadcastTolerance || out[i].Stop-p.Start <= broadcastTolerance) {
				seams = append(seams, item)
			}
			if p.State == "single" && out[i].State == "cached" {
				if agrees(key(out[i]), key(p)) {
					out[i].State = "single"
					out[i].Origins = 1
					detailConflict = detailConflict || item.detailConflict
				} else {
					disputed = append(disputed, p.Start+(p.Stop-p.Start)/2)
				}
			}
			continue
		}
		out = append(out, Programme{})
		copy(out[i+1:], out[i:])
		out[i] = p
		detailConflict = detailConflict || item.detailConflict
	}
	// First use any non-overlapping times actually supplied by the guides.
	// If disjoint groups confirm consecutive shows with a small discrepancy
	// at the junction, keep both using the higher-ranked neighbour's boundary.
	for _, item := range seams {
		p := item.p
		midpoint := p.Start + (p.Stop-p.Start)/2
		i := sort.Search(len(out), func(i int) bool { return out[i].Stop > midpoint })
		if i < len(out) && out[i].Start <= midpoint {
			continue // This broadcast already has a selected interval.
		}
		if i > 0 && out[i-1].Stop > p.Start {
			if out[i-1].State != "confirmed" || out[i-1].Stop-p.Start > broadcastTolerance {
				continue
			}
			p.Start = out[i-1].Stop
		}
		if i < len(out) && out[i].Start < p.Stop {
			if out[i].State != "confirmed" || p.Stop-out[i].Start > broadcastTolerance {
				continue
			}
			p.Stop = out[i].Start
		}
		out = append(out, Programme{})
		copy(out[i+1:], out[i:])
		out[i] = p
		detailConflict = detailConflict || item.detailConflict
	}
	for _, midpoint := range disputed {
		on, _ := nowNext(out, midpoint)
		if on == nil || (on.State != "confirmed" && on.State != "pinned") {
			return out, true
		}
	}
	return out, detailConflict
}
