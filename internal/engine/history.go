package engine

import (
	"errors"
	"sync"
	"time"
)

// A channel's health results must stay in order: success resets its failure streak.
// Event history may be sampled under pressure. Playback never waits on a DB write.
type historyQueue struct {
	mu      sync.Mutex
	ready   *sync.Cond
	pending []func()
	closed  bool
}

func newHistoryQueue() *historyQueue {
	q := &historyQueue{}
	q.ready = sync.NewCond(&q.mu)
	return q
}
func (q *historyQueue) add(health bool, fn func()) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !health && len(q.pending) >= 256 {
		return false
	}
	q.pending = append(q.pending, fn)
	q.ready.Signal()
	return true
}
func (q *historyQueue) size() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.pending) }
func (q *historyQueue) close()    { q.mu.Lock(); q.closed = true; q.ready.Signal(); q.mu.Unlock() }
func (q *historyQueue) run() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		for len(q.pending) == 0 && !q.closed {
			q.ready.Wait()
		}
		if len(q.pending) == 0 {
			return
		}
		fn := q.pending[0]
		q.pending[0] = nil
		q.pending = q.pending[1:]
		q.mu.Unlock()
		fn()
		q.mu.Lock()
	}
}

// SQLite BUSY and LOCKED mean the failed transaction did not commit. Retry on
// the writer, keeping later successes behind the strike that precedes them.
func saveStrike(write func() error) error {
	for {
		err := write()
		var coded interface{ Code() int }
		if !errors.As(err, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
