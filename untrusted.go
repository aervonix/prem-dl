package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// toastText makes a title or error message safe for a Windows toast. Wails puts it in the toast's
// XML as <![CDATA[...]]> without escaping, so a video title containing "]]>" (sites other than
// YouTube allow it) would end the text and add elements of its own: images fetched from any
// path, buttons that open links. Control characters, which XML can't hold, are dropped too.
func toastText(s string) string {
	s = strings.ReplaceAll(s, "]]>", "]] >")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

// openable reports whether Open file may run path. The path comes from yt-dlp's output, so it is
// checked before anything touches it: it must be absolute, and a network path (\\host\share, where
// even a look at the file would send the user's credentials) only inside a download folder.
func openable(path string, dirs ...string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	network := strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
	if !network {
		return true
	}
	clean := filepath.Clean(path)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if rel, err := filepath.Rel(filepath.Clean(d), clean); err == nil && filepath.IsLocal(rel) {
			return true
		}
	}
	return false
}

// removeStaleTemp deletes the private copies of cookies and saved analyses (which hold cookies
// too) that a crash or a kill left in the temp folder. Each run removes its own; only ones older
// than any run could still be using are touched.
func removeStaleTemp() {
	dir := os.TempDir()
	for _, pattern := range []string{"ytgui-cookies-*.txt", "ytgui-info-*.json"} {
		names, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, name := range names {
			if fi, err := os.Lstat(name); err == nil && fi.Mode().IsRegular() && time.Since(fi.ModTime()) > 12*time.Hour {
				_ = os.Remove(name)
			}
		}
	}
}
