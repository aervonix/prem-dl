package main

import (
	"encoding/json"
	"path/filepath"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/sys"
)

// The window opens where it was closed. Its placement is kept in window.json in the data folder
// rather than in settings: it isn't something the user sets, and it changes on every close.

func (a *App) windowFile() string { return filepath.Join(a.dataDir, "window.json") }

// savedWindow is the placement from the last close, if it is still on a connected monitor.
func (a *App) savedWindow() (sys.WindowPlacement, bool) {
	raw, err := settings.ReadFileRetry(a.windowFile())
	if err != nil {
		return sys.WindowPlacement{}, false
	}
	var p sys.WindowPlacement
	if json.Unmarshal(raw, &p) != nil || !p.OnScreen() {
		return sys.WindowPlacement{}, false
	}
	return p, true
}

func (a *App) saveWindow() {
	p, ok := sys.MainWindowPlacement()
	if !ok {
		return
	}
	if raw, err := json.Marshal(p); err == nil {
		_ = settings.WriteFileAtomic(a.windowFile(), raw, 0o600)
	}
}
