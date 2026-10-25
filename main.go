package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "yt-dlp GUI",
		Width:     1100,
		Height:    760,
		MinWidth:  820,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 19, B: 24, A: 255},
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
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
