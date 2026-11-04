// Package monitor assembles safe dashboard data through read-only boundaries.
// It never schedules checks, opens streams, or writes to the catalogue.
package monitor

import (
	"antennarr/internal/activity"
	"antennarr/internal/catalog"
	"antennarr/internal/engine"
	"antennarr/internal/roster"
	"antennarr/internal/stream"
	"context"
	"log/slog"
	"sync"
	"time"
)

type Snapshot struct {
	DatabaseError string                 `json:"database_error,omitempty"`
	Collected     int64                  `json:"collected"`
	Database      catalog.MonitorSummary `json:"database"`
	Playback      stream.Status          `json:"playback"`
	Live          []engine.Status        `json:"live"`
	Work          activity.Snapshot      `json:"work"`
	Checker       roster.CheckerState    `json:"checker"`
}
type Collector struct {
	Store     *catalog.Store
	Playback  func() stream.Status
	Live      func() []engine.Status
	Work      func() activity.Snapshot
	Checker   func() roster.CheckerState
	mu        sync.Mutex
	cached    catalog.MonitorSummary
	at        time.Time
	lastError string
}

func (c *Collector) Snapshot(ctx context.Context) (Snapshot, error) {
	c.mu.Lock()
	if time.Since(c.at) > 5*time.Second {
		v, e := c.Store.ReadMonitor(ctx)
		if e != nil {
			c.lastError = "Catalogue summary delayed; showing last available counts"
			slog.Warn("status catalogue query failed", "error", e, "cached", c.cached.Collected > 0)
			if c.cached.Channels == nil {
				c.cached.Channels = map[string]string{}
			}
		} else {
			c.cached = v
			c.lastError = ""
		}
		c.at = time.Now()
	}
	db := c.cached
	dbError := c.lastError
	c.mu.Unlock()
	return Snapshot{DatabaseError: dbError, Collected: time.Now().Unix(), Database: db, Playback: c.Playback(), Live: c.Live(), Work: c.Work(), Checker: c.Checker()}, nil
}
