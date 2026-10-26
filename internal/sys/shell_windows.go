//go:build windows

package sys

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The notification-area (tray) icon and the taskbar button's progress bar. Both belong to the
// thread that created them, so they share one locked OS thread with a hidden window and its own
// message loop. Other goroutines change the wanted state and post a message; the thread applies it.

// ProgressState is what the taskbar button's progress bar shows.
type ProgressState int

const (
	ProgressNone ProgressState = iota
	ProgressIndeterminate
	ProgressNormal
	ProgressPaused // yellow
	ProgressError  // red
)

// MenuItem is one line of the tray icon's menu. A zero ID makes a separator.
type MenuItem struct {
	ID       int
	Label    string
	Default  bool // shown in bold: what clicking the icon does
	Disabled bool
}

// ShellOptions are the tray icon's callbacks. They run on a goroutine of their own, except Menu,
// which runs on the shell thread while the menu opens and must return quickly.
type ShellOptions struct {
	OnOpen func()            // the icon was clicked, or selected with the keyboard
	Menu   func() []MenuItem // the menu, built each time it opens
	OnMenu func(id int)      // a menu item was chosen
}

// Shell owns the tray icon and the taskbar progress.
type Shell struct {
	opts ShellOptions
	hwnd windows.HWND
	done chan struct{}

	mu   sync.Mutex
	want shellState

	forceApply bool // under mu: apply the progress even if unchanged

	// Touched only on the shell thread.
	have      shellState
	iconShown bool
	icon      windows.Handle
	taskbar   *comObject
	mainHWND  windows.HWND
}

type shellState struct {
	trayVisible bool
	tooltip     string
	progress    ProgressState
	value       int // 0..progressScale
}

const progressScale = 1000

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007B
	wmUser        = 0x0400
	wmApp         = 0x8000
	wmTrayIcon    = wmApp + 1 // the icon's callback message
	wmApply       = wmApp + 2 // the wanted state changed

	ninSelect           = wmUser + 0
	ninKeySelect        = ninSelect | 1
	ninBalloonUserClick = wmUser + 5

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage  = 0x01
	nifIcon     = 0x02
	nifTip      = 0x04
	nifShowTip  = 0x80
	iconVersion = 4 // NOTIFYICON_VERSION_4

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	imageIcon      = 1
	lrShared       = 0x8000
	lrDefaultSize  = 0x0040
	smCxSmIcon     = 49
	smCySmIcon     = 50
	idiApplication = 32512
	appIconID      = 3 // the resource ID Wails gives the app icon

	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	tbpfNoProgress    = 0x0
	tbpfIndeterminate = 0x1
	tbpfNormal        = 0x2
	tbpfError         = 0x4
	tbpfPaused        = 0x8
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type winMsg struct {
	HWnd     windows.HWND
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       [2]int32
	LPrivate uint32
}

var (
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	procCoInitializeEx        = ole32.NewProc("CoInitializeEx")
	procCoUninitialize        = ole32.NewProc("CoUninitialize")
	procCoCreateInstance      = ole32.NewProc("CoCreateInstance")
	procGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassEx       = user32.NewProc("RegisterClassExW")
	procCreateWindowEx        = user32.NewProc("CreateWindowExW")
	procDefWindowProc         = user32.NewProc("DefWindowProcW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procGetMessage            = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessage       = user32.NewProc("DispatchMessageW")
	procPostMessage           = user32.NewProc("PostMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenu            = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem    = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenuEx      = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procIsWindow              = user32.NewProc("IsWindow")
	procIsIconic              = user32.NewProc("IsIconic")
	procLoadImage             = user32.NewProc("LoadImageW")
	procDestroyIcon           = user32.NewProc("DestroyIcon")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")

	clsidTaskbarList  = windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidITaskbarList3  = windows.GUID{Data1: 0xEA1AFB91, Data2: 0x9E28, Data3: 0x4B86, Data4: [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
	shellWndProc      = windows.NewCallback(wndProc)
	msgTaskbarCreated uint32

	// One shell per process: the window procedure finds it here.
	current atomic.Pointer[Shell]
)

// StartShell creates the shell thread. The tray icon stays hidden until SetTray shows it.
func StartShell(opts ShellOptions) (*Shell, error) {
	s := &Shell{opts: opts, done: make(chan struct{})}
	if !current.CompareAndSwap(nil, s) {
		return nil, errors.New("the shell is already running")
	}
	ready := make(chan error, 1)
	go s.loop(ready)
	if err := <-ready; err != nil {
		current.Store(nil)
		return nil, err
	}
	return s, nil
}

func (s *Shell) loop(ready chan<- error) {
	defer close(s.done)
	// The window, the icon and the COM object all belong to this thread. It is never unlocked, so
	// the thread ends with the goroutine.
	runtime.LockOSThread()
	procCoInitializeEx.Call(0, coinitApartmentThreaded)
	defer procCoUninitialize.Call()

	inst, _, _ := procGetModuleHandle.Call(0)
	className, _ := windows.UTF16PtrFromString("PremDlShell")
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: shellWndProc, Instance: windows.Handle(inst), ClassName: className}
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		ready <- err
		return
	}
	title, _ := windows.UTF16PtrFromString("Prem-dl")
	// A real (never shown) top-level window, not a message-only one: those don't get the
	// TaskbarCreated broadcast that says Explorer restarted and the icon must be added again.
	h, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if h == 0 {
		ready <- err
		return
	}
	s.hwnd = windows.HWND(h)
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(name)))
	msgTaskbarCreated = uint32(r)
	s.icon = loadAppIcon(windows.Handle(inst))
	ready <- nil

	var m winMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // WM_QUIT, or an error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	if s.taskbar != nil {
		s.taskbar.release()
		s.taskbar = nil
	}
	if s.icon != 0 {
		procDestroyIcon.Call(uintptr(s.icon))
	}
}

