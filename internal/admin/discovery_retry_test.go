package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Removed administration endpoints must not remain an alternative mutation path.
func TestRemovedEndpointsReturnNotFound(t *testing.T) {
	a := API{}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	for _, route := range []string{"GET /api/activity", "GET /api/health", "GET /api/accounts", "GET /api/streams", "GET /api/assignments", "POST /api/accounts/1/refresh", "POST /api/accounts/1/restore", "POST /api/streams/1/restore", "POST /api/channels/us-preset"} {
		method, path, _ := strings.Cut(route, " ")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != 404 && w.Code != 405 {
			t.Fatalf("%s: %d", route, w.Code)
		}
	}
}
