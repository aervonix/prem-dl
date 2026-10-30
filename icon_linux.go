package main

import _ "embed"

// appIcon is the app's icon for the tray, the window and the desktop entry.
//
//go:embed build/appicon.png
var appIcon []byte
