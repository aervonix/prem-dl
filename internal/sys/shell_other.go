//go:build !windows

package sys

import "errors"

// ProgressState is what the taskbar button's progress bar shows.
type ProgressState int

const (
	ProgressNone ProgressState = iota
	ProgressIndeterminate
	ProgressNormal
	ProgressPaused
	ProgressError
)

// MenuItem is one line of the tray icon's menu. A zero ID makes a separator.
type MenuItem struct {
	ID       int
	Label    string
	Default  bool
	Disabled bool
}

// ShellOptions are the tray icon's callbacks.
type ShellOptions struct {
	OnOpen func()
	Menu   func() []MenuItem
	OnMenu func(id int)
}

// Shell is only implemented on Windows.
type Shell struct{}

// StartShell isn't available here: there is no tray icon or taskbar progress.
func StartShell(ShellOptions) (*Shell, error) { return nil, errors.New("not supported") }

func (s *Shell) SetTray(bool, string)               {}
func (s *Shell) SetProgress(ProgressState, float64) {}
func (s *Shell) Refresh()                           {}
func (s *Shell) Close()                             {}

// MainWindowInFront can't tell here; it says yes so nothing is notified twice.
func MainWindowInFront() bool { return true }

func RegisterURLScheme(scheme, name, exe string) error { return nil }
func URLSchemeCommand(string) string                   { return "" }
func SetRunAtSignIn(name, command string) error        { return errors.New("not supported") }
func RunAtSignIn(string) string                        { return "" }
func SetToastName(id, name string) error               { return nil }
func InstalledDir(name, exe string) string             { return "" }
func StartCommandLine(exe, cmdline string) error       { return errors.New("not supported") }
