package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework CoreServices
#include <stdlib.h>
int desktopMenu(const char *url, const char *settings, const char *folder, const char *scratch);
void desktopAlert(const char *message);
int registerServerBundle(const char *path);
*/
import "C"
import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

var windowSession *session

func serverExecutable(dir string, register bool) (string, error) {
	bundle := filepath.Join(dir, "..", "Helpers", "Antennarr Server.app")
	if register {
		path := C.CString(bundle)
		defer C.free(unsafe.Pointer(path))
		if status := C.registerServerBundle(path); status != 0 {
			return "", fmt.Errorf("cannot register Antennarr Server with macOS (status %d)", status)
		}
	}
	return filepath.Join(bundle, "Contents", "MacOS", "antennarr-server"), nil
}

//export desktopState
func desktopState() C.int { n, _ := windowSession.status(); return C.int(n) }

//export desktopDetail
func desktopDetail() *C.char { _, detail := windowSession.status(); return C.CString(detail) }

//export desktopViewers
func desktopViewers() *C.char { viewers, _ := windowSession.quickStats(); return C.CString(viewers) }

//export desktopCoverage
func desktopCoverage() *C.char { _, coverage := windowSession.quickStats(); return C.CString(coverage) }

func beginDesktop() (func(), bool, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, false, err
	}
	base = filepath.Join(base, "Antennarr Beta")
	if err = os.MkdirAll(base, 0700); err != nil {
		return nil, false, err
	}
	lock, err := os.OpenFile(filepath.Join(base, "desktop.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() { lock.Close() }, false, nil
}
func hideConsole(cmd *exec.Cmd) {}
func showError(message string) {
	text := C.CString(message)
	defer C.free(unsafe.Pointer(text))
	C.desktopAlert(text)
}
func runMenu(s *session, scratch *C.char) bool {
	windowSession = s
	url, settings, folder := C.CString(s.adminURL+"/admin"), C.CString(s.settingsURL()), C.CString(s.data)
	defer C.free(unsafe.Pointer(url))
	defer C.free(unsafe.Pointer(settings))
	defer C.free(unsafe.Pointer(folder))
	return C.desktopMenu(url, settings, folder, scratch) != 0
}
func desktop(s *session) { runMenu(s, nil) }
func menuSmoke(s *session) error {
	dir, err := os.MkdirTemp("", "antennarr-login-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := C.CString(dir)
	defer C.free(unsafe.Pointer(path))
	if !runMenu(s, path) {
		return errors.New("Mac menu or login-item test failed")
	}
	return s.menuReport(true, true, true)
}
