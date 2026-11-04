package ingest

import (
	"antennarr/internal/netguard"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Resolver retrieves only explicitly submitted material, never scheduled discovery.
// Fragment keys stay local; they are never sent to paste.sh or redirect targets.
type Resolver struct{ Client *http.Client }
type ImportReport struct {
	Entries    []Entry  `json:"-"`
	Pages      int      `json:"pages"`
	Duplicates int      `json:"duplicates"`
	Warnings   []string `json:"warnings"`
}

var markdownLink = regexp.MustCompile(`\[[^\]]*\]\((https?://[^\s]+)\)`)

// ResolveLists accepts URLs (including encoded URLs) at the UI boundary. Flexible
// account and playlist parsing is reserved for the fetched list content.
func (r Resolver) ResolveLists(ctx context.Context, input string) (ImportReport, error) {
	if len(input) > 2<<20 {
		return ImportReport{}, errors.New("import exceeds 2 MiB")
	}
	decoded := DecodeLines(input)
	invalid := strings.Contains(decoded, "#EXTM3U") || len(urlToken.FindAllString(decoded, -1)) == 0
	for _, line := range strings.Split(decoded, "\n") {
		line = strings.TrimSpace(line)
		if labels.MatchString(line) && !strings.HasPrefix(line, "https://") && !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "[") {
			invalid = true
		}
	}
	if invalid {
		return ImportReport{}, errors.New("enter list URLs or Base64-encoded URLs, one per line")
	}
	return r.resolve(ctx, input)
}
func (r Resolver) resolve(ctx context.Context, input string) (ImportReport, error) {
	report := ImportReport{Warnings: []string{}}
	seenPages := map[string]bool{}
	seenEntries := map[string]bool{}
	total := 0
	var walk func(string, int) error
	add := func(e Entry) error {
		if len(report.Entries) >= 20000 {
			return errors.New("too many source records")
		}
		key := e.Kind + "\x00" + e.Endpoint + "\x00" + e.Username + "\x00" + e.Password + "\x00" + e.URL
		if seenEntries[key] {
			report.Duplicates++
			return nil
		}
		seenEntries[key] = true
		report.Entries = append(report.Entries, e)
		return nil
	}
	walk = func(text string, depth int) error {
		if depth > 5 {
			return errors.New("nested source link limit reached")
		}
		if len(text) > 2<<20 {
			return errors.New("source material exceeds 2 MiB")
		}
		text = DecodeText(strings.TrimSpace(text))
		text = markdownLink.ReplaceAllString(text, "$1")
		rows, err := Parse([]byte(text))
		if depth == 0 {
			rows = nil
			err = nil
			for _, raw := range urlToken.FindAllString(DecodeLines(text), -1) {
				raw = strings.TrimRight(raw, ")],;")
				canonical, e := Canonical(raw)
				if e != nil {
					return e
				}
				rows = append(rows, Entry{Kind: "direct", URL: canonical, Name: "Unnamed stream"})
			}
		}
		if err != nil {
			return err
		}
		for _, entry := range rows {
			if depth > 0 && (entry.Kind == "xtream" || isMedia(entry.URL) || entry.Name != "Unnamed stream") {
				if e := add(entry); e != nil {
					return e
				}
				continue
			}
			// Recover the fragment from the original supplied text, before Canonical strips it.
			raw := entry.URL
			for _, token := range urlToken.FindAllString(DecodeLines(text), -1) {
				token = strings.TrimRight(token, ")],;")
				base, _ := Canonical(token)
				if base == entry.URL {
					raw = token
					break
				}
			}
			candidateURL, _ := url.Parse(raw)
			if depth > 0 && candidateURL != nil && !pastePage(candidateURL) {
				ext := strings.ToLower(path.Ext(candidateURL.Path))
				if ext != ".txt" && ext != ".m3u" && ext != ".json" {
					continue
				}
			}
			if seenPages[raw] {
				continue
			}
			seenPages[raw] = true
			if report.Pages >= 12 {
				report.Warnings = append(report.Warnings, "Source page limit reached; import further links separately.")
				continue
			}
			report.Pages++
			u, e := url.Parse(raw)
			if e != nil {
				return errors.New("invalid source link")
			}
			fragment := u.Fragment
			u.Fragment = ""
			u.RawFragment = ""
			paste := strings.EqualFold(u.Hostname(), "paste.sh")
			id := strings.Trim(u.Path, "/")
			rawPage(u)
			if paste {
				if strings.Contains(id, "/") || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`).MatchString(id) {
					return errors.New("invalid paste.sh link")
				}
				u.Path = "/" + id + ".txt"
			}
			resp, e := netguard.Request(ctx, r.Client, "GET", u.String())
			if e != nil {
				report.Warnings = append(report.Warnings, "A source page could not be fetched or its destination was blocked.")
				continue
			}
			b, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
			resp.Body.Close()
			total += len(b)
			if readErr != nil || len(b) > 2<<20 || total > 8<<20 {
				return errors.New("source page response exceeds import limit")
			}
			if resp.StatusCode != 200 {
				report.Warnings = append(report.Warnings, "A source page returned an HTTP error.")
				continue
			}
			if paste {
				b, e = decryptPaste(id, fragment, resp.Header.Get("Content-Type"), resp.Header.Get("ETag"), b)
				if e != nil {
					report.Warnings = append(report.Warnings, "A paste could not be decrypted: missing/wrong key, damaged data, or unsupported format.")
					continue
				}
			}
			body := pageText(b, resp.Header.Get("Content-Type"))
			if e := walk(body, depth+1); e != nil {
				report.Warnings = append(report.Warnings, "A source page contained no complete accounts or used an unsupported format.")
			}
		}
		return nil
	}
	if len(input) > 2<<20 {
		return report, errors.New("import exceeds 2 MiB")
	}
	if err := walk(DecodeLines(input), 0); err != nil {
		return report, err
	}
	if len(report.Entries) == 0 {
		return report, errors.New("no complete sources found; check that pages are available and paste links include their full fragment key")
	}
	return report, nil
}
func DecodeLines(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, v := range lines {
		lines[i] = DecodeText(v)
	}
	return strings.Join(lines, "\n")
}
func isMedia(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	return ext == ".ts" || ext == ".m3u8" || strings.Contains(u.Path, "/live/") || u.Query().Get("type") == "m3u_plus"
}
