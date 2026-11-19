package engine

import (
	"sync"
	"testing"
)

func TestStalledHistoryPreservesStrikesAndResets(t *testing.T) {
	q := newHistoryQueue()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); q.run() }()
	entered, release := make(chan struct{}), make(chan struct{})
	q.add(true, func() { close(entered); <-release })
	<-entered
	for i := 0; i < 256; i++ {
		q.add(false, func() {})
	}
	if q.add(false, func() {}) {
		t.Error("optional queue exceeded budget")
	}
	strikes, total := 0, 0
	for _, failed := range []bool{true, true, false, true} {
		q.add(true, func() {
			if failed {
				strikes++
				total++
			} else {
				strikes = 0
			}
		})
	}
	q.close()
	close(release)
	wg.Wait()
	if strikes != 1 || total != 3 {
		t.Fatalf("streak=%d total=%d", strikes, total)
	}
}
