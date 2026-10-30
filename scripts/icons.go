//go:build ignore

// Writes the app's icon into a hicolor icon theme for a package, the same way the app installs
// it for the user (sys.InstallIcon). scripts/release.sh runs it as
//
//	XDG_DATA_HOME=<package>/usr/share go run scripts/icons.go
package main

import (
	"fmt"
	"os"

	"yt-dlp-gui/internal/sys"
)

func main() {
	png, err := os.ReadFile("build/appicon.png")
	if err == nil {
		err = sys.InstallIcon("prem-dl", png)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "icons:", err)
		os.Exit(1)
	}
}
