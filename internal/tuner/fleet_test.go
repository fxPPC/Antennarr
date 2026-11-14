package tuner

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/stream"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func fleetFixture(t *testing.T) (*Fleet, catalog.Profile) {
	t.Helper()
	store, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	p, err := store.AddProfilePreset("uk-london")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ReplaceProfileChannels(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []catalog.Channel{{ID: "us-test", ProfileID: "primary", Number: "101", Name: "US fixture", Region: "US", Enabled: true}, {ID: "uk-test", ProfileID: p.ID, Number: "101", Name: "UK fixture", Region: "GB", Enabled: true}} {
		if _, err = store.SaveChannel(c); err != nil {
			t.Fatal(err)
		}
	}
	p.Enabled = true
	if p, err = store.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	ports, _ := config.ParseTunerPorts("")
	return &Fleet{Store: store, Template: testServer(t), Ports: ports}, p
}
func TestFleetDistinctDevicesAndScopedLineups(t *testing.T) {
	f, p := fleetFixture(t)
	devices := []map[string]any{}
	for offset := 0; offset < 2; offset++ {
		w := httptest.NewRecorder()
		f.Handler(offset).ServeHTTP(w, httptest.NewRequest("GET", "/discover.json", nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var d map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		devices = append(devices, d)
		w = httptest.NewRecorder()
		f.Handler(offset).ServeHTTP(w, httptest.NewRequest("GET", "/lineup.json", nil))
		var rows []map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
			t.Fatal(w.Body.String(), err)
		}
		want := "US fixture"
		base := "http://172.17.0.8:9192"
		if offset == 1 {
			want = "UK fixture"
			base = "http://172.17.0.8:9200"
		}
		if rows[0]["GuideName"] != want || rows[0]["URL"] != base+"/auto/v101" {
			t.Fatal(rows)
		}
		for _, path := range []string{"/admin", "/api/profiles", "/api/status"} {
			w = httptest.NewRecorder()
			f.Handler(offset).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 404 {
				t.Fatalf("extra tuner exposed administration: %d %s", w.Code, path)
			}
		}
	}
	if devices[0]["DeviceID"] == devices[1]["DeviceID"] || devices[0]["BaseURL"] == devices[1]["BaseURL"] {
		t.Fatal(devices)
	}
	if devices[1]["FriendlyName"] != "UK - London" {
		t.Fatal(devices)
	}
	p.Enabled = false
	f.Store.SaveProfile(p)
	w := httptest.NewRecorder()
	f.Handler(1).ServeHTTP(w, httptest.NewRequest("GET", "/auto/v101", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	if err := f.Store.DeleteProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	f.Handler(1).ServeHTTP(w, httptest.NewRequest("GET", "/discover.json", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	next, err := f.Store.AddProfilePreset("uk-london")
	if err != nil || next.PortOffset != 2 || next.DeviceID == p.DeviceID {
		t.Fatal(next, err)
	}
}

func TestFleetSharesSameChannelButSeparatesMarketsAndGlobalLimit(t *testing.T) {
	f, p := fleetFixture(t)
	f.Template.Streams.Close()
	var mu sync.Mutex
	starts := map[string]int{}
	manager := stream.New(3, time.Millisecond, func(ctx context.Context, id string, emit func([]byte)) error {
		mu.Lock()
		starts[id]++
		mu.Unlock()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				emit(make([]byte, 1316))
			}
		}
	})
	defer manager.Close()
	f.Template.Streams = manager
	us := httptest.NewServer(f.Handler(0))
	defer us.Close()
	uk := httptest.NewServer(f.Handler(1))
	defer uk.Close()
	var clients []*http.Response
	defer func() {
		for _, r := range clients {
			r.Body.Close()
		}
	}()
	for _, base := range []string{us.URL, us.URL, uk.URL} {
		r, err := http.Get(base + "/auto/v101")
		if err != nil || r.StatusCode != 200 {
			t.Fatal(r, err)
		}
		clients = append(clients, r)
		b := make([]byte, 1316)
		if _, err = io.ReadFull(r.Body, b); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	actual := map[string]int{}
	for k, v := range starts {
		actual[k] = v
	}
	mu.Unlock()
	if !reflect.DeepEqual(actual, map[string]int{"us-test": 1, "uk-test": 1}) {
		t.Fatal(actual)
	}
	r, err := http.Get(uk.URL + "/auto/v101")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 503 {
		t.Fatal("global viewer cap bypassed", r.StatusCode)
	}
	// Disabling blocks new sessions without tearing down accepted playback.
	p.Enabled = false
	if _, err = f.Store.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 1316)
	if _, err = io.ReadFull(clients[2].Body, b); err != nil {
		t.Fatal("disabled tuner interrupted existing viewer", err)
	}
}
