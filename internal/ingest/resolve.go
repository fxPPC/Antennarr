package ingest

import (
	"antennarr/internal/netguard"
	"context"
	"errors"
	"fmt"
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

// ImportProblem messages are safe to show and log. Raw network errors can contain
// the complete submitted URL, including query credentials.
type ImportProblem string

func (p ImportProblem) Error() string { return string(p) }

// Match a labelled device address, including the arrow used in portal reports.
// A standalone hexadecimal identifier is insufficient evidence of this format.
var portalMAC = regexp.MustCompile(`(?i)\bmac(?:[ \t]+address)?[^\pL\pN\r\n]{1,20}(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b`)

var markdownLink = regexp.MustCompile(`\[[^\]]*\]\((https?://[^\s]+)\)`)

// ResolveLists accepts URLs (including encoded URLs) at the UI boundary. Flexible
// account and playlist parsing is reserved for the fetched list content.
func (r Resolver) ResolveLists(ctx context.Context, input string) (ImportReport, error) {
	if len(input) > 2<<20 {
		return ImportReport{}, ImportProblem("import exceeds 2 MiB")
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
		return ImportReport{}, ImportProblem("enter list URLs or Base64-encoded URLs, one per line")
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
			return ImportProblem("too many source records")
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
			return ImportProblem("nested source link limit reached")
		}
		if len(text) > 2<<20 {
			return ImportProblem("source material exceeds 2 MiB")
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
					return ImportProblem("Invalid list URL.")
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
				if len(report.Warnings) == 0 || report.Warnings[len(report.Warnings)-1] != "Source page limit reached; import further links separately." {
					report.Warnings = append(report.Warnings, "Source page limit reached; import further links separately.")
				}
				continue
			}
			report.Pages++
			u, e := url.Parse(raw)
			if e != nil {
				return ImportProblem("invalid source link")
			}
			fragment := u.Fragment
			u.Fragment = ""
			u.RawFragment = ""
			paste := strings.EqualFold(u.Hostname(), "paste.sh")
			id := strings.Trim(u.Path, "/")
			rawPage(u)
			if paste {
				if strings.Contains(id, "/") || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`).MatchString(id) {
					return ImportProblem("invalid paste.sh link")
				}
				u.Path = "/" + id + ".txt"
			}
			resp, e := netguard.Request(ctx, r.Client, "GET", u.String())
			if e != nil {
				message := "Connection failed or the destination was blocked."
				if errors.Is(e, context.DeadlineExceeded) {
					message = "The request timed out."
				}
				report.Warnings = append(report.Warnings, fmt.Sprintf("Page %d: %s", report.Pages, message))
				continue
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				report.Warnings = append(report.Warnings, fmt.Sprintf("Page %d returned HTTP %d.", report.Pages, resp.StatusCode))
				continue
			}
			b, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
			resp.Body.Close()
			total += len(b)
			if total > 8<<20 {
				return ImportProblem("The batch exceeds the 8 MiB download limit.")
			}
			if readErr != nil || len(b) > 2<<20 {
				message := "The download was interrupted."
				if errors.Is(readErr, context.DeadlineExceeded) {
					message = "The download timed out."
				}
				if len(b) > 2<<20 {
					message = "The page exceeds the 2 MiB limit."
				}
				report.Warnings = append(report.Warnings, fmt.Sprintf("Page %d: %s", report.Pages, message))
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
			// Some reports parse as bare portal URLs, which yield no importable
			// entries. Diagnose these after giving supported records a chance.
			page, before, warnings := report.Pages, len(report.Entries)+report.Duplicates, len(report.Warnings)
			walkErr := walk(body, depth+1)
			if walkErr != nil && total > 8<<20 {
				return walkErr
			}
			if before == len(report.Entries)+report.Duplicates && warnings == len(report.Warnings) && portalMAC.MatchString(body) {
				report.Warnings = append(report.Warnings, fmt.Sprintf("Page %d contains MAC-based portal entries. Antennarr accepts Xtream accounts and M3U playlists.", page))
			} else if walkErr != nil {
				report.Warnings = append(report.Warnings, "A source page contained no complete accounts or used an unsupported format.")
			}
		}
		return nil
	}
	if len(input) > 2<<20 {
		return report, ImportProblem("import exceeds 2 MiB")
	}
	if err := walk(DecodeLines(input), 0); err != nil {
		return report, err
	}
	if len(report.Entries) == 0 {
		if len(report.Warnings) > 0 {
			return report, ImportProblem("No entries imported. " + strings.Join(report.Warnings, " "))
		}
		return report, ImportProblem("no complete sources found; check that pages are available and paste links include their full fragment key")
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
