package media

import (
	"errors"
	"strings"
	"testing"
)

func TestProcessDiagnosticsAreBoundedAndRedacted(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://user:secret@example.test/live/private.ts: Server returned 403 Forbidden", "source rejected access (HTTP 403)"},
		{"https://secret.example/a: Server returned 5XX Server Error", "source server error (HTTP 5xx)"},
		{"Stream map '0:a:0' matches no streams", "source missing required video or audio"},
		{"https://secret.example/: Invalid data found when processing input", "source returned invalid media"},
		{"unrecognised private-password", "source media process stopped"},
	} {
		var d processDiagnostic
		d.Write([]byte(tc.raw))
		if got := d.reason(); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
		if got := probeEndReason(errors.New(d.reason()), false); got != tc.want {
			t.Fatalf("lost safe reason: %s", got)
		}
	}
	var d processDiagnostic
	data := []byte(strings.Repeat("s", 1<<20))
	if n, err := d.Write(data); n != len(data) || err != nil {
		t.Fatal(n, err)
	}
	if len(d.data) != 16<<10 {
		t.Fatalf("unbounded diagnostic: %d", len(d.data))
	}
	d.Write(data)
	if len(d.data) != 16<<10 {
		t.Fatal("grew past limit")
	}
}
