package main

import (
	"antennarr/internal/desktopapp"
	"runtime"
)

func main() {
	runtime.LockOSThread()
	desktopapp.Main(nil)
}
