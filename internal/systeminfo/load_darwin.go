package systeminfo

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

func hostLoad() (float64, bool) {
	// Darwin returns three uint32 fixed-point values, padding, and a 64-bit scale.
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(b) < 24 {
		return 0, false
	}
	scale := binary.NativeEndian.Uint64(b[16:])
	if scale == 0 {
		return 0, false
	}
	return float64(binary.NativeEndian.Uint32(b)) / float64(scale), true
}
