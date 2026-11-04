package admin

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"antennarr/internal/recovery"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBulkConfirmationIdleAndFactoryReset(t *testing.T) {
	root := t.TempDir()
	config.Load(root)
	s, e := catalog.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SaveChannel(catalog.Channel{Number: "1", Name: "Custom", Enabled: true})
	idle := false
	restart := false
	a := API{Store: s, DataRoot: root, Idle: func() bool { return idle }, Restart: func() { restart = true }}
	call := func(body string, reset bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if reset {
			a.resetApplication(w, r)
		} else {
			a.bulkChannels(w, r)
		}
		return w
	}
	if call(`{}`, false).Code != 400 || call(`{}`, true).Code != 400 {
		t.Fatal("confirmation bypass")
	}
	if call(`{"confirmed":true}`, false).Code != 409 || call(`{"confirmed":true}`, true).Code != 409 {
		t.Fatal("active viewer bypass")
	}
	idle = true
	if w := call(`{"confirmed":true,"default_us":true}`, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	chs, _ := s.Channels()
	if len(chs) != 143 {
		t.Fatal(len(chs))
	}
	if w := call(`{"confirmed":true}`, true); w.Code != 200 || !restart || !a.maintenance.Load() {
		t.Fatal(w.Code, restart)
	}
	if e = recovery.ApplyReset(root); e != nil {
		t.Fatal(e)
	}
}
