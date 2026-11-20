package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32        = windows.NewLazySystemDLL("user32.dll")
	messageBox    = user32.NewProc("MessageBoxW")
	destroyMenu   = user32.NewProc("DestroyMenu")
	destroyWindow = user32.NewProc("DestroyWindow")
	postMessage   = user32.NewProc("PostMessageW")
	notifyIcon    = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
)

const trayWindowClass = "AntennarrBetaNotificationArea"
const openApplication = 0x8003 // WM_APP + 3

func serverExecutable(dir string, _ bool) (string, error) {
	return filepath.Join(dir, "antennarr-server.exe"), nil
}

func loginLaunch() bool { return len(os.Args) == 2 && os.Args[1] == "--login-item" }

func wide(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }
func showError(text string) {
	messageBox.Call(0, uintptr(unsafe.Pointer(wide(text))), uintptr(unsafe.Pointer(wide("Antennarr"))), 0x10)
}
func hideConsole(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func beginDesktop() (func(), bool, error) {
	handle, err := windows.CreateMutexEx(nil, wide("Local\\Antennarr-Beta-Desktop"), 0, windows.SYNCHRONIZE)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		if !loginLaunch() {
			hwnd, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(wide(trayWindowClass))), 0)
			if hwnd != 0 {
				postMessage.Call(hwnd, openApplication, 0, 0)
			}
		}
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() { windows.CloseHandle(handle) }, false, nil
}

type windowClass struct {
	Size, Style                        uint32
	Callback                           uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Class                        *uint16
	SmallIcon                          uintptr
}
type message struct {
	Hwnd           uintptr
	ID             uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
	Private        uint32
}

// Layout matches the Unicode NOTIFYICONDATA structure on 64-bit Windows.
type notifyIconData struct {
	Size                       uint32
	Window                     uintptr
	ID, Flags, CallbackMessage uint32
	Icon                       uintptr
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Version                    uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GUID                       windows.GUID
	BalloonIcon                uintptr
}

func trayMenu(a *trayActions) (uintptr, error) {
	menu, _, err := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return 0, err
	}
	login, err := a.loginEnabled()
	if err != nil {
		destroyMenu.Call(menu)
		return 0, err
	}
	var check uintptr
	if login {
		check = 8
	} // MF_CHECKED
	viewers, coverage := a.s.quickStats()
	const disabled, separator = 1, 0x800
	state, detail := a.s.status()
	var webFlags uintptr
	if state != ready {
		webFlags = disabled
	}
	items := []struct {
		flags, id uintptr
		label     string
	}{
		{disabled, 0, detail}, {disabled, 0, viewers}, {disabled, 0, coverage}, {separator, 0, ""},
		{webFlags, adminID, "Open Antennarr"}, {webFlags, settingsID, "Settings"},
		{0, folderID, "Show data folder"}, {check, loginID, "Start at login"}, {separator, 0, ""}, {0, quitID, "Quit Antennarr"},
	}
	appendMenu := user32.NewProc("AppendMenuW")
	for _, item := range items {
		if ok, _, err := appendMenu.Call(menu, item.flags, item.id, uintptr(unsafe.Pointer(wide(item.label)))); ok == 0 {
			destroyMenu.Call(menu)
			return 0, err
		}
	}
	return menu, nil
}

func popup(hwnd uintptr, a *trayActions, x, y int32) error {
	menu, err := trayMenu(a)
	if err != nil {
		return err
	}
	defer destroyMenu.Call(menu)
	if x == -1 && y == -1 {
		var point struct{ X, Y int32 }
		user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
		x, y = point.X, point.Y
	}
	user32.NewProc("SetForegroundWindow").Call(hwnd)
	id, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x100|0x02, uintptr(x), uintptr(y), 0, hwnd, 0)
	// This benign message lets the shell dismiss the next popup on an outside click.
	postMessage.Call(hwnd, 0, 0, 0)
	return a.handle(id)
}

