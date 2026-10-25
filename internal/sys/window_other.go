//go:build !windows

package sys

// WindowPlacement is the main window's normal bounds and whether it is maximised.
type WindowPlacement struct {
	Left, Top, Right, Bottom int32
	Maximised                bool
}

// MainWindowPlacement is not available here; the window opens at its default size.
func MainWindowPlacement() (WindowPlacement, bool) { return WindowPlacement{}, false }

// OnScreen reports whether the bounds are usable.
func (p WindowPlacement) OnScreen() bool { return false }

// RestoreMainWindow does nothing here.
func RestoreMainWindow(WindowPlacement) bool { return false }
