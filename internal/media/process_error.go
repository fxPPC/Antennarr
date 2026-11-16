package media

import "strings"

// FFmpeg can print credentials from nested provider URLs. Keep a bounded
// diagnostic buffer, and export only fixed categories, never its raw contents.
type processDiagnostic struct{ data []byte }

func (d *processDiagnostic) Write(p []byte) (int, error) {
	const limit = 16 << 10
	n := len(p)
	if available := limit - len(d.data); available > 0 {
		if len(p) > available {
			p = p[:available]
		}
		d.data = append(d.data, p...)
	}
	return n, nil
}

func (d *processDiagnostic) reason() string {
	text := strings.ToLower(string(d.data))
	for _, rule := range []struct{ match, reason string }{
		{"401 unauthorized", "source rejected access (HTTP 401)"},
		{"403 forbidden", "source rejected access (HTTP 403)"},
		{"404 not found", "source stream not found (HTTP 404)"},
		{"429 too many requests", "source rate limited (HTTP 429)"},
		{"server returned 5", "source server error (HTTP 5xx)"},
		{"connection timed out", "source connection timed out"},
		{"matches no streams", "source missing required video or audio"},
		{"invalid data found when processing input", "source returned invalid media"},
	} {
		if strings.Contains(text, rule.match) {
			return rule.reason
		}
	}
	return "source media process stopped"
}