func desktop(s *session) {
	if err := runTray(s); err != nil {
		showError(err.Error())
	}
}

func runTray(s *session) error {
	const callbackMessage, childStopped = 0x8001, 0x8002 // WM_APP messages
	const wmDestroy, wmClose, wmEndSession, wmContextMenu, wmTimer = 2, 0x10, 0x16, 0x7b, 0x113
	const nimAdd, nimDelete, nimSetVersion = 0, 2, 4
	a, err := newTrayActions(s, runKey, func(url string) error {
		return windows.ShellExecute(0, wide("open"), wide(url), nil, nil, windows.SW_SHOWNORMAL)
	})
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	iconPath := filepath.Join(filepath.Dir(self), "Antennarr.ico")
	icon, _, err := user32.NewProc("LoadImageW").Call(0, uintptr(unsafe.Pointer(wide(iconPath))), 1, 0, 0, 0x10|0x40)
	if icon == 0 {
		return fmt.Errorf("could not load the Antennarr tray icon: %w", err)
	}
	defer user32.NewProc("DestroyIcon").Call(icon)
	taskbarCreated, _, err := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(wide("TaskbarCreated"))))
	if taskbarCreated == 0 {
		return err
	}
	var data notifyIconData
	data.Size = uint32(unsafe.Sizeof(data))
	data.Flags, data.CallbackMessage, data.ID, data.Icon = 0x01|0x02|0x04|0x80, callbackMessage, 1, icon
	tip, _ := windows.UTF16FromString("Antennarr Beta")
	copy(data.Tip[:], tip)
	registered := false
	opened := loginLaunch()
	registerIcon := func() {
		data.Version = 0
		ok, _, _ := notifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
		registered = ok != 0
		if registered {
			data.Version = 4
			notifyIcon.Call(nimSetVersion, uintptr(unsafe.Pointer(&data)))
		}
	}
	callback := windows.NewCallback(func(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case uint32(taskbarCreated):
			registered = false
			registerIcon()
		case wmTimer:
			if !registered {
				registerIcon()
			}
			if state, _ := s.status(); state == ready && !opened {
				opened = true
				if err := a.handle(adminID); err != nil {
					showError(err.Error())
				}
			}
		case callbackMessage:
			switch l & 0xffff {
			case wmContextMenu, 0x400, 0x401: // NIN_SELECT and NIN_KEYSELECT
				if err := popup(hwnd, a, int32(int16(w)), int32(int16(w>>16))); err != nil {
					showError(err.Error())
				}
			}
		case openApplication:
			opened = false
		case wmClose:
			s.stop()
		case wmEndSession:
			if w != 0 {
				s.stop()
				select {
				case <-s.done:
				case <-time.After(4 * time.Second):
				}
			}
		case childStopped:
			destroyWindow.Call(hwnd)
		case wmDestroy:
			notifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
			user32.NewProc("PostQuitMessage").Call(0)
		default:
			v, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), w, l)
			return v
		}
		return 0
	})
	name := wide(trayWindowClass)
	instance, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	class := windowClass{Callback: callback, Instance: instance, Icon: icon, SmallIcon: icon, Class: name}
	class.Size = uint32(unsafe.Sizeof(class))
	if r, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); r == 0 {
		return err
	}
	defer user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), instance)
	// A hidden top-level window can receive Explorer's taskbar restart broadcast.
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0x80, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(wide("Antennarr Beta"))), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		return err
	}
	data.Window = hwnd
	registerIcon()
	user32.NewProc("SetTimer").Call(hwnd, 1, 2000, 0)
	go func() { <-s.done; postMessage.Call(hwnd, childStopped, 0, 0) }()
	var m message
	for {
		r, _, err := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) < 0 {
			destroyWindow.Call(hwnd)
			return err
		}
		if r == 0 {
			break
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
	}
	if state, detail := s.status(); state == failed {
		return errors.New(detail)
	}
	return nil
}