// loadAppIcon loads the exe's own icon at the small-icon size, or the generic one.
func loadAppIcon(inst windows.Handle) windows.Handle {
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	if h, _, _ := procLoadImage.Call(uintptr(inst), appIconID, imageIcon, cx, cy, 0); h != 0 {
		return windows.Handle(h)
	}
	h, _, _ := procLoadImage.Call(0, idiApplication, imageIcon, 0, 0, lrShared|lrDefaultSize)
	return windows.Handle(h)
}

func wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	s := current.Load()
	if s == nil || s.hwnd != hwnd {
		// Messages sent while the window is being created, before loop has its handle.
		r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
		return r
	}
	switch msg {
	case wmApply:
		s.apply()
		return 0
	case wmTrayIcon:
		switch uint32(lParam & 0xFFFF) {
		case ninSelect, ninKeySelect, ninBalloonUserClick:
			if s.opts.OnOpen != nil {
				go s.opts.OnOpen()
			}
		case wmContextMenu:
			// With NOTIFYICON_VERSION_4 the anchor point is in wParam.
			s.showMenu(int32(int16(wParam&0xFFFF)), int32(int16((wParam>>16)&0xFFFF)))
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		s.removeIcon()
		procPostQuitMessage.Call(0)
		return 0
	}
	if msg != 0 && msg == msgTaskbarCreated {
		// Explorer restarted: the icon is gone and the taskbar object is stale.
		s.iconShown = false
		if s.taskbar != nil {
			s.taskbar.release()
			s.taskbar = nil
		}
		s.mu.Lock()
		s.forceApply = true
		s.mu.Unlock()
		s.apply()
		return 0
	}
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// SetTray shows or hides the tray icon and sets its tooltip (the first 127 characters).
func (s *Shell) SetTray(visible bool, tooltip string) {
	s.mu.Lock()
	s.want.trayVisible, s.want.tooltip = visible, tooltip
	s.mu.Unlock()
	s.post(wmApply)
}

// SetProgress sets the taskbar button's progress bar; fraction is 0..1.
func (s *Shell) SetProgress(state ProgressState, fraction float64) {
	v := int(fraction*progressScale + 0.5)
	v = max(0, min(progressScale, v))
	s.mu.Lock()
	s.want.progress, s.want.value = state, v
	s.mu.Unlock()
	s.post(wmApply)
}

// Refresh applies the taskbar progress again: the taskbar button is recreated whenever the window
// is shown after being hidden, and starts out empty.
func (s *Shell) Refresh() {
	s.mu.Lock()
	s.forceApply = true
	s.mu.Unlock()
	s.post(wmApply)
}

// Close removes the tray icon and stops the shell thread.
func (s *Shell) Close() {
	s.post(wmClose)
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
	current.CompareAndSwap(s, nil)
}

func (s *Shell) post(msg uint32) {
	procPostMessage.Call(uintptr(s.hwnd), uintptr(msg), 0, 0)
}

// apply brings the icon and the progress bar in line with the wanted state. Shell thread only.
func (s *Shell) apply() {
	s.mu.Lock()
	want := s.want
	force := s.forceApply
	s.forceApply = false
	s.mu.Unlock()

	switch {
	case want.trayVisible && !s.iconShown:
		s.addIcon(want.tooltip)
	case !want.trayVisible && s.iconShown:
		s.removeIcon()
	case want.trayVisible && want.tooltip != s.have.tooltip:
		nid := s.iconData(nifTip|nifShowTip, want.tooltip)
		procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	}
	s.have.trayVisible, s.have.tooltip = want.trayVisible, want.tooltip

	if force || want.progress != s.have.progress || want.value != s.have.value {
		if s.setTaskbar(want.progress, want.value) {
			s.have.progress, s.have.value = want.progress, want.value
		} else {
			// No taskbar button yet (the window is hidden): try again on the next change.
			s.have.progress = -1
		}
	}
}

func (s *Shell) iconData(flags uint32, tooltip string) notifyIconData {
	nid := notifyIconData{CbSize: uint32(unsafe.Sizeof(notifyIconData{})), HWnd: s.hwnd, UID: 1, UFlags: flags}
	if flags&nifTip != 0 {
		tip, _ := windows.UTF16FromString(tooltip)
		copy(nid.SzTip[:len(nid.SzTip)-1], tip)
	}
	return nid
}

func (s *Shell) addIcon(tooltip string) {
	nid := s.iconData(nifMessage|nifIcon|nifTip|nifShowTip, tooltip)
	nid.UCallbackMessage = wmTrayIcon
	nid.HIcon = s.icon
	if r, _, _ := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return // Explorer isn't running; TaskbarCreated will bring us back
	}
	nid.UVersion = iconVersion
	procShellNotifyIcon.Call(nimSetVersion, uintptr(unsafe.Pointer(&nid)))
	s.iconShown = true
}

