package budget

import (
	"sync"
	"testing"
)

func TestLimitsPriorityAndRelease(t *testing.T) {
	b := New(10, 5)
	r, ok := b.Reserve(1, "p", 1, true)
	if !ok {
		t.Fatal("live denied")
	}
	if _, ok = b.Reserve(1, "p", 1, true); ok {
		t.Fatal("account cap ignored")
	}
	if _, ok = b.Reserve(2, "q", 1, false); ok {
		t.Fatal("background competed with live")
	}
	r()
	r()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if release, ok := b.Reserve(int64(i), "p", 1, true); ok {
				if b.Snapshot().Total > 5 {
					t.Error("provider cap ignored")
				}
				release()
			}
		}(i)
	}
	wg.Wait()
	if b.Snapshot().Total != 0 {
		t.Fatal("leaked budget")
	}
}
