package media

import (
	"antennarr/internal/checkcpu"
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

type Stats struct {
	LastPacket, LastVideo, LastAudio time.Time
	VideoChanges, AudioChanges       int
	Bytes                            int64
	InvalidAudio                     int
}
type Analyzer struct {
	audioCodec         string
	mu                 sync.Mutex
	s                  Stats
	videoPTS, audioPTS uint64
	haveV, haveA       bool
}

func pts(b []byte) uint64 {
	return (uint64(b[0]&14) << 29) | (uint64(b[1]) << 22) | (uint64(b[2]&254) << 14) | (uint64(b[3]) << 7) | uint64(b[4]>>1)
}
func (a *Analyzer) Push(b []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for i := 0; i+188 <= len(b); i += 188 {
		p := b[i : i+188]
		if p[0] != 0x47 || p[1]&0x80 != 0 {
			return errors.New("invalid transport packet")
		}
		a.s.Bytes += 188
		a.s.LastPacket = now
		pid := int(p[1]&31)<<8 | int(p[2])
		if p[1]&0x40 == 0 || p[3]&0x10 == 0 {
			continue
		}
		offset := 4
		if p[3]&0x20 != 0 {
			offset += int(p[4]) + 1
		}
		if offset+14 > 188 {
			continue
		}
		pes := p[offset:]
		if pes[0] != 0 || pes[1] != 0 || pes[2] != 1 || pes[7]&0x80 == 0 {
			continue
		}
		stamp := pts(pes[9:14])
		if pid == 256 && (!a.haveV || stamp != a.videoPTS) {
			a.videoPTS = stamp
			a.haveV = true
			a.s.VideoChanges++
			a.s.LastVideo = now
		}
		if pid == 257 && a.audioCodec != "" {
			start := 9 + int(pes[8])
			if start+2 <= len(pes) {
				frame := pes[start:]
				valid := true
				switch a.audioCodec {
				case "aac":
					valid = frame[0] == 0xff && frame[1]&0xf6 == 0xf0
				case "aac_latm":
					valid = frame[0] == 0x56 && frame[1]&0xe0 == 0xe0
				case "ac3", "eac3":
					valid = frame[0] == 0x0b && frame[1] == 0x77
				}
				if valid {
					a.s.InvalidAudio = 0
				} else {
					a.s.InvalidAudio++
				}
			}
		}
		if pid == 257 && (!a.haveA || stamp != a.audioPTS) {
			a.audioPTS = stamp
			a.haveA = true
			a.s.AudioChanges++
			a.s.LastAudio = now
		}
	}
	return nil
}

// The source adapter emits complete audio frames at PES starts. Check their
// framing as well as timestamps, which can advance even on corrupt media.
func (a *Analyzer) SetAudioCodec(codec string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.audioCodec = codec
	a.s.InvalidAudio = 0
}
func (a *Analyzer) Snapshot() Stats { a.mu.Lock(); defer a.mu.Unlock(); return a.s }
func (g *Gateway) Copy(ctx context.Context, binary, raw string, emit func([]byte)) error {
	local, release, e := g.Add(ctx, raw)
	if e != nil {
		return e
	}
	defer release()
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "http,tcp,crypto", "-rw_timeout", "10000000", "-threads", "1", "-re", "-probesize", "262144", "-analyzeduration", "500000", "-fflags", "+genpts+discardcorrupt", "-i", local, "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-streamid", "0:256", "-streamid", "1:257", "-mpegts_flags", "+resend_headers+initial_discontinuity", "-f", "mpegts", "pipe:1"}
	cmd := checkcpu.Command(ctx, binary, args...)
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return errors.New("media process failed to start")
	}
	sourceProcessStarted(ctx, cmd.Process.Pid)
	buf := make([]byte, 188*7)
	for {
		n, e := io.ReadFull(pipe, buf)
		if n > 0 {
			if n%188 != 0 {
				cmd.Process.Kill()
				cmd.Wait()
				return errors.New("partial transport packet")
			}
			emit(buf[:n])
		}
		if e != nil {
			break
		}
	}
	if e = cmd.Wait(); e != nil {
		return errors.New("source media process stopped")
	}
	return nil
}

type ProbeResult struct {
	TimedOut      bool
	Codec         string
	Width, Height int
	Success       bool
	StartupMS     int64
	DurationMS    int64
	Reason        string
}

func (g *Gateway) Probe(ctx context.Context, binary, raw string, duration time.Duration) (result ProbeResult) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second+duration)
	defer cancel()
	defer func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
			result.Success = false
			result.Reason = "media check timed out"
		}
	}()
	var a Analyzer
	var sampleMu sync.Mutex
	sample := make([]byte, 0, 512<<10)
	first := make(chan time.Time, 1)
	done := make(chan error, 1)
	go func() {
		done <- g.Copy(ctx, binary, raw, func(b []byte) {
			sampleMu.Lock()
			if len(sample) < 4<<20 {
				sample = append(sample, b...)
			}
			sampleMu.Unlock()
			if a.Push(b) != nil {
				cancel()
				return
			}
			select {
			case first <- time.Now():
			default:
			}
		})
	}()
	result = ProbeResult{Reason: "no progressing media"}
	select {
	case at := <-first:
		result.StartupMS = at.Sub(start).Milliseconds()
	case <-done:
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	case <-ctx.Done():
		result.DurationMS = time.Since(start).Milliseconds()
		<-done
		return result
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-done:
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	case <-ctx.Done():
	}
	stats := a.Snapshot()
	result.Success = stats.VideoChanges >= 3 && stats.AudioChanges >= 3 && time.Since(stats.LastVideo) < 2*time.Second && time.Since(stats.LastAudio) < 2*time.Second
	if result.Success {
		sampleMu.Lock()
		snapshot := append([]byte(nil), sample...)
		sampleMu.Unlock()
		info, e := Inspect(ctx, binary, snapshot)
		if e != nil {
			result.Success = false
			result.Reason = "media sample failed decoding"
			if errors.Is(e, context.DeadlineExceeded) {
				result.TimedOut = true
				result.Reason = "media inspection timed out"
			}
		} else {
			result.Codec = info.Codec
			result.Width = info.Width
			result.Height = info.Height
			result.Reason = "progressing and decodable video and audio"
		}
	}
	cancel()
	<-done
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}
