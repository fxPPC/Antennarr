package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStopOutgoingRequiresHeaderAndTargetsChannel(t *testing.T) {
	called := ""
	a := API{StopStream: func(id string) bool { called = id; return true }}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	for _, header := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/api/outgoing/channel-a/stop", nil)
		if header {
			r.Header.Set("X-Antennarr-Request", "1")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if !header && (w.Code != 403 || called != "") {
			t.Fatal("unprotected mutation")
		}
		if header && (w.Code != 200 || called != "channel-a") {
			t.Fatal(w.Code, called)
		}
	}
}
