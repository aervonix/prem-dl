//go:build !linux

package main

// appIcon is only needed on Linux: Windows takes the icon from the exe's resources.
var appIcon []byte

func installDesktopIcon() error { return nil }
