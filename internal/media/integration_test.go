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
