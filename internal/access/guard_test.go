package access

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyIdentityCannotBeSpoofed(t *testing.T) {
	g, e := New("172.18.0.2/32")
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.8:50000"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	if got := g.Key(r); got != "203.0.113.8" {
		t.Fatal(got)
	}
	r.RemoteAddr = "172.18.0.2:50000"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.8")
	if got := g.Key(r); got != "203.0.113.8" {
		t.Fatal(got)
	}
	r.Header.Set("X-Forwarded-For", "broken")
	if got := g.Key(r); got != "172.18.0.2" {
		t.Fatal(got)
	}
	if _, e := New("not a proxy"); e == nil {
		t.Fatal("invalid proxy accepted")
	}
}
func TestFailureCooldownAndCorrectLoginRecovery(t *testing.T) {
	g, _ := New("")
	now := time.Now()
	g.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		if wait := g.Failure("client"); wait != 0 {
			t.Fatal(wait)
		}
	}
	if wait := g.Failure("client"); wait != 2*time.Second {
		t.Fatal(wait)
	}
	if wait := g.Failure("client"); wait != 2*time.Second {
		t.Fatal("blocked attempt extended cooldown", wait)
	}
	now = now.Add(2 * time.Second)
	if wait := g.Failure("client"); wait != 4*time.Second {
		t.Fatal(wait)
	}
	g.Success("client")
	if wait := g.Failure("client"); wait != 0 {
		t.Fatal("valid login did not clear cooldown", wait)
	}
	for i := 0; i < 5000; i++ {
		g.Failure(fmt.Sprint(i))
	}
	if len(g.entries) > 4096 {
		t.Fatal("unbounded client state")
	}
}
func TestAuthenticatedBurstAndCostLimits(t *testing.T) {
	g, _ := New("")
	now := time.Now()
	g.now = func() time.Time { return now }
	for i := 0; i < 60; i++ {
		if g.Allow("client") != 0 {
			t.Fatal("burst prematurely limited")
		}
	}
	if g.Allow("client") == 0 {
		t.Fatal("unlimited API requests")
	}
	now = now.Add(time.Second)
	if g.Allow("client") != 0 {
		t.Fatal("API credit did not refill")
	}
	done, wait := g.Cost("client", "/api/import", "POST")
	if wait != 0 {
		t.Fatal(wait)
	}
	if _, wait = g.Cost("other", "/api/backup", "POST"); wait == 0 {
		t.Fatal("parallel expensive operations")
	}
	done()
	done()
	if _, wait = g.Cost("client", "/api/accounts/12/refresh", "POST"); wait == 0 {
		t.Fatal("missing import/refresh spacing")
	}
	done, wait = g.Cost("client", "/api/backup", "POST")
	if wait != 0 {
		t.Fatal(wait)
	}
	done()
	done, wait = g.Cost("client", "/api/restore", "POST")
	if wait != 0 {
		t.Fatal("backup blocked immediate restore")
	}
	done()
	now = now.Add(time.Minute)
	done, wait = g.Cost("client", "/api/import", "POST")
	if wait != 0 {
		t.Fatal(wait)
	}
	done()
}
