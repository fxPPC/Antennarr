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

// FailureDetail translates the probe's sanitised reasons into status fields.
// An access rejection from the media URL is scoped to this stream.
func (r ProbeResult) FailureDetail() (kind string, httpStatus int) {
	if r.TimedOut {
		return "timeout", 0
	}
	switch r.Reason {
	case "source rejected access (HTTP 401)":
		return "access_denied", 401
	case "source rejected access (HTTP 403)":
		return "access_denied", 403
	case "source stream not found (HTTP 404)":
		return "stream_not_found", 404
	case "source rate limited (HTTP 429)":
		return "rate_limited", 429
	case "source server error (HTTP 5xx)":
		return "http_error", 0
	case "source connection timed out":
		return "timeout", 0
	case "source ended before sample completed", "source media process stopped":
		return "source_stopped", 0
	case "media process failed to start", "source adapter failed", "source URL blocked":
		return "source_adapter_error", 0
	default:
		return "media_invalid", 0
	}
}
