package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	settingsID = 101 + iota
	loginID
	quitID
	adminID
	folderID
)

const runKey = "Software\\Microsoft\\Windows\\CurrentVersion\\Run"
const loginValue = "Antennarr Beta"

type trayActions struct {
	s          *session
	keyPath    string
	executable string
	open       func(string) error
}

func newTrayActions(s *session, keyPath string, open func(string) error) (*trayActions, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &trayActions{s: s, keyPath: keyPath, executable: self, open: open}, nil
}

func (a *trayActions) loginEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, a.keyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(loginValue)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	args, err := windows.DecomposeCommandLine(value)
	if err != nil || len(args) == 0 || !strings.EqualFold(args[0], a.executable) {
		return false, err
	}
	return len(args) == 1 || (len(args) == 2 && args[1] == "--login-item"), nil
}

func (a *trayActions) handle(id uintptr) error {
	switch id {
	case adminID:
		return a.open(a.s.adminURL + "/admin")
	case folderID:
		return a.open(a.s.data)
	case settingsID:
		return a.open(a.s.settingsURL())
	case loginID:
		enabled, err := a.loginEnabled()
		if err != nil {
			return err
		}
		key, _, err := registry.CreateKey(registry.CURRENT_USER, a.keyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		if enabled {
			return key.DeleteValue(loginValue)
		}
		return key.SetStringValue(loginValue, windows.ComposeCommandLine([]string{a.executable, "--login-item"}))
	case quitID:
		a.s.stop()
	}
	return nil
}

func menuSmoke(s *session) error {
	keyPath := fmt.Sprintf("Software\\AntennarrMenuTest-%d", os.Getpid())
	defer registry.DeleteKey(registry.CURRENT_USER, keyPath)
	var target string
	a, err := newTrayActions(s, keyPath, func(url string) error { target = url; return nil })
	if err != nil {
		return err
	}
	menu, err := trayMenu(a)
	if err != nil {
		return err
	}
	defer destroyMenu.Call(menu)
	viewers, coverage := s.quickStats()
	for i, expected := range []string{viewers, coverage} {
		var label [128]uint16
		n, _, _ := user32.NewProc("GetMenuStringW").Call(menu, uintptr(i+1), uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)), 0x400) // MF_BYPOSITION
		if n == 0 || windows.UTF16ToString(label[:]) != expected {
			return fmt.Errorf("notification menu statistic %d differs from the server", i+1)
		}
	}
	if err = a.handle(settingsID); err != nil || target != s.settingsURL() {
		return fmt.Errorf("Settings action returned %q (%v)", target, err)
	}
	if err = a.handle(loginID); err != nil {
		return err
	}
	enabled, err := a.loginEnabled()
	if err != nil || !enabled {
		return fmt.Errorf("login toggle did not enable (%v)", err)
	}
	if err = a.handle(loginID); err != nil {
		return err
	}
	stillEnabled, err := a.loginEnabled()
	if err != nil || stillEnabled {
		return fmt.Errorf("login toggle did not disable (%v)", err)
	}
	if err = a.handle(quitID); err != nil {
		return err
	}
	<-s.done
	return s.menuReport(enabled, !stillEnabled, true)
}
