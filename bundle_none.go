//go:build !bundled

package main

// Builds without -tags bundled (wails dev, go test) carry no tools; bundleFS stays nil.