func (s *Shell) removeIcon() {
	if !s.iconShown {
		return
	}
	nid := s.iconData(0, "")
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	s.iconShown = false
}

func (s *Shell) showMenu(x, y int32) {
	if s.opts.Menu == nil {
		return
	}
	items := s.opts.Menu()
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	for _, it := range items {
		if it.ID == 0 {
			procAppendMenu.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Disabled {
			flags |= mfGrayed
		}
		label, _ := windows.UTF16PtrFromString(it.Label)
		procAppendMenu.Call(menu, flags, uintptr(it.ID), uintptr(unsafe.Pointer(label)))
		if it.Default {
			procSetMenuDefaultItem.Call(menu, uintptr(it.ID), 0)
		}
	}
	// Without the foreground the menu doesn't close when you click elsewhere; the WM_NULL after
	// it is the documented workaround for it not opening again on the next click.
	procSetForegroundWindow.Call(uintptr(s.hwnd))
	id, _, _ := procTrackPopupMenuEx.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify, uintptr(x), uintptr(y), uintptr(s.hwnd), 0)
	procPostMessage.Call(uintptr(s.hwnd), wmNull, 0, 0)
	if id != 0 && s.opts.OnMenu != nil {
		go s.opts.OnMenu(int(id))
	}
}

// setTaskbar applies the progress to the main window's taskbar button. Shell thread only.
func (s *Shell) setTaskbar(state ProgressState, value int) bool {
	if s.mainHWND == 0 || !isWindow(s.mainHWND) {
		s.mainHWND = mainWindow()
		if s.mainHWND == 0 {
			return false
		}
	}
	if s.taskbar == nil {
		tb, err := newTaskbarList()
		if err != nil {
			return false
		}
		s.taskbar = tb
	}
	flag := uintptr(tbpfNoProgress)
	switch state {
	case ProgressIndeterminate:
		flag = tbpfIndeterminate
	case ProgressNormal:
		flag = tbpfNormal
	case ProgressPaused:
		flag = tbpfPaused
	case ProgressError:
		flag = tbpfError
	}
	// ITaskbarList3: SetProgressState is method 10, SetProgressValue method 9.
	if hr := s.taskbar.call(10, uintptr(s.mainHWND), flag); hr != 0 {
		return false
	}
	if flag == tbpfNormal || flag == tbpfPaused || flag == tbpfError {
		s.taskbar.call(9, uintptr(s.mainHWND), uintptr(value), progressScale)
	}
	return true
}

// comObject is a COM interface pointer: a pointer to a table of method pointers.
type comObject struct {
	vtbl *[16]uintptr
}

func (o *comObject) call(method int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObject) release() { o.call(2) }

func newTaskbarList() (*comObject, error) {
	var obj *comObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidITaskbarList3)), uintptr(unsafe.Pointer(&obj)))
	if hr != 0 || obj == nil {
		return nil, syscall.Errno(hr)
	}
	if hr := obj.call(3); hr != 0 { // HrInit
		obj.release()
		return nil, syscall.Errno(hr)
	}
	return obj, nil
}

func isWindow(h windows.HWND) bool {
	r, _, _ := procIsWindow.Call(uintptr(h))
	return r != 0
}

// MainWindowInFront reports whether the main window is the one the user is working in: shown, not
// minimised, and in the foreground.
func MainWindowInFront() bool {
	h := mainWindow()
	if h == 0 {
		return false
	}
	fg, _, _ := procGetForegroundWindow.Call()
	if windows.HWND(fg) != h {
		return false
	}
	visible, _, _ := procIsWindowVisible.Call(uintptr(h))
	iconic, _, _ := procIsIconic.Call(uintptr(h))
	return visible != 0 && iconic == 0
}
