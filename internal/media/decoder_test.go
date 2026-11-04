package media

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBoundedDecoderDiagnosticsAcrossFFmpegVersions(t *testing.T) {
	for _, line := range []string{"Error while decoding stream #0:1: Invalid data found when processing input\n", "[aist#0:1/aac] Error submitting packet to decoder: Invalid data found when processing input\n"} {
		m := &Mux{}
		w := &decoderMonitor{mux: m}
		// Pipe writes can split an error line anywhere.
		for i := 0; i < 8; i++ {
			w.Write([]byte(line[:13]))
			w.Write([]byte(line[13:]))
		}
		if !m.AudioDecodeFailed() {
			t.Fatal("repeated audio decode failure was hidden")
		}
		m.decodeLast.Store(time.Now().Add(-3 * time.Second).UnixNano())
		if m.AudioDecodeFailed() {
			t.Fatal("old errors remained active")
		}
	}
	m := &Mux{}
	w := &decoderMonitor{mux: m}
	w.Write([]byte(strings.Repeat("untrusted metadata", 10000)))
	if len(w.partial) > 4096 {
		t.Fatal("unbounded stderr")
	}
	w.Write([]byte("\n"))
	if m.AudioDecodeFailed() {
		t.Fatal("metadata mistaken for failed decoding")
	}
	fmt.Fprint(w, "Error while decoding stream #0:1: bad audio\n")
	if m.AudioDecodeFailed() {
		t.Fatal("one error triggered failover")
	}
}
