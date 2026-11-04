package media

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A standby replay can contain 4 MiB and arrives without real-time pacing.
// Keep headroom for that bounded burst while the pipe writer is scheduled.
const muxInputSlots = 4096

type Mux struct {
	decodeCount  atomic.Int64
	decodeWindow atomic.Int64
	decodeLast   atomic.Int64
	lastOutput   atomic.Int64
	analyzer     Analyzer
	Input        chan []byte
	done         chan struct{}
	cancel       context.CancelFunc
	pipe         io.WriteCloser
	once         sync.Once
	Err          error
}

func Remux(parent context.Context, binary string, emit func([]byte), versions ...int) (*Mux, error) {
	version := 0
	if len(versions) > 0 {
		version = versions[0] % 32
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, binary, "-nostdin", "-hide_banner", "-loglevel", "error", "-filter_threads", "1", "-filter_complex_threads", "1", "-threads", "1", "-probesize", "262144", "-analyzeduration", "500000", "-fflags", "+genpts+discardcorrupt", "-dts_delta_threshold", "0.5", "-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0", "-map", "0:a:0", "-c:v", "copy", "-c:a", "aac", "-profile:a", "aac_low", "-ar", "48000", "-ac", "2", "-b:a", "128k", "-threads:a", "1", "-mpegts_flags", "+resend_headers+initial_discontinuity", "-tables_version", strconv.Itoa(version), "-muxdelay", "0", "-muxpreload", "0", "-f", "mpegts", "pipe:1")
	stdin, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		cancel()
		stdin.Close()
		return nil, e
	}
	m := &Mux{Input: make(chan []byte, muxInputSlots), done: make(chan struct{}), cancel: cancel, pipe: stdin}
	cmd.Stderr = &decoderMonitor{mux: m}
	if e = cmd.Start(); e != nil {
		cancel()
		stdin.Close()
		return nil, errors.New("output remux failed to start")
	}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer stdin.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-m.Input:
				if _, e := stdin.Write(b); e != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer close(m.done)
		buf := make([]byte, 188*7)
		for {
			n, e := io.ReadFull(stdout, buf)
			if n > 0 {
				_ = m.analyzer.Push(buf[:n])
				m.lastOutput.Store(time.Now().UnixNano())
				emit(buf[:n])
			}
			if e != nil {
				break
			}
		}
		m.Err = cmd.Wait()
		cancel()
		stdin.Close()
		<-writerDone
	}()
	return m, nil
}
func (m *Mux) Stopped() bool {
	select {
	case <-m.done:
		return true
	default:
		return false
	}
}
func (m *Mux) LastOutput() time.Time {
	n := m.lastOutput.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}
func (m *Mux) Send(b []byte) bool {
	if m.Stopped() {
		return false
	}
	select {
	case <-m.done:
		return false
	case m.Input <- b:
		return true
	default:
		return false
	}
}
func (m *Mux) Close() { m.once.Do(func() { m.cancel(); m.pipe.Close(); <-m.done }) }

// OutputStats verifies that expected audio still advances after decoding.
func (m *Mux) OutputStats() Stats { return m.analyzer.Snapshot() }

// Keep only error counts, never raw stderr or untrusted media metadata.
type decoderMonitor struct {
	mux     *Mux
	partial []byte
}

func (d *decoderMonitor) Write(b []byte) (int, error) {
	n := len(b)
	for _, v := range b {
		if v == '\n' {
			line := string(d.partial)
			d.partial = d.partial[:0]
			if strings.Contains(line, "Error while decoding stream #0:1") || strings.Contains(line, "Error submitting packet to decoder") {
				now := time.Now()
				if now.Sub(time.Unix(0, d.mux.decodeWindow.Load())) > 2*time.Second {
					d.mux.decodeCount.Store(0)
					d.mux.decodeWindow.Store(now.UnixNano())
				}
				d.mux.decodeCount.Add(1)
				d.mux.decodeLast.Store(now.UnixNano())
			}
		} else if len(d.partial) < 4096 {
			d.partial = append(d.partial, v)
		}
	}
	return n, nil
}
func (m *Mux) AudioDecodeFailed() bool {
	return m.decodeCount.Load() >= 8 && time.Since(time.Unix(0, m.decodeLast.Load())) < 2*time.Second
}
