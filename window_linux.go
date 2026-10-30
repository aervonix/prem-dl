package main

import (
	"os"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/sys"
)

// On Linux the window is measured and placed through Wails. Wayland neither tells an app where
// its window is nor lets it move it, so there only the size and maximised state come back.

func onWayland() bool { return os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("GDK_BACKEND") != "x11" }

// openSize is the size the window is made with: its size when it was last closed.
func (a *App) openSize(width, height int) (int, int) {
	if p, ok := a.savedWindow(); ok {
		return int(p.Right - p.Left), int(p.Bottom - p.Top)
	}
	return width, height
}

func (a *App) currentPlacement() (sys.WindowPlacement, bool) {
	ctx := a.rt()
	if ctx == nil {
		return sys.WindowPlacement{}, false
	}
	if wr.WindowIsMaximised(ctx) {
		// Its size now is the screen's; the normal size is the one saved before.
		p, ok := a.savedWindow()
		if !ok {
			p = sys.WindowPlacement{Right: defaultWidth, Bottom: defaultHeight}
		}
		p.Maximised = true
		return p, true
	}
	w, h := wr.WindowGetSize(ctx)
	x, y := 0, 0
	if !onWayland() {
		x, y = wr.WindowGetPosition(ctx)
	}
	return sys.WindowPlacement{Left: int32(x), Top: int32(y), Right: int32(x + w), Bottom: int32(y + h)}, true
}

// restorePlacement moves the window back where it was, where the desktop allows it. Its size
// was set when it was made (openSize).
func (a *App) restorePlacement(p sys.WindowPlacement) {
	if ctx := a.rt(); ctx != nil && !onWayland() && p.Left >= 0 && p.Top >= 0 {
		wr.WindowSetPosition(ctx, int(p.Left), int(p.Top))
	}
}
