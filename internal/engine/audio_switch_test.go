package engine

import (
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/media"
	"antennarr/internal/stream"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type growingCatalogue struct {
	fixtureCatalogue
	available atomic.Int32
}

func (c *growingCatalogue) Candidates(_ string, _ bool) ([]catalog.Candidate, error) {
	return c.candidates[:int(c.available.Load())], nil
}

// Plex keeps its original audio-copy configuration throughout a live session.
// An independently decodable TS is insufficient: AAC -> AC-3 breaks that pipeline.
func TestAudioCodecFailoversSurvivePlexCopyPipeline(t *testing.T) {
	t.Run("disconnects", func(t *testing.T) { testAudioSwitches(t, false, false) })
	t.Run("corrupt_audio_with_progressing_timestamps", func(t *testing.T) { testAudioSwitches(t, true, false) })
	t.Run("corrupt_payload_with_valid_audio_headers", func(t *testing.T) { testAudioSwitches(t, true, true) })
}
func testAudioSwitches(t *testing.T, corruptAudio, preserveHeaders bool) {
	dir := os.Getenv("ANTENNARR_TEST_FIXTURES")
	if dir == "" {
		t.Skip("requires generated fixtures")
	}
	binary, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Fatal(e)
	}
	ac3dir := t.TempDir()
	ac3 := filepath.Join(ac3dir, "480.ts")
	cmd := exec.Command(binary, "-nostdin", "-hide_banner", "-v", "error", "-i", filepath.Join(dir, "480.ts"), "-c:v", "copy", "-c:a", "ac3", "-b:a", "128k", "-threads:a", "1", "-ar", "48000", "-ac", "2", "-f", "mpegts", ac3)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate Dolby test audio: %v %s", e, b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	var recorderLog, playerLog bytes.Buffer
	recorder := exec.CommandContext(ctx, binary, "-nostdin", "-hide_banner", "-v", "warning", "-i", "pipe:0", "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-f", "mpegts", "pipe:1")
	player := exec.CommandContext(ctx, binary, "-nostdin", "-hide_banner", "-v", "warning", "-i", "pipe:0", "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-bsf:a", "aac_adtstoasc", "-f", "mp4", "-movflags", "frag_keyframe+empty_moov", "pipe:1")
	linkR, linkW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer linkR.Close()
	defer linkW.Close()
	recorder.Stdout = linkW
	recorder.Stderr = &recorderLog
	player.Stdin = linkR
	player.Stdout = io.Discard
	player.Stderr = &playerLog
	input, e := recorder.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	defer input.Close()
	if e = player.Start(); e != nil {
		t.Fatal(e)
	}
	if e = recorder.Start(); e != nil {
		t.Fatal(e)
	}
	linkR.Close()
	linkW.Close()
	cat := &growingCatalogue{}
	cat.available.Store(2)
	faults := map[int]chan struct{}{}
	for id := 1; id <= 6; id++ {
		faults[id] = make(chan struct{})
		cat.candidates = append(cat.candidates, catalog.Candidate{StreamID: int64(id), AccountID: int64(id), Provider: strconv.Itoa(id), Limit: 1, URL: strconv.Itoa(id)})
	}
	firstReady := make(chan struct{})
	var writeFailed atomic.Bool
	var delivered atomic.Int64
	eng := &Engine{Catalogue: cat, Budget: budget.New(10, 5), Binary: binary, Inspect: func(ctx context.Context, b []byte) (media.Info, error) { return media.Inspect(ctx, binary, b) }}
	eng.Copy = func(parent context.Context, raw string, emit func([]byte)) error {
		id, _ := strconv.Atoi(raw)
		if id == 2 {
			select {
			case <-firstReady:
			case <-parent.Done():
				return parent.Err()
			}
		}
		sourceCtx, stop := context.WithCancel(parent)
		defer stop()
		go func() {
			select {
			case <-faults[id]:
				if !corruptAudio || id != 1 {
					stop()
				}
			case <-sourceCtx.Done():
			}
		}()
		sourceDir, fixture := dir, "720"
		if id%2 == 0 {
			sourceDir, fixture = ac3dir, "480"
		}
		return stream.FixtureRunner(binary, sourceDir)(sourceCtx, fixture, func(b []byte) {
			if corruptAudio && id == 1 {
				select {
				case <-faults[id]:
					b = append([]byte(nil), b...)
					for i := 0; i+188 <= len(b); i += 188 {
						p := b[i : i+188]
						if int(p[1]&31)<<8|int(p[2]) != 257 || p[3]&16 == 0 {
							continue
						}
						off := 4
						if p[3]&32 != 0 {
							off += int(p[4]) + 1
						}
						if p[1]&64 != 0 && off+9 <= 188 {
							off += 9 + int(p[off+8])
							if preserveHeaders && off+7 <= 188 {
								n := 7
								if p[off+1]&1 == 0 {
									n = 9
								}
								off += n
							}
						}
						for j := off; j < 188; j++ {
							p[j] = 0
						}
					}
				default:
				}
			}
			emit(b)
		})
	}
	engineCtx, stopEngine := context.WithCancel(ctx)
	defer stopEngine()
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(engineCtx, "audio-switch", func(b []byte) {
			if _, e := input.Write(b); e != nil {
				writeFailed.Store(true)
			} else {
				delivered.Add(int64(len(b)))
			}
		})
	}()
	cleaned := false
	defer func() {
		if !cleaned {
			cancel()
			<-done
			input.Close()
			recorder.Wait()
			player.Wait()
		}
	}()
	wait := func(primary, ghost int64) {
		t.Helper()
		deadline := time.Now().Add(14 * time.Second)
		for time.Now().Before(deadline) {
			for _, s := range eng.Status() {
				if s.Primary == primary && (ghost == 0 || s.Ghost == ghost) {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("expected primary=%d ghost=%d; status=%+v", primary, ghost, eng.Status())
	}
	wait(1, 0)
	close(firstReady)
	wait(1, 2)
	for id := 1; id <= 4; id++ {
		cat.available.Store(int32(id + 2))
		close(faults[id])
		wait(int64(id+1), int64(id+2))
		for _, status := range eng.Status() {
			if status.OutputRestarts != 0 {
				t.Fatalf("bad source caused output restart loop: %+v", status)
			}
		}
	}
	time.Sleep(2 * time.Second)
	stopEngine()
	<-done
	input.Close()
	recorderErr := recorder.Wait()
	playerErr := player.Wait()
	cleaned = true
	logs := recorderLog.String() + playerLog.String()
	if delivered.Load() < 100000 || writeFailed.Load() || recorderErr != nil || playerErr != nil {
		t.Fatalf("Plex copy pipeline ended: recorder=%v player=%v writeFailed=%v bytes=%d\n%s", recorderErr, playerErr, writeFailed.Load(), delivered.Load(), logs)
	}
	for _, bad := range []string{"not in ADTS", "Error parsing ADTS", "Error applying bitstream", "Error muxing", "Malformed AAC"} {
		if strings.Contains(logs, bad) {
			t.Fatalf("audio handover broke packaging: %s", logs)
		}
	}
	t.Logf("four alternating AAC/Dolby failovers, fixed AAC output, shared continuous recorder/player pipes; %d bytes", delivered.Load())
}
