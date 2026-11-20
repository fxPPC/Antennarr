//go:build desktop

package main

import (
	"os"
	"runtime"

	"antennarr/internal/desktopapp"
)

// Cocoa must run on the original process thread, including after Go package initialisation.
func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--serve" || os.Args[1] == "healthcheck") {
		serverMain()
		return
	}
	desktopapp.Main(serve)
}
