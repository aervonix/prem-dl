//go:build bundled

package main

import (
	"embed"
	"io/fs"
)

// The tools fetched into bundle/ by `go run ./cmd/bundle`, built into the exe by
// `wails build -tags bundled`. Named one by one, so nothing else left there is built in.
//
//go:embed bundle/*.zip bundle/yt-dlp.version
var bundleFiles embed.FS

func init() {
	bundleFS, _ = fs.Sub(bundleFiles, "bundle")
}
