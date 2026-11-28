package guide

import (
	"math/rand"
	"testing"
)

func TestCorroboratedProgrammeTimesSurviveSkewAndTurnover(t *testing.T) {
	show := func(title string, start, stop int64) Programme {
		return Programme{Title: title, Start: start, Stop: stop}
	}
	guide := func(origin string, rows ...Programme) Candidate { return Candidate{Source: origin, programmes: rows} }
	a := show("News", 1000, 2800)
	b := show("Film", 2800, 6400)
	skew := show("News", 980, 2830)
	for _, tc := range []struct {
		name    string
		sources []Candidate
	}{
		{"minority timing", []Candidate{guide("A", skew), guide("B", a, b), guide("C", a, b)}},
		{"credible slots share a boundary", []Candidate{guide("A", skew), guide("B", a, b), guide("C", b)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for seed := int64(0); seed < 32; seed++ {
				rand.New(rand.NewSource(seed)).Shuffle(len(tc.sources), func(i, j int) { tc.sources[i], tc.sources[j] = tc.sources[j], tc.sources[i] })
				got, conflict := resolve(tc.sources, nil, "", 999)
				if conflict || len(got) != 2 || got[0].Start != a.Start || got[0].Stop != a.Stop || got[1].Start != b.Start || got[1].Stop != b.Stop {
					t.Fatalf("seed %d: received %+v, conflict %v", seed, got, conflict)
				}
			}
		})
	}
}

func TestAdjacentConfirmedProgrammesCanDisagreeOnTheirSeam(t *testing.T) {
	first := Programme{Title: "News", Start: 1000, Stop: 2830}
	last := Programme{Title: "Drama", Start: 2800, Stop: 6400}
	source := func(origin string, p Programme) Candidate {
		return Candidate{Source: origin, programmes: []Programme{p}}
	}
	for _, d := range []int64{1, 30, 90, 91} {
		first.Stop = last.Start + d
		cs := []Candidate{source("A", first), source("B", first), source("C", last), source("D", last)}
		for seed := int64(0); seed < 16; seed++ {
			rand.New(rand.NewSource(seed)).Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
			got, _ := resolve(cs, nil, "", 999)
			if d > broadcastTolerance {
				if len(got) > 1 {
					t.Fatalf("artificially reconciled a %ds disagreement: %+v", d, got)
				}
			} else if len(got) != 2 || got[0].Title != first.Title || got[1].Title != last.Title || got[0].Stop != got[1].Start || got[0].Start != first.Start || got[1].Stop != last.Stop {
				t.Fatalf("lost a corroborated programme for %ds disagreement: %+v", d, got)
			}
		}
	}
}
