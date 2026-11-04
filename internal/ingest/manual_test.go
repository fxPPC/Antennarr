package ingest

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFlexibleAccounts(t *testing.T) {
	for _, text := range []string{
		"Portal    User : Pass    Country / expiry\nhttps://one.example:8080   alice : p&x    US 2027\nhttps://two.example    bob : p2    UK 2027",
		"Some heading\n🌍 URL: https://one.example:8080\n👤 Username: alice\n🔑 Password: p&x\n-----\nHost = https://two.example\nLogin = bob\nPass = p2",
		"server: https://one.example:8080 username: alice password: p&x\nserver: https://two.example user: bob pwd: p2",
		`<div>Server: https://one.example:8080</div><div>Username: alice</div><div>Password: p&amp;x</div><p>Host: https://two.example</p><p>User: bob</p><p>Pass: p2</p>`,
		`[{"url":"https://one.example:8080","user":"alice","pass":"p&x"},{"host":"https://two.example","login":"bob","pwd":"p2"}]`,
		`{"accounts":[{"host":"https://one.example:8080","login":"alice","pass":"p&x"},{"portal":"https://two.example","user":"bob","pwd":"p2"}]}`,
		"svg\nhttps://one.example:8080/get.php?username=alice&password=p%26x\nhttps://two.example/get.php?username=bob&password=p2",
		"Accounts\nhttps://one.example:8080\nUsername: alice\nPassword: p&x\nhttps://two.example\nUser: bob\nPass: p2",
	} {
		rows, e := Parse([]byte(text))
		if e != nil || len(rows) != 2 {
			t.Fatalf("format not parsed: entries=%d error=%v", len(rows), e)
		}
		if rows[0].Username != "alice" || rows[0].Password != "p&x" || rows[1].Username != "bob" {
			t.Fatal("credential field extraction failed")
		}
	}
}
func pasteFixture(t *testing.T, id, key, plain, version string) (string, string) {
	t.Helper()
	password := id + key + "https://paste.sh"
	salt := []byte("12345678")
	derived, _ := pbkdf2.Key(sha512.New, password, salt, 1, 48)
	block, _ := aes.NewCipher(derived[:32])
	b := []byte(plain)
	pad := 16 - len(b)%16
	for i := 0; i < pad; i++ {
		b = append(b, byte(pad))
	}
	ct := make([]byte, len(b))
	cipher.NewCBCEncrypter(block, derived[32:]).CryptBlocks(ct, b)
	data := append(append([]byte("Salted__"), salt...), ct...)
	inner := hmac.New(sha512.New, []byte("auth key"))
	inner.Write([]byte(password))
	mac := hmac.New(sha512.New, inner.Sum(nil))
	mac.Write(data)
	return "\n" + base64.StdEncoding.EncodeToString(data) + "\n", `"` + base64.RawStdEncoding.EncodeToString(mac.Sum(nil)) + `"`
}
func TestEncodedPasteResolutionAndNoFragmentSent(t *testing.T) {
	plain := "Server: https://provider.example\nUsername: test-user\nPassword: test-password"
	body, etag := pasteFixture(t, "fixture", "link-key", plain, "v2")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Fragment != "" || r.URL.RawFragment != "" || r.URL.Path != "/fixture.txt" {
			t.Error("paste fragment leaked or wrong raw endpoint")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/vnd.paste.sh-v2"}, "Etag": []string{etag}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	link := "https://paste.sh/fixture#link-key"
	encoded := base64.StdEncoding.EncodeToString([]byte(link))
	report, e := (Resolver{Client: client}).ResolveLists(context.Background(), encoded+"\nsvg\n["+link+"]("+link+")\n"+encoded)
	if e != nil || len(report.Entries) != 1 || calls != 1 || report.Pages != 1 {
		t.Fatalf("resolve entries=%d calls=%d pages=%d error=%v", len(report.Entries), calls, report.Pages, e)
	}
	if report.Entries[0].Password != "test-password" {
		t.Fatal("paste body was not parsed")
	}
	if _, e := decryptPaste("fixture", "wrong-key", "text/vnd.paste.sh-v2", etag, []byte(body)); e == nil {
		t.Fatal("accepted wrong paste key")
	}
	body, etag = pasteFixture(t, "fixture", "link-key", "Subject: fixture\nContent-Type: text/plain\n\n"+plain, "v3")
	decoded, e := decryptPaste("fixture", "link-key", "text/vnd.paste.sh-v3", etag, []byte(body))
	if e != nil || string(decoded) != plain {
		t.Fatal("v3 headers not handled")
	}
}
func TestResolverBlockedAndBounded(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("should not reach private address")
	})}
	_, e := (Resolver{Client: client}).ResolveLists(context.Background(), "https://169.254.169.254/metadata")
	if e == nil || calls != 0 {
		t.Fatal("metadata destination accepted")
	}
	_, e = (Resolver{Client: client}).ResolveLists(context.Background(), strings.Repeat("x", (2<<20)+1))
	if e == nil {
		t.Fatal("oversized input accepted")
	}
}

func TestListPagesAcrossServices(t *testing.T) {
	cases := []struct{ link, path, contentType, body string }{
		{"https://pastebin.com/abc123", "/raw/abc123", "text/plain", "Host: https://provider.example\nUser: demo\nPass: sample"},
		{"https://rentry.co/list", "/list/raw", "text/plain", "Host: https://provider.example\nUser: demo\nPass: sample"},
		{"https://dpaste.com/ABCDE", "/ABCDE.txt", "text/plain", "Host: https://provider.example\nUser: demo\nPass: sample"},
		{"https://paste.ee/p/abc", "/r/abc", "text/plain", "Host: https://provider.example\nUser: demo\nPass: sample"},
		{"https://gist.github.com/user/abc", "/user/abc/raw", "text/plain", "Host: https://provider.example\nUser: demo\nPass: sample"},
		{"https://other-paste.example/list", "/list", "text/html", "<html><nav>https://irrelevant.example</nav><textarea>Host: https://provider.example\nUser: demo\nPass: sam&amp;ple</textarea></html>"},
		{"https://other-paste.example/list", "/list", "text/html", "<pre>Host: https://provider.example<br>User: demo<br>Pass: sample</pre>"},
	}
	for _, c := range cases {
		t.Run(c.link+c.body[:4], func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != c.path {
					t.Errorf("raw path: %s want %s", r.URL.Path, c.path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{c.contentType}}, Body: io.NopCloser(strings.NewReader(c.body))}, nil
			})}
			report, e := (Resolver{Client: client}).ResolveLists(context.Background(), base64.StdEncoding.EncodeToString([]byte(c.link)))
			if e != nil || len(report.Entries) != 1 || report.Pages != 1 {
				t.Fatal(report, e)
			}
			if report.Entries[0].Username != "demo" {
				t.Fatal("did not parse account")
			}
		})
	}
	for _, bad := range []string{"Host: https://provider.example\nUser: demo\nPass: sample", "#EXTM3U\nhttps://provider.example/1.ts", "not a URL"} {
		if _, e := (Resolver{}).ResolveLists(context.Background(), bad); e == nil {
			t.Fatal("accepted non-link input")
		}
	}
}

func TestPlainTextPasswordWithAngleBrackets(t *testing.T) {
	entries, e := Parse([]byte("Host: https://provider.example\nUser: demo\nPass: a<word>b"))
	if e != nil || len(entries) != 1 || entries[0].Password != "a<word>b" {
		t.Fatal("plain-text password altered", e)
	}
}
