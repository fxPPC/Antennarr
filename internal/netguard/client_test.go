package netguard

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestBlockedDestinations(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "172.17.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "198.18.0.1", "224.0.0.1", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2002:a00:1::"} {
		if Public(netip.MustParseAddr(s)) {
			t.Fatal("allowed", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !Public(netip.MustParseAddr(s)) {
			t.Fatal("blocked public", s)
		}
	}
	for _, s := range []string{"file:///etc/passwd", "http://localhost:9001/", "http://169.254.169.254/", "http://docker.local/"} {
		u, _ := url.Parse(s)
		if URL(u) == nil {
			t.Fatal("unsafe URL", s)
		}
	}
}

func TestLiveConnectionsAreNotCappedBelowTheStreamBudget(t *testing.T) {
	entered := make(chan struct{}, 6)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.Write([]byte("fixture"))
	}))
	defer server.Close()
	client := Client(3 * time.Second)
	transport := client.Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, _ string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	var wg sync.WaitGroup
	wg.Add(6)
	for i := 0; i < 6; i++ {
		go func() {
			defer wg.Done()
			r, e := client.Get("http://fixture.example/live")
			if e == nil {
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
			}
		}()
	}
	arrived := 0
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
loop:
	for arrived < 6 {
		select {
		case <-entered:
			arrived++
		case <-timer.C:
			break loop
		}
	}
	close(release)
	wg.Wait()
	if arrived != 6 {
		t.Fatalf("transport blocked configured parallel streams: %d/6", arrived)
	}
}
