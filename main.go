package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	launch := parseLaunch(os.Args[1:])
	app := NewApp(launch)
	start := options.Normal
	if p, ok := app.savedWindow(); ok && p.Maximised {
		start = options.Maximised
	}
	width, height := app.openSize(defaultWidth, defaultHeight)

	err := wails.Run(&options.App{
		Title:     "Prem-dl",
		Width:     width,
		Height:    height,
		MinWidth:  820,
		MinHeight: 560,
		// Its position and normal size are restored in startup (see window.go).
		WindowStartState: start,
		// Started with Windows: the tray icon shows the window when wanted.
		StartHidden: launch.Background,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 255},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		// Two instances would race on the queue file, so a second launch just focuses the first.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "8d2f6c1e-5a3b-4c7d-9e10-3f4a5b6c7d8e",
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
		Linux: &linux.Options{
			// Names the window to the desktop, matching the prem-dl.desktop entry, so the dock
			// shows its name and icon.
			ProgramName: "prem-dl",
			Icon:        appIcon,
			// What Wails picks when Linux options are left out (wails#2977).
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
