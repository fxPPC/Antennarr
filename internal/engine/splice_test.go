package engine

import (
	"antennarr/internal/media"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Exercise audio-copy packaging as Plex does, rather than accepting TS bytes alone.
func TestRepeatedSourceSplicesRemainPackageable(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("requires generated fixtures")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	consumer := exec.CommandContext(ctx, binary, "-nostdin", "-hide_banner", "-v", "warning", "-i", "pipe:0", "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-bsf:a", "aac_adtstoasc", "-f", "mp4", "-movflags", "frag_keyframe+empty_moov", "pipe:1")
	input, e := consumer.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	consumer.Stdout = io.Discard
	consumer.Stderr = &stderr
	if e = consumer.Start(); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	var writeErr error
	mux, e := media.Remux(ctx, binary, func(b []byte) {
		mu.Lock()
		defer mu.Unlock()
		if writeErr == nil {
			_, writeErr = input.Write(b)
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer mux.Close()
	samples := [][]byte{}
	for _, name := range []string{"720.ts", "480.ts"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		samples = append(samples, b)
	}
	for n := 0; n < 32; n++ {
		data := samples[n%2]
		f := &feed{bufferDuration: time.Hour}
		// Each backup joins at a different packet offset and unrelated clock.
		start := ((n * 113) % 700) * 188
		for i := start; i+1316 <= len(data); i += 1316 {
			f.buffer(data[i : i+1316])
		}
		replay := f.replay()
		if len(replay) == 0 {
			t.Fatal("no keyframe replay")
		}
		replay = replay[:len(replay)-((n*19)%80+1)]
		for _, b := range replay {
			// Fixtures arrive many times faster than real-time video. Let the
			// bounded packager drain rather than confusing overload with corruption.
			deadline := time.Now().Add(5 * time.Second)
			for !mux.Send(b) {
				if mux.Stopped() || time.Now().After(deadline) {
					t.Fatal("packager did not drain splice burst")
				}
				time.Sleep(time.Millisecond)
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	time.Sleep(time.Second)
	mux.Close()
	input.Close()
	e = consumer.Wait()
	mu.Lock()
	defer mu.Unlock()
	if e != nil || writeErr != nil {
		t.Fatalf("Plex-style audio-copy packaging failed: %v %v\n%s", e, writeErr, stderr.String())
	}
	for _, bad := range []string{"Error parsing ADTS", "AAC bitstream not in ADTS", "Error muxing", "Invalid data found"} {
		if bytes.Contains(stderr.Bytes(), []byte(bad)) {
			t.Fatal(fmt.Sprintf("corrupt splice: %s", stderr.String()))
		}
	}
	t.Logf("32 interrupted source splices packaged successfully; diagnostics=%s", stderr.String())
}
