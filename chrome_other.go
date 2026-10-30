//go:build !linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

const noChromeMessage = "Couldn't find Chrome on this computer"

// detectBrowsers finds Chrome, the one browser the helper runs in here. The Cookies tab lists
// yt-dlp's browsers itself.
func detectBrowsers() []Browser {
	if p := findChrome(); p != "" {
		return []Browser{{ID: "chrome", Label: "Chrome", Family: "chrome", Installed: true, Helper: true, command: []string{p}}}
	}
	return nil
}

// findChrome returns chrome.exe from its usual install locations.
func findChrome() string {
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("chrome"); err == nil {
		return p
	}
	return ""
}

// startBrowser starts Chrome as Windows always has.
func startBrowser(cmd *exec.Cmd) error { return cmd.Start() }

// sandboxedHelperDir is for Linux's snap and Flatpak browsers.
func sandboxedHelperDir() string { return "" }
