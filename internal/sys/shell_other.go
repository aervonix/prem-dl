//go:build !windows && !linux

package sys

import "errors"

// Shell is only implemented on Windows and Linux.
type Shell struct{}

// StartShell isn't available here: there is no tray icon or taskbar progress.
func StartShell(ShellOptions) (*Shell, error) { return nil, errors.New("not supported") }

func (s *Shell) SetTray(bool, string)               {}
func (s *Shell) SetProgress(ProgressState, float64) {}
func (s *Shell) Refresh()                           {}
func (s *Shell) Close()                             {}

// MainWindowInFront can't tell here; it says yes so nothing is notified twice.
func MainWindowInFront() bool { return true }

// SetMainWindowFocused is for Linux, where the page reports it.
func SetMainWindowFocused(bool) {}
