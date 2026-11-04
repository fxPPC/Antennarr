package ingest

import (
	"antennarr/internal/netguard"
	"context"
	"errors"
)

// ProviderError exposes only fixed categories/status codes. The wrapped cause
// supports cancellation/pacing checks but is never rendered into diagnostics.
type ProviderError struct {
	Kind   string
	Status int
	cause  error
}

func (e ProviderError) Error() string { return "provider request: " + e.Kind }
func (e ProviderError) Unwrap() error { return e.cause }
func FailureKind(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if netguard.IsTimeout(err) {
		return "timeout"
	}
	var p ProviderError
	if errors.As(err, &p) {
		return p.Kind
	}
	var tagged interface{ FailureKind() string }
	if errors.As(err, &tagged) {
		return tagged.FailureKind()
	}
	return "transport_error"
}
func FailureStatus(err error) int {
	var p ProviderError
	if errors.As(err, &p) && p.Status != 0 {
		return p.Status
	}
	var tagged interface{ HTTPStatus() int }
	if errors.As(err, &tagged) {
		return tagged.HTTPStatus()
	}
	return 0
}
