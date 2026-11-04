package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestMuxQueueAcceptsWholeStandbyReplay(t *testing.T) {
	m := &Mux{Input: make(chan []byte, muxInputSlots), done: make(chan struct{})}
	for n := 0; n < 4<<20; n += 1316 {
		if !m.Send(make([]byte, 1316)) {
			t.Fatalf("standby replay overflow at %d bytes", n)
		}
	}
}

func TestRealMuxStandbyBurst(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("requires media fixtures")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	sample, e := os.ReadFile(filepath.Join(dir, "720.ts"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var output atomic.Int64
	m, e := Remux(ctx, binary, func(b []byte) { output.Add(int64(len(b))) })
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	// Fill almost 4 MiB without waiting for the writer or output to catch up.
	total := 0
	for total < 4<<20 {
		for i := 0; i+1316 <= len(sample) && total < 4<<20; i += 1316 {
			if !m.Send(sample[i : i+1316]) {
				t.Fatalf("real replay overflow at %d bytes", total)
			}
			total += 1316
		}
	}
	for output.Load() == 0 && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if output.Load() == 0 {
		t.Fatal("remux produced no output")
	}
}
