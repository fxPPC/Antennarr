package media

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHLSRewritingAndPrivateReferenceBlock(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		requests++
		body := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:2,\nseg.ts\n"
		if strings.HasSuffix(r.URL.Path, "bad.m3u8") {
			body = "#EXTM3U\nhttp://169.254.169.254/latest/meta-data\n"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}
	g, e := NewGateway(client)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	local, release, e := g.Add(context.Background(), "https://tv.example/live/main.m3u8")
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	r, e := http.Get(local)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || strings.Contains(string(b), "tv.example") || strings.Contains(string(b), "seg.ts") || strings.Count(string(b), g.base) != 2 {
		t.Fatal(string(b))
	}
	bad, rb, _ := g.Add(context.Background(), "https://tv.example/bad.m3u8")
	defer rb()
	r, e = http.Get(bad)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 502 {
		t.Fatal("private HLS reference accepted")
	}
	if requests != 2 {
		t.Fatal(requests)
	}
	if _, _, e = g.Add(context.Background(), "http://127.0.0.1:9001"); e == nil {
		t.Fatal("private source accepted")
	}
	release()
	r, _ = http.Get(local)
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatal("released source accessible")
	}
}
