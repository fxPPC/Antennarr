package netguard

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestTimeoutClassification(t *testing.T) {
	for _, e := range []error{context.DeadlineExceeded, errors.Join(errors.New("wrapped"), context.DeadlineExceeded), &net.DNSError{IsTimeout: true}} {
		if !IsTimeout(e) {
			t.Fatal(e)
		}
	}
	for _, e := range []error{nil, context.Canceled, errors.New("network refused")} {
		if IsTimeout(e) {
			t.Fatal(e)
		}
	}
}
