// Package stream shares one bounded media pipeline per active channel.
package stream

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"sync"
	"time"
)

// A 4 MiB standby replay arrives in a burst. At 1,316-byte output chunks,
// this queue allows that bounded burst plus headroom without evicting a viewer
// before its HTTP writer has a chance to run. Stalled readers remain bounded.
const subscriberSlots = 4096

var ErrBusy = errors.New("all tuners in use")

type Runner func(context.Context, string, func([]byte)) error

type Manager struct {
	mu              sync.Mutex
	channels        map[string]*channel
	runners         map[string]*channel
	active, limit   int
	grace           time.Duration
	runner          Runner
	closing         bool
	admissionPaused bool
	wg              sync.WaitGroup
}
type channel struct {
	id          string
	done        chan struct{}
	cancel      context.CancelFunc
	subscribers map[chan []byte]bool
	timer       *time.Timer
}
type Outgoing struct {
	Channel string `json:"channel_id"`
	Viewers int    `json:"viewers"`
}
type Status struct {
	Viewers  int        `json:"viewers"`
	Channels int        `json:"active_channels"`
	Limit    int        `json:"max_outgoing_streams"`
	Outgoing []Outgoing `json:"outgoing_streams"`
}

func New(limit int, grace time.Duration, runner Runner) *Manager {
	return &Manager{channels: map[string]*channel{}, runners: map[string]*channel{}, limit: limit, grace: grace, runner: runner}
}

// SetLimit applies to new viewers; existing playback is never evicted.
func (m *Manager) SetLimit(limit int) { m.mu.Lock(); defer m.mu.Unlock(); m.limit = limit }
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Status{Limit: m.limit, Viewers: m.active, Channels: len(m.channels), Outgoing: []Outgoing{}}
	for id, c := range m.channels {
		if len(c.subscribers) > 0 {
			out.Outgoing = append(out.Outgoing, Outgoing{Channel: id, Viewers: len(c.subscribers)})
		}
	}
	sort.Slice(out.Outgoing, func(i, j int) bool { return out.Outgoing[i].Channel < out.Outgoing[j].Channel })
	return out
}
func (m *Manager) Subscribe(id, fixture string) (<-chan []byte, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admissionPaused {
		return nil, nil, ErrBusy
	}
	if m.closing {
		return nil, nil, errors.New("shutting down")
	}
	if m.active >= m.limit {
		return nil, nil, ErrBusy
	}
	c := m.channels[id]
	if c == nil {
		// A channel change must not compete with unused grace-period feeds.
		for oldID, old := range m.channels {
			if len(old.subscribers) == 0 {
				if old.timer != nil {
					old.timer.Stop()
				}
				delete(m.channels, oldID)
				old.cancel()
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		previous := m.runners[id]
		c = &channel{id: id, done: make(chan struct{}), cancel: cancel, subscribers: map[chan []byte]bool{}}
		m.channels[id] = c
		m.runners[id] = c
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer close(c.done)
			// Returning quickly to a channel must not overlap its previous
			// controller's feed cleanup, metrics, or live-status teardown.
			if previous != nil {
				<-previous.done
			}
			if ctx.Err() == nil {
				_ = m.runner(ctx, fixture, func(b []byte) { m.broadcast(c, b) })
			}
			m.finish(id, c)
		}()
	}
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	sub := make(chan []byte, subscriberSlots)
	c.subscribers[sub] = true
	m.active++
	var once sync.Once
	return sub, func() { once.Do(func() { m.release(id, c, sub) }) }, nil
}
func (m *Manager) broadcast(c *channel, b []byte) {
	// Runner memory may be reused; subscribers share this immutable copy.
	data := append([]byte(nil), b...)
	m.mu.Lock()
	defer m.mu.Unlock()
	for sub := range c.subscribers {
		select {
		case sub <- data:
		default:
			// A stalled viewer cannot hold up live media for other viewers.
			// Drop buffered media immediately; otherwise a slow HTTP client can
			// drain megabytes after eviction while appearing to use no tuner slot.
		draining:
			for {
				select {
				case <-sub:
				default:
					break draining
				}
			}
			close(sub)
			delete(c.subscribers, sub)
			m.active--
		}
	}
	m.idleLocked(c.id, c)
}
func (m *Manager) release(id string, c *channel, sub chan []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.subscribers[sub] {
		delete(c.subscribers, sub)
		close(sub)
		m.active--
	}
	m.idleLocked(id, c)
}
func (m *Manager) idleLocked(id string, c *channel) {
	if m.channels[id] != c || len(c.subscribers) != 0 || c.timer != nil {
		return
	}
	c.timer = time.AfterFunc(m.grace, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.channels[id] == c && len(c.subscribers) == 0 {
			delete(m.channels, id)
			c.cancel()
		}
	})
}
func (m *Manager) finish(id string, c *channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.channels[id] == c {
		delete(m.channels, id)
	}
	if m.runners[id] == c {
		delete(m.runners, id)
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	for sub := range c.subscribers {
		close(sub)
		delete(c.subscribers, sub)
		m.active--
	}
}

// Stop ends every viewer of one shared channel and cancels its upstream pair.
// Removing the channel first lets a later tune start a fresh controller safely.
func (m *Manager) Stop(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.channels[id]
	if c == nil {
		return false
	}
	delete(m.channels, id)
	if c.timer != nil {
		c.timer.Stop()
	}
	c.cancel()
	for sub := range c.subscribers {
	drain:
		for {
			select {
			case <-sub:
			default:
				break drain
			}
		}
		close(sub)
		delete(c.subscribers, sub)
		m.active--
	}
	return true
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closing = true
	for _, c := range m.channels {
		if c.timer != nil {
			c.timer.Stop()
		}
		c.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func FixtureRunner(binary, dir string) Runner {
	return func(ctx context.Context, fixture string, emit func([]byte)) error {
		if fixture != "720" && fixture != "480" && fixture != "mpeg2" {
			return errors.New("invalid fixture")
		}
		cmd := exec.CommandContext(ctx, binary, "-nostdin", "-hide_banner", "-loglevel", "error", "-re", "-stream_loop", "-1", "-i", dir+"/"+fixture+".ts", "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-mpegts_flags", "+resend_headers", "-f", "mpegts", "pipe:1")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err = cmd.Start(); err != nil {
			return err
		}
		buf := make([]byte, 188*7)
		for {
			n, e := io.ReadFull(stdout, buf)
			if n > 0 {
				emit(buf[:n])
			}
			if e != nil {
				break
			}
		}
		return cmd.Wait()
	}
}

// WithIdle prevents a new tune racing a channel-lineup replacement. Existing
// playback is never interrupted; callers receive ErrBusy until viewers leave.
func (m *Manager) WithIdle(fn func() error) error {
	m.mu.Lock()
	if m.active > 0 || m.closing || m.admissionPaused {
		m.mu.Unlock()
		return ErrBusy
	}
	m.admissionPaused = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.admissionPaused = false; m.mu.Unlock() }()
	return fn()
}
