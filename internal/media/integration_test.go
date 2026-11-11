package media

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRealMediaProbe(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("set ANTENNARR_TEST_FIXTURES to generated fixture directory")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	sample, e := os.ReadFile(filepath.Join(dir, "720.ts"))
	if e != nil {
		t.Fatal(e)
	}
	client := &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(sample)), Header: http.Header{"Content-Type": []string{"video/mp2t"}}, Request: r}, nil
	})}
	g, e := NewGateway(client)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	result := g.Probe(context.Background(), binary, "https://authorised-fixture.example/test.ts", 3*time.Second)
	if !result.Success || result.Codec != "h264" || result.Height != 720 {
		t.Fatalf("%+v", result)
	}
	t.Logf("validated media: %+v", result)
}

// Broadcast TS can advertise audio in its PMT before an audio packet arrives.
// A short byte probe then identifies AAC but cannot discover its sample rate,
// so codec-copy remuxing fails even though the complete source is valid.
func TestMediaProbeWaitsForDelayedAudio(t *testing.T) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("set ANTENNARR_TEST_FIXTURES to generated fixture directory")
	}
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	// Leave ample media after probing. A short finite clip can reach EOF just
	// before the sample timer on a slower CI runner, although startup worked.
	sample, err := exec.Command(binary, "-v", "error", "-stream_loop", "2", "-i", filepath.Join(dir, "720.ts"), "-c", "copy", "-f", "mpegts", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	// The synthetic fixture uses video PID 256 and audio PID 257. Keep its
	// programme tables while withholding audio beyond the old 256 KiB probe.
	var delayed []byte
	dropped := 0
	for i := 0; i+188 <= len(sample); i += 188 {
		packet := sample[i : i+188]
		pid := int(packet[1]&31)<<8 | int(packet[2])
		if pid == 257 && i < 750000 {
			dropped++
			continue
		}
		delayed = append(delayed, packet...)
	}
	if dropped == 0 || len(sample) < 1000000 {
		t.Fatal("fixture lacks enough delayed audio")
	}
	client := &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(delayed)), Header: http.Header{"Content-Type": []string{"video/mp2t"}}, Request: r}, nil
	})}
	g, err := NewGateway(client)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	result := g.Probe(context.Background(), binary, "https://authorised-fixture.example/delayed.ts", 3*time.Second)
	if !result.Success || result.Codec != "h264" || result.Height != 720 {
		t.Fatalf("valid source with delayed audio rejected: %+v", result)
	}
}
