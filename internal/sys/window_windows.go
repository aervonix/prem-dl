//go:build windows

package sys

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowPlacement is the main window's normal (not maximised) bounds in screen pixels, and
// whether it is maximised.
type WindowPlacement struct {
	Left      int32 `json:"left"`
	Top       int32 `json:"top"`
	Right     int32 `json:"right"`
	Bottom    int32 `json:"bottom"`
	Maximised bool  `json:"maximised"`
}

type winPlacement struct {
	length, flags, showCmd uint32
	minPos, maxPos         [2]int32
	normal                 [4]int32
}

const (
	swHide          = 0
	swShowMaximized = 3
	monitorNull     = 0 // MONITOR_DEFAULTTONULL
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = user32.NewProc("SetWindowPlacement")
	procMonitorFromRect    = user32.NewProc("MonitorFromRect")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
)

// mainWindow finds this process's Wails window.
func mainWindow() windows.HWND {
	var found windows.HWND
	pid := uint32(os.Getpid())
	cb := windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(h, &owner); err != nil || owner != pid {
			return 1
		}
		name := make([]uint16, 64)
		if n, err := windows.GetClassName(h, &name[0], int32(len(name))); err == nil && windows.UTF16ToString(name[:n]) == "wailsWindow" {
			found = h
			return 0
		}
		return 1
	})
	_ = windows.EnumWindows(cb, nil)
	return found
}

// MainWindowPlacement reads where the main window is, for restoring it next launch.
func MainWindowPlacement() (WindowPlacement, bool) {
	h := mainWindow()
	if h == 0 {
		return WindowPlacement{}, false
	}
	wp := winPlacement{length: uint32(unsafe.Sizeof(winPlacement{}))}
	if r, _, _ := procGetWindowPlacement.Call(uintptr(h), uintptr(unsafe.Pointer(&wp))); r == 0 {
		return WindowPlacement{}, false
	}
	n := wp.normal
	return WindowPlacement{Left: n[0], Top: n[1], Right: n[2], Bottom: n[3], Maximised: wp.showCmd == swShowMaximized}, true
}

// OnScreen reports whether the bounds still overlap a connected monitor.
func (p WindowPlacement) OnScreen() bool {
	if p.Right-p.Left < 200 || p.Bottom-p.Top < 150 {
		return false
	}
	// The top strip, where the title bar is, must be reachable.
	rect := [4]int32{p.Left, p.Top, p.Right, p.Top + 40}
	m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&rect)), monitorNull)
	return m != 0
}

// RestoreMainWindow moves the still-hidden main window to the saved normal bounds; Wails shows
// it (maximised if asked) once the page has loaded.
func RestoreMainWindow(p WindowPlacement) bool {
	h := mainWindow()
	if h == 0 || !p.OnScreen() {
		return false
	}
	wp := winPlacement{length: uint32(unsafe.Sizeof(winPlacement{})), showCmd: swHide, normal: [4]int32{p.Left, p.Top, p.Right, p.Bottom}}
	r, _, _ := procSetWindowPlacement.Call(uintptr(h), uintptr(unsafe.Pointer(&wp)))
	return r != 0
}

// RaiseMainWindow puts the main window above the others and asks for the foreground. Windows only
// grants that to the app the user is in, so a window raised for a link sent from the browser may
// end up on top without focus, which is still better than behind the browser.
func RaiseMainWindow() {
	h := mainWindow()
	if h == 0 {
		return
	}
	const swpNoSize, swpNoMove, swpNoActivate = 0x1, 0x2, 0x10
	topmost, notTopmost := ^uintptr(0), ^uintptr(1) // HWND_TOPMOST (-1), HWND_NOTOPMOST (-2)
	procSetWindowPos.Call(uintptr(h), topmost, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate)
	procSetWindowPos.Call(uintptr(h), notTopmost, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate)
	procSetForegroundWindow.Call(uintptr(h))
}
