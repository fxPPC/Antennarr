package ingest

import (
	"encoding/base64"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var urlToken = regexp.MustCompile(`(?i)https?://[^\s<>"\x60]+`)
var tags = regexp.MustCompile(`(?s)<[^>]*>`)
var hiddenHTML = regexp.MustCompile(`(?is)<(?:script|style|svg)\b[^>]*>.*?</(?:script|style|svg)>`)
var tableAccount = regexp.MustCompile(`^\s*(https?://\S+)\s+(\S+)\s+:\s+(\S+)(?:\s|$)`)
var labels = regexp.MustCompile(`(?i)\b(server(?:\s*url)?|host|portal|url|user\s*name|user|login|account|password|passwd|pass|pwd)\s*[:=]\s*`)

// DecodeText unwraps conventional text encodings only; it never executes input.
func DecodeText(s string) string {
	for i := 0; i < 4; i++ {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToLower(s), "http%3a") || strings.HasPrefix(strings.ToLower(s), "https%3a") {
			if v, e := url.QueryUnescape(s); e == nil {
				s = v
				continue
			}
		}
		compact := strings.Join(strings.Fields(s), "")
		decoded := false
		if len(compact) >= 16 && !strings.ContainsAny(compact, ":<>#") {
			for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
				if b, e := enc.DecodeString(compact); e == nil && utf8.Valid(b) && (strings.Contains(string(b), "http") || strings.Contains(string(b), "#EXTM3U") || strings.Contains(string(b), "username")) {
					s = string(b)
					decoded = true
					break
				}
			}
		}
		if !decoded {
			break
		}
	}
	return s
}

func textContent(s string) string {
	s = hiddenHTML.ReplaceAllString(s, "")
	// Preserve line boundaries and useful anchor destinations in exported pages.
	s = regexp.MustCompile(`(?i)<(?:br\s*/?|/?(?:p|div|li|tr|pre|h[1-6]))\b[^>]*>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?i)<a\b[^>]*href=["'](https?://[^"']+)["'][^>]*>`).ReplaceAllString(s, "\n$1\n")
	return html.UnescapeString(tags.ReplaceAllString(s, " "))
}

// flexible parses labelled account blocks and ignores surrounding commentary.
// A bare server is never treated as an account without both credential fields.
func flexible(s string) ([]Entry, error) {
	s = DecodeText(s)
	if strings.HasPrefix(strings.TrimSpace(s), "<") {
		s = textContent(s)
	}
	var obj any
	if json.Unmarshal([]byte(s), &obj) == nil {
		var out []Entry
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case []any:
				for _, v := range x {
					walk(v)
				}
			case map[string]any:
				fields := map[string]string{}
				for k, v := range x {
					if st, ok := v.(string); ok {
						fields[strings.ToLower(k)] = st
					}
				}
				first := func(keys ...string) string {
					for _, k := range keys {
						if fields[k] != "" {
							return fields[k]
						}
					}
					return ""
				}
				server := first("server", "host", "portal", "endpoint", "url")
				user := first("username", "user", "login", "account")
				pass := first("password", "pass", "passwd", "pwd")
				if server != "" && user != "" && pass != "" {
					if e, err := accountEntry(server, user, pass); err == nil {
						out = append(out, e)
					}
				} else {
					for _, v := range x {
						switch v.(type) {
						case []any, map[string]any:
							walk(v)
						}
					}
				}
			}
		}
		walk(obj)
		if len(out) > 0 {
			return out, nil
		}
	}
	var out []Entry
	server, user, pass := "", "", ""
	flush := func() {
		if e, err := accountEntry(server, user, pass); err == nil {
			out = append(out, e)
		}
		server, user, pass = "", "", ""
	}
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := DecodeText(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if line != strings.TrimSpace(raw) && strings.Contains(line, "\n") {
			flush()
			if rows, e := Parse([]byte(line)); e == nil {
				out = append(out, rows...)
			}
			continue
		}

		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if !strings.ContainsAny(line, " \t") {
				if e, err := FromURL(line, "", ""); err == nil {
					flush()
					if e.Kind == "direct" && strings.TrimRight(e.URL, "/") == strings.TrimRight(e.Endpoint, "/") {
						server = e.URL
					} else {
						out = append(out, e)
					}
					continue
				}
			}
		}

		if m := tableAccount.FindStringSubmatch(line); m != nil {
			if entry, e := accountEntry(m[1], m[2], m[3]); e == nil {
				flush()
				out = append(out, entry)
				continue
			}
		}
		completeURL := false
		for _, token := range urlToken.FindAllString(line, -1) {
			token = strings.TrimRight(token, ")],;")
			if e, err := FromURL(token, "", ""); err == nil && e.Kind == "xtream" && e.Username != "" && e.Password != "" {
				flush()
				out = append(out, e)
				completeURL = true
			}
		}
		if completeURL {
			continue
		}
		ms := labels.FindAllStringSubmatchIndex(line, -1)
		if len(ms) > 0 {
			for i, m := range ms {
				end := len(line)
				if i+1 < len(ms) {
					end = ms[i+1][0]
				}
				key := strings.ToLower(strings.ReplaceAll(line[m[2]:m[3]], " ", ""))
				value := strings.Trim(strings.TrimSpace(line[m[1]:end]), " \t│┃")
				if len(value) > 1 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == 39 && value[len(value)-1] == 39)) {
					value = value[1 : len(value)-1]
				}
				switch key {
				case "server", "serverurl", "host", "portal", "url":
					if server != "" {
						flush()
					}
					if u := urlToken.FindString(value); u != "" {
						server = strings.TrimRight(u, ")],;")
					}
				case "username", "user", "login", "account":
					if user != "" && pass != "" {
						old := server
						flush()
						server = old
					}
					user = value
				case "password", "passwd", "pass", "pwd":
					pass = value
				}
			}
			continue
		}
		if rows, e := parse([]byte(line), 0); e == nil {
			flush()
			out = append(out, rows...)
			continue
		}
		for _, rawURL := range urlToken.FindAllString(line, -1) {
			if e, err := FromURL(strings.TrimRight(rawURL, ")],;"), "", ""); err == nil {
				flush()
				out = append(out, e)
			}
		}
	}
	flush()
	if len(out) == 0 {
		return nil, errNoRecords
	}
	return out, nil
}

func accountEntry(server, user, pass string) (Entry, error) {
	if server == "" || user == "" || pass == "" {
		return Entry{}, errNoRecords
	}
	normalized, e := Canonical(server)
	if e != nil {
		return Entry{}, errNoRecords
	}
	u, _ := url.Parse(normalized)
	if u.User != nil || u.RawQuery != "" {
		return Entry{}, errNoRecords
	}
	u.Fragment = ""
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/player_api.php"), "/get.php")
	return Entry{Kind: "xtream", Endpoint: strings.TrimRight(u.String(), "/"), Username: user, Password: pass}, nil
}
