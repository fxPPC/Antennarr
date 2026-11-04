package ingest

import (
	"net/url"
	"regexp"
	"strings"
)

// Raw endpoints are limited to known paste services. Unknown hosts are fetched
// normally, and visible pre/textarea content is extracted without running scripts.
func pastePage(u *url.URL) bool {
	switch strings.ToLower(u.Hostname()) {
	case "paste.sh", "pastebin.com", "www.pastebin.com", "rentry.co", "rentry.org", "dpaste.com", "paste.ee", "gist.github.com":
		return true
	}
	return false
}
func rawPage(u *url.URL) {
	host := strings.ToLower(u.Hostname())
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch host {
	case "pastebin.com", "www.pastebin.com":
		if len(parts) == 1 && parts[0] != "" {
			u.Path = "/raw/" + parts[0]
		}
	case "rentry.co", "rentry.org":
		if len(parts) == 1 && parts[0] != "" {
			u.Path = "/" + parts[0] + "/raw"
		}
	case "dpaste.com":
		if len(parts) == 1 && parts[0] != "" && !strings.HasSuffix(u.Path, ".txt") {
			u.Path = strings.TrimRight(u.Path, "/") + ".txt"
		}
	case "paste.ee":
		if len(parts) == 2 && parts[0] == "p" {
			u.Path = "/r/" + parts[1]
		}
	case "gist.github.com":
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			u.Path += "/raw"
		}
	case "github.com":
		if len(parts) >= 5 && parts[2] == "blob" {
			u.Host = "raw.githubusercontent.com"
			u.Path = "/" + strings.Join(append(parts[:2], parts[3:]...), "/")
		}
	}
	u.RawPath = ""
}

var pasteBlocks = regexp.MustCompile(`(?is)<(textarea|pre)\b[^>]*>(.*?)</(?:textarea|pre)>`)

func pageText(b []byte, contentType string) string {
	body := string(b)
	if strings.Contains(strings.ToLower(contentType), "text/html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "<!doctype html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "<html") {
		blocks := pasteBlocks.FindAllStringSubmatch(body, -1)
		if len(blocks) > 0 {
			var out []string
			for _, block := range blocks {
				out = append(out, textContent(block[2]))
			}
			return strings.Join(out, "\n")
		}
		return textContent(body)
	}
	return body
}
