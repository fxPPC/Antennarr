package main

import (
	"antennarr/internal/catalog"
	"antennarr/internal/desktopstats"
	"antennarr/internal/stream"
	"context"
	"time"
)

func writeDesktopStats(ctx context.Context, report func(desktopstats.Counters) error, store *catalog.Store, playback func() stream.Status) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		var counts desktopstats.Counters
		// One indexed roster lookup per enabled channel avoids scanning the
		// provider catalogue whenever the desktop menu updates.
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := store.DB.QueryRowContext(queryCtx, `SELECT count(*), coalesce(sum(EXISTS
			(SELECT 1 FROM roster r WHERE r.channel_id=c.id)),0)
			FROM active_channels c WHERE c.enabled=1`).Scan(&counts.Channels, &counts.WithFeeds)
		cancel()
		if err == nil {
			counts.Viewers = playback().Viewers
			if report(counts) != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
