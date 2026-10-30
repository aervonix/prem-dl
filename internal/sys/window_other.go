//go:build !windows

package sys

// WindowPlacement is the main window's normal bounds and whether it is maximised.
type WindowPlacement struct {
	Left, Top, Right, Bottom int32
	Maximised                bool
}

// MainWindowPlacement is not available here; the window opens at its default size.
func MainWindowPlacement() (WindowPlacement, bool) { return WindowPlacement{}, false }

// OnScreen reports whether the bounds are usable. Which monitor they are on can't be checked
// here, only that the size is sane.
func (p WindowPlacement) OnScreen() bool {
	w, h := p.Right-p.Left, p.Bottom-p.Top
	return w >= 200 && h >= 200 && w <= 16384 && h <= 16384
}

// RestoreMainWindow does nothing here.
func RestoreMainWindow(WindowPlacement) bool { return false }

// RaiseMainWindow does nothing here.
func RaiseMainWindow() {}
