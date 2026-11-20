//go:build !windows

package systeminfo

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func systemName() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if b, err := exec.CommandContext(ctx, "uname", "-a").Output(); err == nil {
		return strings.TrimSpace(string(b))
	}
	return runtime.GOOS + " " + runtime.GOARCH
}

func diskSpace(path string) (free, total uint64) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) == nil {
		return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize)
	}
	return 0, 0
}
