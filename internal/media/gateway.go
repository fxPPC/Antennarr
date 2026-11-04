// Gateway confines FFmpeg to loopback. Go validates and pins all external dials,
// including nested HLS playlists, keys, maps, and segments.
package media

import (
	"antennarr/internal/netguard"
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Gateway struct {
	mu       sync.Mutex
	sessions map[string]*origin
	client   *http.Client
	server   *http.Server
	base     string
	slots    chan struct{}
}
type origin struct {
	ctx     context.Context
	mu      sync.Mutex
	urls    map[string]string
	reverse map[string]string
	next    uint64
}

var uriAttr = regexp.MustCompile(`URI="([^"]*)"`)

func NewGateway(client *http.Client) (*Gateway, error) {
	if client == nil {
		client = netguard.Client(0)
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	g := &Gateway{sessions: map[string]*origin{}, client: client, base: "http://" + ln.Addr().String(), slots: make(chan struct{}, 256)}
	g.server = &http.Server{Handler: g, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	go g.server.Serve(ln)
	return g, nil
}
func (g *Gateway) Close() error { return g.server.Close() }
func (g *Gateway) Add(ctx context.Context, raw string) (string, func(), error) {
	u, e := url.Parse(raw)
	if e != nil || netguard.URL(u) != nil {
		return "", nil, errors.New("source URL blocked")
	}
	b := make([]byte, 24)
	if _, e = rand.Read(b); e != nil {
		return "", nil, e
	}
	token := hex.EncodeToString(b)
	o := &origin{ctx: ctx, urls: map[string]string{"0": raw}, reverse: map[string]string{raw: "0"}, next: 1}
	g.mu.Lock()
	g.sessions[token] = o
	g.mu.Unlock()
	var once sync.Once
	return g.base + "/" + token + "/0", func() { once.Do(func() { g.mu.Lock(); delete(g.sessions, token); g.mu.Unlock() }) }, nil
}
func (g *Gateway) resolve(o *origin, token string, parent *url.URL, raw string) (string, error) {
	ref, e := url.Parse(raw)
	if e != nil {
		return "", errors.New("invalid playlist reference")
	}
	u := parent.ResolveReference(ref)
	if e = netguard.URL(u); e != nil {
		return "", e
	}
	raw = u.String()
	o.mu.Lock()
	defer o.mu.Unlock()
	id, ok := o.reverse[raw]
	if !ok {
		if len(o.urls) >= 4096 {
			// Retain playlist references while evicting old media URLs. A source cannot grow unbounded.
			cutoff := uint64(0)
			if o.next > 2048 {
				cutoff = o.next - 2048
			}
			for old, value := range o.urls {
				n, _ := strconv.ParseUint(old, 10, 64)
				if n > 0 && n < cutoff && !strings.Contains(strings.ToLower(value), "m3u8") {
					delete(o.urls, old)
					delete(o.reverse, value)
				}
			}
			if len(o.urls) >= 4096 {
				return "", errors.New("too many playlist references")
			}
		}
		id = strconv.FormatUint(o.next, 10)
		o.next++
		o.urls[id] = raw
		o.reverse[raw] = id
	}
	return g.base + "/" + token + "/" + id, nil
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	g.mu.Lock()
	o := g.sessions[parts[0]]
	g.mu.Unlock()
	if o == nil {
		http.NotFound(w, r)
		return
	}
	o.mu.Lock()
	raw, ok := o.urls[parts[1]]
	o.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithCancel(o.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	case <-ctx.Done():
		return
	}
	u, e := url.Parse(raw)
	if e != nil || netguard.URL(u) != nil {
		http.Error(w, "Source blocked", 502)
		return
	}
	req, e := http.NewRequestWithContext(ctx, r.Method, raw, nil)
	if e != nil {
		http.Error(w, "Invalid source", 502)
		return
	}
	req.Header.Set("User-Agent", "Antennarr/0.1")
	if v := r.Header.Get("Range"); v != "" && len(v) < 100 {
		req.Header.Set("Range", v)
	}
	upstream, e := g.client.Do(req)
	if e != nil {
		http.Error(w, "Source unavailable", 502)
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode != 200 && upstream.StatusCode != 206 {
		http.Error(w, "Source unavailable", 502)
		return
	}
	body := bufio.NewReaderSize(upstream.Body, 4096)
	prefix, _ := body.Peek(7)
	if strings.EqualFold(string(prefix), "#EXTM3U") || strings.Contains(strings.ToLower(upstream.Header.Get("Content-Type")), "mpegurl") {
		b, e := io.ReadAll(io.LimitReader(body, (1<<20)+1))
		if e != nil || len(b) > 1<<20 {
			http.Error(w, "Playlist exceeds limit", 502)
			return
		}
		lines := strings.Split(string(b), "\n")
		var rewriteErr error
		for i, line := range lines {
			line = strings.TrimSuffix(line, "\r")
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				lines[i] = uriAttr.ReplaceAllStringFunc(line, func(attr string) string {
					m := uriAttr.FindStringSubmatch(attr)
					dest, e := g.resolve(o, parts[0], upstream.Request.URL, m[1])
					if e != nil {
						rewriteErr = e
						return ""
					}
					return `URI="` + dest + `"`
				})
			} else if strings.TrimSpace(line) != "" {
				lines[i], rewriteErr = g.resolve(o, parts[0], upstream.Request.URL, strings.TrimSpace(line))
			}
			if rewriteErr != nil {
				http.Error(w, "Playlist reference blocked", 502)
				return
			}
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, strings.Join(lines, "\n"))
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := upstream.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(upstream.StatusCode)
	_, _ = io.CopyBuffer(w, body, make([]byte, 32<<10))
}
