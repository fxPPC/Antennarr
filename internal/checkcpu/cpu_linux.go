//go:build linux

// Package checkcpu confines background work without capping live playback.
package checkcpu

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type key struct{}

func lastTwo(mask unix.CPUSet) unix.CPUSet {
	var limited unix.CPUSet
	n := 0
	for i := 1023; i >= 0 && n < 2; i-- {
		if mask.IsSet(i) {
			limited.Set(i)
			n++
		}
	}
	return limited
}

// Enter pins the serial check worker to two permitted CPUs. Restore its original
// mask before releasing the OS thread back to Go's scheduler.
func Enter(ctx context.Context) (context.Context, func(), error) {
	runtime.LockOSThread()
	var original unix.CPUSet
	if err := unix.SchedGetaffinity(0, &original); err != nil {
		runtime.UnlockOSThread()
		return ctx, func() {}, err
	}
	limited := lastTwo(original)
	if err := unix.SchedSetaffinity(0, &limited); err != nil {
		runtime.UnlockOSThread()
		return ctx, func() {}, err
	}
	ids := []string{}
	for i := 0; i < 1024; i++ {
		if limited.IsSet(i) {
			ids = append(ids, strconv.Itoa(i))
		}
	}
	return context.WithValue(ctx, key{}, strings.Join(ids, ",")), func() {
		// If restoration fails, exiting a locked goroutine discards its OS thread;
		// never return a restricted thread to the shared scheduler.
		if unix.SchedSetaffinity(0, &original) == nil {
			runtime.UnlockOSThread()
		}
	}, nil
}

// Command also confines subprocesses started by other goroutines in a check.
// A missing taskset fails the check closed instead of running unrestricted.
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	if cpus, ok := ctx.Value(key{}).(string); ok {
		argv := append([]string{"--cpu-list", cpus, binary}, args...)
		return exec.CommandContext(ctx, "taskset", argv...)
	}
	return exec.CommandContext(ctx, binary, args...)
}
