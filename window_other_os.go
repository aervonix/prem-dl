//go:build !linux

package main

import "yt-dlp-gui/internal/sys"

// The window's placement is read and set by sys (Win32 on Windows), including its normal size.

func (a *App) openSize(width, height int) (int, int) { return width, height }

func (a *App) currentPlacement() (sys.WindowPlacement, bool) { return sys.MainWindowPlacement() }

func (a *App) restorePlacement(p sys.WindowPlacement) { sys.RestoreMainWindow(p) }
