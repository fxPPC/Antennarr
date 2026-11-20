//go:build !darwin && !windows

package systeminfo

import (
	"os"
	"strconv"
	"strings"
)

func hostLoad() (float64, bool) {
	b, err := os.ReadFile("/proc/loadavg")
	fields := strings.Fields(string(b))
	if err != nil || len(fields) == 0 {
		return 0, false
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	return load, err == nil
}
