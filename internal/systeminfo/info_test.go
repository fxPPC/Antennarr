package systeminfo

import (
	"testing"
	"time"
)

func TestModeAveragesExcludeIdleAndExpired(t *testing.T) {
	c := New(t.TempDir(), func() (bool, bool) { return false, false })
	c.samples = []sample{{time.Now(), 2, false}, {time.Now(), 4, false}, {time.Now(), 6, true}, {time.Now().Add(-2 * time.Hour), 99, true}}
	v := c.Snapshot()
	if v.Background.Load != 3 || v.Background.Samples != 2 || v.Streaming.Load != 6 || v.Streaming.Samples != 1 {
		t.Fatal(v)
	}
	if v.DiskTotal == 0 {
		t.Fatal("no disk info")
	}
}
