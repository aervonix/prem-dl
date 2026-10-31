package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// appImagePath is the AppImage this copy runs from ($APPIMAGE, set by its runtime), when its
// folder can take a new one; "" for any other copy (the .deb, a plain build, a dev build).
func appImagePath() string {
	if devBuild && os.Getenv("PREMDL_TEST_APPIMAGE") != "" {
		return os.Getenv("PREMDL_TEST_APPIMAGE") // trying the swap in wails dev
	}
	img := os.Getenv("APPIMAGE")
	if devBuild || img == "" || !filepath.IsAbs(img) {
		return ""
	}
	fi, err := os.Stat(img)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	// The new one is written beside it and renamed over it.
	probe, err := os.CreateTemp(filepath.Dir(img), ".prem-dl-write-*")
	if err != nil {
		return ""
	}
	probe.Close()
	os.Remove(probe.Name())
	return img
}

// swapAppImage puts the downloaded AppImage in place of the running one and starts it once this
// process has exited (two copies at once would hand over to this one and stop). The running
// copy keeps its old, now unlinked, file until it quits.
func swapAppImage(downloaded, img string) error {
	if err := os.Chmod(downloaded, 0o755); err != nil {
		return err
	}
	if err := os.Rename(downloaded, img); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "-c", `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; exec "$2"`,
		"prem-dl-update", strconv.Itoa(os.Getpid()), img)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
