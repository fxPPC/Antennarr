//go:build !linux

package checkcpu

import (
	"context"
	"os/exec"
)

// CPU affinity is Linux-specific. Other platforms still apply the worker limits.
func Enter(ctx context.Context) (context.Context, func(), error) { return ctx, func() {}, nil }
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, binary, args...)
}
