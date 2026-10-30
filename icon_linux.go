package main

import (
	_ "embed"

	"yt-dlp-gui/internal/sys"
)

// appIcon is the app's icon for the tray, the window and the desktop entry.
//
//go:embed build/appicon.png
var appIcon []byte

// installDesktopIcon puts the icon where the desktop entries, dock and notifications find it,
// unless a package (the .deb) installed it already.
func installDesktopIcon() error {
	if sys.SystemIcon(urlScheme) {
		return nil
	}
	return sys.InstallIcon(urlScheme, appIcon)
}
