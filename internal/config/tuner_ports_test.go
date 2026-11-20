package config

import (
	"math"
	"os"
	"path/filepath"
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
	if e = p.ValidateListeners("127.0.0.1:9192", "192.168.1.10:9192"); e != nil {
		t.Fatal("separate legacy interfaces", e)
	}
	if e = p.ValidateListeners(":9192", "127.0.0.1:9192"); e == nil {
		t.Fatal("wildcard bind conflict accepted")
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

func TestNetworkRangeFreshAndUpgrade(t *testing.T) {
	for _, key := range []string{"ANTENNARR_PORT_RANGE", "ANTENNARR_LISTEN", "ANTENNARR_ADMIN_LISTEN", "ANTENNARR_TUNER_PORTS", "ANTENNARR_COMPAT_LISTEN"} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	fresh, err := ReadListeners(root, false)
	if err != nil || fresh.Primary != ":39193" || fresh.Admin != ":39192" || fresh.Extra != (TunerPorts{39194, 39198}) {
		t.Fatal(fresh, err)
	}
	if _, err = os.Stat(filepath.Join(root, "config")); !os.IsNotExist(err) {
		t.Fatal("read created configuration", err)
	}
	c, err := Load(root)
	if err != nil || c.PortRange != DefaultPortRange {
		t.Fatal(c.PortRange, err)
	}
	desktop, err := ReadListeners(root, true)
	if err != nil || desktop.Admin != "127.0.0.1:39192" {
		t.Fatal(desktop, err)
	}
	if url, err := fresh.BaseURL("http://antennarr", false); err != nil || url != "http://antennarr:39193" {
		t.Fatal(url, err)
	}

	c.PortRange = ""
	if err = Save(root, c); err != nil {
		t.Fatal(err)
	}
	old, err := ReadListeners(root, false)
	if err != nil || old.Range != "" || old.Primary != ":9192" || old.Admin != "" || old.Extra != (TunerPorts{9200, 9207}) {
		t.Fatal(old, err)
	}
	if url, err := old.BaseURL("http://antennarr:9192", false); err != nil || url != "http://antennarr:9192" {
		t.Fatal(url, err)
	}
	if err = os.WriteFile(filepath.Join(root, "config", "port-range"), []byte("41000-41006\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := ReadListeners(root, true)
	if err != nil || installed.Admin != "127.0.0.1:41000" || installed.Extra.Last != 41006 {
		t.Fatal(installed, err)
	}
	t.Setenv("ANTENNARR_PORT_RANGE", "65529-65535")
	over, err := ReadListeners(root, false)
	if err != nil || over.Extra.Last != 65535 {
		t.Fatal(over, err)
	}
	t.Setenv("ANTENNARR_LISTEN", ":45555")
	if _, err = ReadListeners(root, false); err == nil {
		t.Fatal("ambiguous overrides accepted")
	}
	t.Setenv("ANTENNARR_PORT_RANGE", "")
	over, err = ReadListeners(root, false)
	if err != nil || over.Primary != ":45555" || over.Range != "" {
		t.Fatal(over, err)
	}
	legacy, err := Load(t.TempDir())
	if err != nil || legacy.PortRange != "" {
		t.Fatal("fresh explicit legacy override was not retained", legacy.PortRange, err)
	}
}

func TestNetworkRangeInvalidAndConflicting(t *testing.T) {
	for _, key := range []string{"ANTENNARR_PORT_RANGE", "ANTENNARR_LISTEN", "ANTENNARR_ADMIN_LISTEN", "ANTENNARR_TUNER_PORTS", "ANTENNARR_COMPAT_LISTEN"} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	for _, invalid := range []string{"81-87", "3000-3005", "3000-3007", "65530-65536", "-6-0", "4000", "+4000-4006", "03000-3006", "4000-3994", "1-2-3"} {
		t.Setenv("ANTENNARR_PORT_RANGE", invalid)
		if _, err := ReadListeners(root, false); err == nil {
			t.Fatal("accepted", invalid)
		}
	}
	t.Setenv("ANTENNARR_PORT_RANGE", "4000-4006")
	for _, port := range []string{":4000", ":4001", ":4002", ":4006"} {
		t.Setenv("ANTENNARR_COMPAT_LISTEN", port)
		if _, err := ReadListeners(root, false); err == nil {
			t.Fatal("overlapping compatible listener", port)
		}
	}
	t.Setenv("ANTENNARR_COMPAT_LISTEN", ":5004")
	if _, err := ReadListeners(root, false); err != nil {
		t.Fatal(err)
	}
}

func TestListenerFileStaysWithinAppdata(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "config")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadListeners(root, false); err == nil {
		t.Fatal("read configuration outside the appdata root")
	}
}
