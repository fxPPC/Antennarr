package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"antennarr/internal/budget"
	"antennarr/internal/catalog"
)

func TestPortalAccountLinkRotationAndPlaybackPriority(t *testing.T) {
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	ch, e := store.SaveChannel(catalog.Channel{Name: "Fixture TV", Number: "1", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	limits := budget.New(3, 2)
	var calls atomic.Int64
	var stall atomic.Bool
	entered := make(chan struct{})
	var reply atomic.Value
	reply.Store("#EXTM3U\n#EXTINF:0 tvg-id=\"fixture\",Fixture TV\nhttps://cdn.example/stream?token=first\n")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if stall.Load() {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(reply.Load().(string)))}, nil
	})}
	api := &API{Store: store, Client: client, PortalBudget: limits.Reserve, PortalWork: &PortalWork{}}
	mux := http.NewServeMux()
	api.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	post := func(path, b string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(b))
		req.Header.Set("X-Antennarr-Request", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	cfg := `{"portal_url":"https://tv.example/root","playlist_url":"https://tv.example/account?signature=private","mac":"02:00:00:00:00:01"}`
	w := post("/api/input/portal", cfg)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"channels":1`) || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	queue, err := store.InputPage("", 0)
	if err != nil || len(queue.Items) != 1 || queue.Items[0].Kind != "portal" {
		t.Fatal("portal account cannot be identified for refresh")
	}
	rows, e := store.Candidates(ch, false)
	if e != nil || len(rows) != 1 || rows[0].Limit != 1 || !strings.HasPrefix(rows[0].URL, "portal:") {
		t.Fatalf("candidates %+v %v", rows, e)
	}
	ref := rows[0].URL
	reply.Store(strings.ReplaceAll(reply.Load().(string), "first", "renewed"))
	got, e := store.ResolvePortalStream(context.Background(), client, ref)
	if e != nil || !strings.HasSuffix(got, "renewed") {
		t.Fatalf("resolve %q %v", got, e)
	}
	const refresh = "/api/input/portal/refresh"
	const payload = `{"account_id":1}`
	occupied, ok := limits.Reserve(rows[0].AccountID, "tv.example", 1, true)
	if !ok {
		t.Fatal("cannot reserve")
	}
	before := calls.Load()
	if w = post(refresh, payload); w.Code != 409 || calls.Load() != before {
		t.Fatal("busy account contacted")
	}
	occupied()
	stall.Store(true)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- post(refresh, payload) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh never began")
	}
	api.PortalWork.Preempt()
	if limits.AccountUsage(rows[0].AccountID) != 0 {
		t.Fatal("cancelled refresh retained the live connection slot")
	}
	if w = <-done; w.Code != 409 {
		t.Fatal("unexpected interrupted refresh result", w.Code)
	}
	stall.Store(false)
	reply.Store("#EXTM3U\n")
	if _, e = store.ResolvePortalStream(context.Background(), client, ref); e == nil {
		t.Fatal("removed channel played")
	}
	if w = post(refresh, payload); w.Code != 200 {
		t.Fatalf("refresh: %s", w.Body)
	}
	rows, e = store.Candidates(ch, false)
	if e != nil || len(rows) != 0 {
		t.Fatalf("removed channel persisted as eligible: %+v %v", rows, e)
	}
}
