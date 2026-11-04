package netguard

import (
	"context"
	"errors"
	"net"
)

// IsTimeout recognises wrapped deadlines and network timeouts without exposing
// the underlying error text, which may contain a URL with credentials.
func IsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timed net.Error
	return errors.As(err, &timed) && timed.Timeout()
}
