package desktopapp

import (
	"context"
	"io"
)

// AppKit and the tuner listeners run in this same process so macOS authorises one application.
func (s *session) startServer(_ string, log io.Writer, serve Server) (func() error, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	return func() error { return serve(ctx, log, s.recordStats) }, nil
}
