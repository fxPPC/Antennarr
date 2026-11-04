package ingest

import (
	"encoding/base64"
	"testing"
)

func TestFormatsAndRegionalIdentity(t *testing.T) {
	m3u := "#EXTM3U\n#EXTINF:-1 tvg-id=\"CNN.us\",CNN US\nhttps://TV.EXAMPLE:443/live/user/pass/10.ts?b=2&a=1\n#EXTINF:-1 tvg-id=\"CNN.int\",CNN International\nhttps://tv.example/live/user/pass/11.ts\n"
	rows, e := Parse([]byte(m3u))
	if e != nil || len(rows) != 2 {
		t.Fatal(rows, e)
	}
	if rows[0].Kind != "xtream" || rows[0].Endpoint != "https://tv.example" || rows[0].Username != "user" || rows[0].Password != "pass" {
		t.Fatal(rows[0])
	}
	if rows[0].TVGID == rows[1].TVGID {
		t.Fatal("regional feeds merged")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(m3u))
	again, e := Parse([]byte(encoded))
	if e != nil || len(again) != 2 {
		t.Fatal(e)
	}
	account, e := FromURL("https://tv.example/get.php?password=p%26x&username=u", "", "")
	if e != nil || account.Password != "p&x" || account.URL != "" {
		t.Fatal(account, e)
	}
	for _, input := range []string{`[{"server":"https://tv.example","username":"u","password":"p"}]`, "https://tv.example|u|p"} {
		rows, e = Parse([]byte(input))
		if e != nil || len(rows) != 1 || rows[0].Kind != "xtream" {
			t.Fatal(rows, e)
		}
	}
	for _, bad := range []string{"", "file:///etc/passwd", "javascript:alert(1)", "https://x|u|", `[{"server":"bad"}]`} {
		if _, e = Parse([]byte(bad)); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestCanonicalEquivalence(t *testing.T) {
	a, _ := Canonical("https://EXAMPLE.com:443/path?b=2&a=1#ignored")
	b, _ := Canonical("https://example.com/path?a=1&b=2")
	if a != b {
		t.Fatal(a, b)
	}
}
