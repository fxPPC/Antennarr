package engine

import (
	"antennarr/internal/catalog"
	"antennarr/internal/stream"
	"context"
	"errors"
	"strings"
	"time"
)

// FixtureCatalogue is enabled only by the explicit ANTENNARR_TEST_FAILOVER flag.
// It supplies generated sources; it never contacts an external provider.
type FixtureCatalogue struct{ Catalogue }

func (f FixtureCatalogue) Candidates(channel string, healthy bool) ([]catalog.Candidate, error) {
	if channel != "test-1" {
		return f.Catalogue.Candidates(channel, healthy)
	}
	out := []catalog.Candidate{}
	for i := 1; i <= 5; i++ {
		out = append(out, catalog.Candidate{StreamID: -int64(i), AccountID: -int64(i), Provider: "fixture", Limit: 1, URL: []string{"", "fixture://primary", "fixture://ghost", "fixture://third", "fixture://fourth", "fixture://fifth"}[i]})
	}
	return out, nil
}
func (f FixtureCatalogue) Record(id int64, r catalog.Result) error {
	if id < 0 {
		return nil
	}
	return f.Catalogue.Record(id, r)
}
func FixtureCopy(binary, dir string, fallback CopyFunc) CopyFunc {
	return func(ctx context.Context, raw string, emit func([]byte)) error {
		if !strings.HasPrefix(raw, "fixture://") {
			return fallback(ctx, raw, emit)
		}
		child, cancel := context.WithCancel(ctx)
		defer cancel()
		name := strings.TrimPrefix(raw, "fixture://")
		if name == "primary" {
			timer := time.AfterFunc(25*time.Second, cancel)
			defer timer.Stop()
		} else if name == "ghost" {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(250 * time.Millisecond):
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
		}
		fixture := "720"
		if name == "ghost" {
			fixture = "480"
		}
		err := stream.FixtureRunner(binary, dir)(child, fixture, emit)
		if child.Err() != nil && ctx.Err() == nil {
			return errors.New("deliberately induced fixture failure")
		}
		return err
	}
}
