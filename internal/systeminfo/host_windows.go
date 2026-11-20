package systeminfo

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

func systemName() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("Windows %d.%d build %d (%s)", v.MajorVersion, v.MinorVersion, v.BuildNumber, runtime.GOARCH)
}

func diskSpace(path string) (free, total uint64) {
	p, err := windows.UTF16PtrFromString(path)
	if err == nil && windows.GetDiskFreeSpaceEx(p, &free, &total, nil) == nil {
		return free, total
	}
	return 0, 0
}

func hostLoad() (float64, bool) { return 0, false }
