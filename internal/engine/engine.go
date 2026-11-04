// Package engine owns one primary/ghost pair per active channel. It neither
// synchronises programmes nor returns to a recovered former primary by preference.
package engine

import (
	"antennarr/internal/activity"
	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/media"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Catalogue interface {
	Candidates(string, bool) ([]catalog.Candidate, error)
	Record(int64, catalog.Result) error
	Event(string, string, int64, string)
}
type CopyFunc func(context.Context, string, func([]byte)) error
type InspectFunc func(context.Context, []byte) (media.Info, error)
type Output interface {
	Send([]byte) bool
	Close()
	Stopped() bool
	LastOutput() time.Time
}
type Engine struct {
	metrics      sync.WaitGroup
	Activity     *activity.Tracker
	NewOutput    func(context.Context, func([]byte), int) (Output, error)
	RaceSize     int
	Buffer       time.Duration
	StallTimeout time.Duration
	Catalogue    Catalogue
	Budget       *budget.Budget
	Copy         CopyFunc
	Inspect      InspectFunc
	Binary       string
	Preempt      func()
	mu           sync.Mutex
	live         map[string]Status
}
type Status struct {
	PrimaryProcess   int    `json:"primary_process,omitempty"`
	GhostProcess     int    `json:"ghost_process,omitempty"`
	AudioCodec       string `json:"audio_codec"`
	SourceAudioCodec string `json:"source_audio_codec"`
	GhostAudioCodec  string `json:"ghost_audio_codec"`
	Recovery         string `json:"recovery"`
	LastIssue        string `json:"last_issue"`
	OutputRestarts   int    `json:"output_restarts"`
	RetrySeconds     int    `json:"retry_seconds"`
	PendingMetrics   int    `json:"pending_metrics"`
	DroppedMetrics   int    `json:"dropped_metrics"`
	Channel          string `json:"channel_id"`
	Primary          int64  `json:"primary"`
	Ghost            int64  `json:"ghost"`
	Candidates       int    `json:"candidate_connections"`
	Failovers        int    `json:"failovers"`
	PrimaryBytes     int64  `json:"primary_bytes"`
	Codec            string `json:"codec"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	Standby          string `json:"standby"`
}

func (e *Engine) Status() []Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Status{}
	for _, v := range e.live {
		out = append(out, v)
	}
	return out
}
func (e *Engine) publish(v Status) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.live == nil {
		e.live = map[string]Status{}
	}
	e.live[v.Channel] = v
}

type chunk struct {
	at   time.Time
	data []byte
}
type feed struct {
	process        atomic.Int64
	bufferDuration time.Duration
	stallTimeout   time.Duration
	candidate      catalog.Candidate
	cancel         context.CancelFunc
	started        time.Time
	analyzer       media.Analyzer
	info           media.Info
	ready          bool
	ring           []chunk
	ringBytes      int
	stalls         []time.Time
	lastPacket     time.Time
	lastRecorded   time.Time
}
type event struct {
	f     *feed
	data  []byte
	ready bool
	info  media.Info
	err   error
}

func (f *feed) buffer(b []byte) {
	now := time.Now()
	f.ring = append(f.ring, chunk{now, b})
	f.ringBytes += len(b)
	for len(f.ring) > 1 && (now.Sub(f.ring[0].at) > f.bufferDuration || f.ringBytes > 4<<20) {
		f.ringBytes -= len(f.ring[0].data)
		f.ring[0] = chunk{}
		f.ring = f.ring[1:]
	}
}
func (f *feed) replay() [][]byte {
	// FFmpeg normalises video PID 256 and PMT PID 4096. Start on a signalled
	// random-access point and repeat tables so the next decoder can initialise.
	var pat, pmt []byte
	startChunk, startOffset := -1, 0
	for n, ch := range f.ring {
		for i := 0; i+188 <= len(ch.data); i += 188 {
			p := ch.data[i : i+188]
			pid := int(p[1]&31)<<8 | int(p[2])
			if pid == 0 {
				pat = append([]byte(nil), p...)
			}
			if pid == 4096 {
				pmt = append([]byte(nil), p...)
			}
			if startChunk < 0 && pid == 256 && p[1]&0x40 != 0 && p[3]&0x20 != 0 && p[4] > 0 && p[5]&0x40 != 0 {
				startChunk = n
				startOffset = i
			}
		}
	}
	if startChunk < 0 {
		return nil
	}
	out := [][]byte{}
	if pat != nil {
		out = append(out, pat)
	}
	if pmt != nil {
		out = append(out, pmt)
	}
	out = append(out, f.ring[startChunk].data[startOffset:])
	for _, ch := range f.ring[startChunk+1:] {
		out = append(out, ch.data)
	}
	return out
}
func (f *feed) unhealthy(now time.Time) bool {
	if !f.ready {
		return now.Sub(f.started) > 15*time.Second
	}
	s := f.analyzer.Snapshot()
	return now.Sub(s.LastPacket) > f.stallTimeout || now.Sub(s.LastVideo) > f.stallTimeout+300*time.Millisecond || now.Sub(s.LastAudio) > f.stallTimeout+time.Second || len(f.stalls) >= 3 || s.InvalidAudio >= 3
}

// Run owns one channel controller. Feed selection and switching are serialised
// in its event loop; source goroutines communicate through a bounded queue.
// History and operational-event writes run separately so storage cannot stall
// live delivery. Cancellation closes the shared output and every upstream.
func (e *Engine) Run(parent context.Context, channel string, emit func([]byte)) error {
	if e.Preempt != nil {
		e.Preempt()
	}
	if prepare, ok := e.Catalogue.(interface{ Prepare(context.Context, string) }); ok {
		prepare.Prepare(parent, channel)
	}
	candidates := e.Catalogue.Candidates

	initial, initialErr := candidates(channel, false)
	if initialErr != nil {
		return initialErr
	}
	if len(initial) == 0 {
		job := e.Activity.Begin("stream_recovery", "Channel has no eligible source accounts", 0, 0)
		job.Finish("failed")
		return errors.New("no eligible sources configured")
	}
	if e.Preempt != nil {
		e.Preempt()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	updates := make(chan func(), 256)
	e.metrics.Add(1)
	go func() {
		defer e.metrics.Done()
		for update := range updates {
			update()
		}
	}()
	// History writes finish independently: a database writer must not delay a retune.
	defer close(updates)
	droppedUpdates := 0
	var failedWrites atomic.Int64
	record := func(id int64, r catalog.Result) {
		if !r.Success {
			if observer, ok := e.Catalogue.(interface{ Invalidate(int64) }); ok {
				observer.Invalidate(id)
			}
		}
		select {
		case updates <- func() {
			if err := e.Catalogue.Record(id, r); err != nil {
				failedWrites.Add(1)
				slog.Warn("stream health result could not be saved", "stream_id", id, "error_type", fmt.Sprintf("%T", err))
			}
		}:
		default:
			droppedUpdates++
		}
	}
	recordEvent := func(kind, channel string, id int64, reason string) {
		j := e.Activity.Begin("stream_recovery", reason, 0, id)
		outcome := "finished"
		if kind == "source_failure" {
			outcome = "failed"
		}
		if kind == "output_recovery" {
			outcome = "recovering"
		}
		if kind == "source_pool" {
			outcome = "waiting"
		}
		j.Finish(outcome)
		select {
		case updates <- func() { e.Catalogue.Event(kind, channel, id, reason) }:
		default:
			droppedUpdates++
		}
	}
	events := make(chan event, 512)
	feeds := map[int64]*feed{}
	var wg sync.WaitGroup
	// A feed gets one attempt per acquisition pass. Exhaust the alternatives
	// before cycling the pool again; this is not a timed source cooldown.
	failedPass := map[int64]int64{}
	var primary, ghost *feed
	var mux Output
	var muxStarted, outputRetry time.Time
	var outputFailures, outputRestarts int
	var recovery, lastIssue string
	var recoveryJob *activity.Job
	defer func() {
		if recoveryJob != nil {
			recoveryJob.Finish("paused")
		}
	}()
	outputCodec := ""
	muxVersion := 0
	failovers := 0
	defer func() {
		cancel()
		for _, f := range feeds {
			f.cancel()
		}
		if mux != nil {
			mux.Close()
		}
		wg.Wait()
		e.mu.Lock()
		delete(e.live, channel)
		e.mu.Unlock()
	}()
	send := func(v event) {
		select {
		case events <- v:
		case <-ctx.Done():
		}
	}
	launch := func(c catalog.Candidate) bool {
		releaseClaim := func() {}
		if claims, ok := e.Catalogue.(interface{ ClaimPlayback(int64) (func(), bool) }); ok {
			var claimed bool
			releaseClaim, claimed = claims.ClaimPlayback(c.StreamID)
			if !claimed {
				return false
			}
		}
		release, ok := e.Budget.Reserve(c.AccountID, catalog.ProviderKey(c.Provider), c.Limit, true)
		if !ok {
			releaseClaim()
			return false
		}
		sourceCtx, stop := context.WithCancel(ctx)
		buffer := e.Buffer
		if buffer == 0 {
			buffer = 3 * time.Second
		}
		stall := e.StallTimeout
		if stall == 0 {
			stall = 1500 * time.Millisecond
		}
		f := &feed{bufferDuration: buffer, stallTimeout: stall, candidate: c, cancel: stop, started: time.Now(), lastRecorded: time.Now()}
		feeds[c.StreamID] = f
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer release()
			defer releaseClaim()
			ready := false
			lastInspection := time.Time{}
			sample := make([]byte, 0, 256<<10)
			err := e.Copy(media.ObserveProcess(sourceCtx, func(pid int) { f.process.Store(int64(pid)) }), c.URL, func(b []byte) {
				if sourceCtx.Err() != nil {
					return
				}
				if f.analyzer.Push(b) != nil {
					stop()
					return
				}
				data := append([]byte(nil), b...)
				if !ready {
					if len(sample) < 4<<20 {
						sample = append(sample, b...)
					}
					stats := f.analyzer.Snapshot()
					if stats.VideoChanges >= 5 && stats.AudioChanges >= 5 && time.Since(lastInspection) >= 500*time.Millisecond {
						lastInspection = time.Now()
						info, err := e.Inspect(sourceCtx, sample)
						if err != nil {
							// Joining a live feed mid-GOP is normal. Keep collecting
							// until a keyframe arrives or the startup deadline expires.
							send(event{f: f, data: data})
							return
						}
						ready = true
						send(event{f: f, ready: true, info: info})
					}
				}
				send(event{f: f, data: data})
			})
			slog.Debug("upstream finished", "channel_id", channel, "stream_id", c.StreamID, "account_id", c.AccountID, "cancelled", sourceCtx.Err() != nil, "error_type", fmt.Sprintf("%T", err), "duration_ms", time.Since(f.started).Milliseconds())
			if sourceCtx.Err() == nil {
				if err == nil {
					err = errors.New("source ended")
				}
				send(event{f: f, err: err})
			} else {
				send(event{f: f, err: errors.New("source cancelled")})
			}
		}()
		return true
	}
	remove := func(f *feed, failed bool, reason string) {
		if f == nil {
			return
		}
		delete(feeds, f.candidate.StreamID)
		f.cancel()
		if !failed {
			recordEvent("source_released", channel, f.candidate.StreamID, "Unused startup candidate released; verified roster retained")
		}
		if failed {
			recordEvent("source_failure", channel, f.candidate.StreamID, reason)
			failedPass[f.candidate.StreamID] = time.Now().Unix()
			record(f.candidate.StreamID, catalog.Result{Success: false, Reason: reason, Stalls: 1, PlaybackSeconds: time.Since(f.lastRecorded).Seconds()})
		}
	}
	startMux := func(f *feed, restart bool) error {
		if (restart || (outputCodec != "" && outputCodec != f.info.Codec+"/"+f.info.AudioCodec)) && mux != nil {
			mux.Close()
			muxVersion = (muxVersion + 1) % 32
			mux = nil
		}
		if mux == nil {
			var err error
			if e.NewOutput != nil {
				mux, err = e.NewOutput(ctx, emit, muxVersion)
			} else {
				mux, err = media.Remux(ctx, e.Binary, emit, muxVersion)
			}
			muxStarted = time.Now()
			outputCodec = f.info.Codec + "/" + f.info.AudioCodec
			if err != nil {
				mux = nil
				return err
			}
		}
		for _, b := range f.replay() {
			if !mux.Send(b) {
				return errors.New("output backpressure")
			}
		}
		return nil
	}
	outputFailed := func(reason string) {
		if mux != nil {
			mux.Close()
			mux = nil
		}
		outputRestarts++
		muxVersion = (muxVersion + 1) % 32
		outputFailures++
		delay := 250 * time.Millisecond * time.Duration(1<<min(outputFailures-1, 5))
		outputRetry = time.Now().Add(delay)
		lastIssue = reason
		recovery = "Restarting channel output"
		if recoveryJob == nil {
			recoveryJob = e.Activity.Begin("output_recovery", recovery, 0, 0)
		}
		recoveryJob.Stage(reason + "; retrying output")
		recordEvent("output_recovery", channel, 0, reason)
	}
	uhdAllowed := func() bool {
		if policy, ok := e.Catalogue.(interface{ UHDAllowed() bool }); ok {
			return policy.UHDAllowed()
		}
		return false
	}
	recruitDelay := 500 * time.Millisecond
	poolWaiting := false
	recruit := func() {
		if primary != nil && ghost != nil && primary.candidate.UHD == ghost.candidate.UHD {
			recruitDelay = 500 * time.Millisecond
			return
		}
		pool, err := candidates(channel, true)
		if err != nil {
			return
		}
		race := e.RaceSize
		if policy, ok := e.Catalogue.(interface{ RaceSize(string) int }); ok {
			race = policy.RaceSize(channel)
		}
		if race == 0 {
			race = 5
		}
		more, _ := candidates(channel, false)
		seen := map[int64]bool{}
		for _, c := range pool {
			seen[c.StreamID] = true
		}
		for _, c := range more {
			if !seen[c.StreamID] {
				pool = append(pool, c)
				seen[c.StreamID] = true
			}
		}
		// Only remember failures still in this bounded candidate pool.
		for id := range failedPass {
			if !seen[id] {
				delete(failedPass, id)
			}
		}
		diversity := false
		if policy, ok := e.Catalogue.(interface{ PreferDiversity(string) bool }); ok {
			diversity = policy.PreferDiversity(channel)
		}
		pool = catalog.OrderCandidates(pool, false, diversity)
		if primary != nil && diversity {
			same := catalog.ProviderKey(primary.candidate.Provider)
			sort.SliceStable(pool, func(i, j int) bool {
				return catalog.ProviderKey(pool[i].Provider) != same && catalog.ProviderKey(pool[j].Provider) == same
			})
		}
		preferredUHD := uhdAllowed()
		if primary != nil {
			preferredUHD = primary.candidate.UHD
		}
		used := map[int64]bool{}
		pendingPreferred := false
		for _, f := range feeds {
			used[f.candidate.AccountID] = true
			if f != primary && f != ghost && f.candidate.UHD == preferredUHD {
				pendingPreferred = true
			}
		}
		preferred, fallback := []catalog.Candidate{}, []catalog.Candidate{}
		for _, c := range pool {
			if c.UHD && !uhdAllowed() {
				continue
			}
			if failedAt, failed := failedPass[c.StreamID]; failed && c.LastSuccess <= failedAt {
				continue
			}
			if feeds[c.StreamID] != nil || used[c.AccountID] {
				continue
			}
			if c.UHD == preferredUHD {
				preferred = append(preferred, c)
			} else {
				fallback = append(fallback, c)
			}
		}
		if len(preferred)+len(fallback) == 0 && !pendingPreferred && len(failedPass) > 0 {
			clear(failedPass) // Next channel-wide retry starts a fresh pass.
		}
		launched := 0
		launchTier := func(tier []catalog.Candidate) {
			for _, c := range tier {
				if len(feeds) >= race {
					break
				}
				if used[c.AccountID] {
					continue
				}
				if launch(c) {
					launched++
					used[c.AccountID] = true
				}
			}
		}
		launchTier(preferred)
		// Do not race HD against still-starting UHD (or change an established HD
		// session to UHD). Cross-class feeds are the last available fallback.
		if launched == 0 && !pendingPreferred && ghost == nil {
			launchTier(fallback)
		}

		if launched > 0 {
			recruitDelay = 500 * time.Millisecond
			poolWaiting = false
		} else {
			recruitDelay = min(10*time.Second, recruitDelay*2)
			if !poolWaiting && ghost == nil {
				recordEvent("source_pool", channel, 0, "No additional eligible source slots; retrying automatically")
				poolWaiting = true
			}
		}
	}
	promote := func(f *feed) error {
		old := primary
		primary = f
		if ghost == f {
			ghost = nil
		}
		restart := old != nil && old.info.Codec != f.info.Codec
		if old != nil {
			failovers++
			recordEvent("failover", channel, f.candidate.StreamID, "promoted live ghost")
		}
		return startMux(f, restart)
	}
	recruit()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	lastRecruit := time.Now()
	emptySince := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-events:
			f := ev.f
			if feeds[f.candidate.StreamID] != f {
				continue
			}
			if ev.err != nil {
				wasPrimary := primary == f
				if ghost == f {
					ghost = nil
				}
				if wasPrimary {
					if ghost != nil && !ghost.unhealthy(time.Now()) {
						if err := promote(ghost); err != nil {
							outputFailed("Could not start replacement output")
						}
					} else {
						primary = nil
					}
				}
				remove(f, true, "source stopped")
				continue
			}
			if ev.ready {
				f.ready = true
				f.info = ev.info
				f.analyzer.SetAudioCodec(ev.info.AudioCodec)
				record(f.candidate.StreamID, catalog.Result{Success: true, StartupMS: time.Since(f.started).Milliseconds(), Reason: "startup media decoded", Codec: f.info.Codec, Width: f.info.Width, Height: f.info.Height})
				if primary == nil {
					if err := promote(f); err != nil {
						outputFailed("Could not start channel output")
					}
				} else if ghost == nil && f != primary {
					ghost = f
				} else if ghost != nil && f != primary && ghost.candidate.UHD != primary.candidate.UHD && f.candidate.UHD == primary.candidate.UHD {
					old := ghost
					ghost = f
					remove(old, false, "")
				}
				if primary != nil && ghost != nil {
					for _, other := range feeds {
						if other != primary && other != ghost && (primary.candidate.UHD == ghost.candidate.UHD || other.candidate.UHD != primary.candidate.UHD) {
							remove(other, false, "")
						}
					}
				}
			}
			if ev.data != nil {
				now := time.Now()
				if f.ready && !f.lastPacket.IsZero() && now.Sub(f.lastPacket) > 350*time.Millisecond {
					f.stalls = append(f.stalls, now)
				}
				f.lastPacket = now
				for len(f.stalls) > 0 && now.Sub(f.stalls[0]) > 10*time.Second {
					f.stalls = f.stalls[1:]
				}
				f.buffer(ev.data)
				if primary == f && mux != nil {
					if !mux.Send(ev.data) {
						outputFailed("Output queue or process stopped")
					}
				}
			}
		case now := <-tick.C:
			if !uhdAllowed() {
				for _, f := range feeds {
					if f.candidate.UHD {
						if primary == f {
							primary = nil
						}
						if ghost == f {
							ghost = nil
						}
						remove(f, false, "")
						recordEvent("source_policy", channel, f.candidate.StreamID, "UHD disabled; selecting a non-UHD source")
					}
				}
				if primary == nil && ghost != nil && !ghost.unhealthy(now) {
					if err := promote(ghost); err != nil {
						outputFailed("Could not start non-UHD replacement")
					}
				}
			}
			// A corrupt elementary audio stream can retain progressing source PTS.
			// Verify audio after decoding as well, while distinguishing a stopped
			// output process (handled above) from a source losing only its audio.
			audioStalled := false
			if health, ok := mux.(interface{ OutputStats() media.Stats }); ok && primary != nil {
				st := health.OutputStats()
				audioStalled = !mux.Stopped() && now.Sub(muxStarted) > 5*time.Second && !st.LastVideo.IsZero() && now.Sub(st.LastVideo) < time.Second && (st.LastAudio.IsZero() || now.Sub(st.LastAudio) > 3*time.Second)
				if decoder, ok := mux.(interface{ AudioDecodeFailed() bool }); ok && decoder.AudioDecodeFailed() && (mux.Stopped() || now.Sub(muxStarted) > 2*time.Second) && (st.LastAudio.IsZero() || now.Sub(st.LastAudio) > time.Second) {
					audioStalled = true
				}
			}
			if primary != nil && !primary.unhealthy(now) && !audioStalled {
				if mux != nil {
					at := mux.LastOutput()
					if at.IsZero() {
						at = muxStarted
					}
					if mux.Stopped() {
						outputFailed("Output process stopped")
					} else if now.Sub(at) > 5*time.Second {
						outputFailed("Output stopped delivering media")
					}
				}
				if mux == nil && !now.Before(outputRetry) {
					if err := startMux(primary, false); err != nil {
						outputFailed("Output restart failed; keeping healthy feeds")
					}
				}
				if mux != nil && !mux.LastOutput().IsZero() {
					recovery = ""
					if recoveryJob != nil {
						recoveryJob.Finish("finished")
						recoveryJob = nil
					}
					if now.Sub(muxStarted) > 30*time.Second {
						outputFailures = 0
					}
				}
			}
			if primary != nil && (primary.unhealthy(now) || audioStalled) {
				bad := primary
				if ghost != nil && !ghost.unhealthy(now) {
					if err := promote(ghost); err != nil {
						outputFailed("Could not start replacement output")
					}
				} else {
					primary = nil
				}
				reason := "video, audio, or transport stalled"
				if audioStalled {
					reason = "source audio could not be decoded"
				}
				remove(bad, true, reason)
			}
			if ghost != nil && ghost.unhealthy(now) {
				bad := ghost
				ghost = nil
				remove(bad, true, "standby stalled")
			}
			for _, f := range feeds {
				if !f.ready && f.unhealthy(now) {
					remove(f, true, "source startup timeout")
				}
			}
			if primary != nil {
				if recovery == "Waiting for a working source" {
					recovery = ""
				}
				emptySince = now
				if now.Sub(primary.lastRecorded) >= 30*time.Second {
					record(primary.candidate.StreamID, catalog.Result{Success: true, Reason: "stable playback", PlaybackSeconds: now.Sub(primary.lastRecorded).Seconds(), Codec: primary.info.Codec, Width: primary.info.Width, Height: primary.info.Height})
					primary.lastRecorded = now
				}
			}
			if now.Sub(lastRecruit) >= recruitDelay {
				recruit()
				lastRecruit = now
			}
			if primary == nil && now.Sub(emptySince) > 30*time.Second {
				// Keep an established downstream session available for recovery.
				// Before first playback, the HTTP handler still enforces its tune deadline.
				recovery = "Waiting for a working source"
			}
			retrySeconds := 0
			if now.Before(outputRetry) {
				retrySeconds = int(outputRetry.Sub(now).Seconds()) + 1
			}
			status := Status{Recovery: recovery, LastIssue: lastIssue, OutputRestarts: outputRestarts, RetrySeconds: retrySeconds, PendingMetrics: len(updates), DroppedMetrics: droppedUpdates + int(failedWrites.Load()), Channel: channel, Candidates: len(feeds), Failovers: failovers, Standby: "recruiting"}
			if primary != nil {
				status.Primary = primary.candidate.AccountID
				status.PrimaryProcess = int(primary.process.Load())
				status.PrimaryBytes = primary.analyzer.Snapshot().Bytes
				status.Codec = primary.info.Codec
				status.AudioCodec = "aac"
				status.SourceAudioCodec = primary.info.AudioCodec
				status.Width = primary.info.Width
				status.Height = primary.info.Height
			}
			if poolWaiting && ghost == nil {
				status.Standby = "waiting for eligible source slots"
			}
			if ghost != nil {
				status.Ghost = ghost.candidate.AccountID
				status.GhostProcess = int(ghost.process.Load())
				status.GhostAudioCodec = ghost.info.AudioCodec
				status.Standby = "receiving"
			}
			e.publish(status)
		}
	}
}

// FlushMetrics is called after all channel controllers stop during shutdown.
func (e *Engine) FlushMetrics(ctx context.Context) error {
	done := make(chan struct{})
	go func() { e.metrics.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
