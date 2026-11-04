package media

import "context"

type processObserverKey struct{}

// ObserveProcess reports the source adapter's PID for protected diagnostics.
// URLs and credentials never form part of that report.
func ObserveProcess(ctx context.Context, started func(int)) context.Context {
	return context.WithValue(ctx, processObserverKey{}, started)
}
func sourceProcessStarted(ctx context.Context, pid int) {
	if started, ok := ctx.Value(processObserverKey{}).(func(int)); ok {
		started(pid)
	}
}
