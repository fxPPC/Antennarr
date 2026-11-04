package admin

import (
	"antennarr/internal/catalog"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImportMappingRedactionAndRequiredHeader(t *testing.T) {
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	a := API{Store: store, Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("https://tv.example/get.php?username=fixture-user&password=fixture-password"))}, nil
	})}}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(method, path, body string, header bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if header {
			r.Header.Set("X-Antennarr-Request", "1")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	body := `{"text":"https://source.example/accounts.txt","provenance":"Authorised test","authorised":true}`
	if w := call("POST", "/api/import", body, false); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/api/import", body, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitImport(t, &a)
	w := call("GET", "/api/input/accounts", "", false)
	if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-password") || strings.Contains(w.Body.String(), "fixture-user") {
		t.Fatal("account view leaked credentials")
	}
	var accounts catalog.AccountPage
	if json.Unmarshal(w.Body.Bytes(), &accounts) != nil || len(accounts.Items) != 1 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/connectors", "", false)
	if w.Code != 404 {
		t.Fatal("discovery route still exists")
	}
	w = call("POST", "/api/channels", `{"number":"101","name":"CNN","region":"US","enabled":true}`, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestManualPageImportAndImmediatePolicy(t *testing.T) {
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<div>Host: https://provider.example</div><div>User: test</div><div>Pass: unit-password</div>"))}, nil
	})}
	a := API{Store: store, Client: client}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Antennarr-Request", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/api/import", `{"text":"https://source.example/codes","provenance":"Manual test","authorised":true}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitImport(t, &a)
	w = call("POST", "/api/removal-policy", `{"account_failures":3,"stream_failures":5}`)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	p, e := store.RemovalPolicy()
	if e != nil || p.AccountFailures != 3 || p.StreamFailures != 5 {
		t.Fatal(p, e)
	}
	w = call("GET", "/api/input/accounts", "")
	if strings.Contains(w.Body.String(), "unit-password") {
		t.Fatal("default list leaked login")
	}
	w = call("POST", "/api/accounts/1/credentials", `{}`)
	if w.Code != 404 || strings.Contains(w.Body.String(), "unit-password") {
		t.Fatal("removed credential endpoint exposed a login")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func waitImport(t *testing.T, a *API) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for a.importing.Load() {
		if time.Now().After(end) {
			t.Fatal("import did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAsyncImportDoesNotHoldRequestAndGatesRestore(t *testing.T) {
	store, e := catalog.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	a := API{Store: store, Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("https://provider.example/get.php?username=fixture&password=fixture"))}, nil
	})}}
	mux := http.NewServeMux()
	a.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	request := httptest.NewRequest("POST", "/api/import", strings.NewReader(`{"text":"https://list.example/a","authorised":true}`))
	request.Header.Set("X-Antennarr-Request", "1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, request)
	if w.Code != 202 {
		close(release)
		t.Fatal(w.Code)
	}
	<-entered
	backup := httptest.NewRequest("POST", "/api/backup", strings.NewReader(`{}`))
	backup.Header.Set("X-Antennarr-Request", "1")
	bw := httptest.NewRecorder()
	mux.ServeHTTP(bw, backup)
	close(release)
	waitImport(t, &a)
	if bw.Code != 409 {
		t.Fatal("backup raced asynchronous import", bw.Code)
	}
}
