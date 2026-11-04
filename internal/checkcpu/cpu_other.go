//go:build !linux

package checkcpu

import (
	"context"
	"os/exec"
)

// Production uses Linux CPU affinity. Other platforms support local fixtures.
func Enter(ctx context.Context) (context.Context, func(), error) { return ctx, func() {}, nil }
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, binary, args...)
}
