package config

import (
	"math"
	"testing"
)

func TestTunerPortAllocation(t *testing.T) {
	p, e := ParseTunerPorts("")
	if e != nil {
		t.Fatal(e)
	}
	for offset, want := range map[int]string{0: "http://antennarr:9192", 1: "http://antennarr:9200", 8: "http://antennarr:9207"} {
		got, e := p.URL("http://antennarr:9192", offset)
		if e != nil || got != want {
			t.Fatal(got, e)
		}
	}
	if _, e = p.URL("http://[fd00::1]:9192", 1); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Port(math.MaxInt); e == nil {
		t.Fatal("overflowing slot accepted")
	}
	if _, e = p.Port(9); e == nil {
		t.Fatal("accepted exhausted range")
	}
	if e = p.ValidateListeners(":9192", ":9193", ":5004"); e != nil {
		t.Fatal(e)
	}
	if e = p.ValidateListeners(":9201"); e == nil {
		t.Fatal("listener collision accepted")
	}
	for _, v := range []string{"80-90", "9200-9199", "9200-9300", "65535-65536", "bad", "1-2-3"} {
		if _, e := ParseTunerPorts(v); e == nil {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"ftp://host:9192", "http://user:pass@host:9192", "http://host/tuner", "http://host?token=secret"} {
		if _, e := p.URL(v, 1); e == nil {
			t.Fatal("invalid base accepted")
		}
	}
}
