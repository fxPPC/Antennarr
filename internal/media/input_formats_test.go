package media

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceRejectsDASHExternalReferences(t *testing.T) {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("requires ffmpeg")
	}
	var requests atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(404) }))
	defer external.Close()
	manifest := `<?xml version="1.0"?><MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT8S" minBufferTime="PT1S"><Period><AdaptationSet mimeType="video/mp4"><Representation id="1" bandwidth="1000000" codecs="avc1.42c01e"><BaseURL>` + external.URL + `/</BaseURL><SegmentTemplate initialization="init.mp4" media="seg-$Number$.m4s" duration="2" startNumber="1"/></Representation></AdaptationSet></Period></MPD>`
	g, err := NewGateway(&http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/dash+xml"}}, Request: r, Body: io.NopCloser(strings.NewReader(manifest))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = g.Copy(ctx, binary, "https://fixture.example/live.mpd", func([]byte) { t.Error("unsupported DASH produced output") })
	if err == nil || ctx.Err() != nil {
		t.Fatalf("DASH not promptly rejected: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("FFmpeg bypassed gateway for external reference")
	}
}

func TestHLSInputFormats(t *testing.T) {
	fixtures := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if fixtures == "" {
		t.Skip("requires synthetic fixtures")
	}
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	for _, segmentType := range []string{"mpegts", "fmp4"} {
		t.Run(segmentType, func(t *testing.T) {
			dir := t.TempDir()
			args := []string{"-nostdin", "-v", "error", "-stream_loop", "2", "-i", filepath.Join(fixtures, "720.ts"), "-map", "0:v:0", "-map", "0:a:0", "-c", "copy"}
			if segmentType == "fmp4" {
				args = append(args, "-bsf:a", "aac_adtstoasc")
			}
			args = append(args, "-hls_time", "2", "-hls_list_size", "0", "-hls_segment_type", segmentType, filepath.Join(dir, "index.m3u8"))
			cmd := exec.Command(binary, args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture generation: %v %s", err, out)
			}
			g, err := NewGateway(&http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
				b, err := os.ReadFile(filepath.Join(dir, filepath.Base(r.URL.Path)))
				if err != nil {
					return nil, err
				}
				h := make(http.Header)
				if strings.HasSuffix(r.URL.Path, ".m3u8") {
					h.Set("Content-Type", "application/vnd.apple.mpegurl")
				}
				return &http.Response{StatusCode: 200, Header: h, Request: r, Body: io.NopCloser(bytes.NewReader(b))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			result := g.Probe(context.Background(), binary, "https://fixture.example/index.m3u8", 3*time.Second)
			if !result.Success {
				t.Fatalf("supported HLS rejected: %+v", result)
			}
		})
	}
}
