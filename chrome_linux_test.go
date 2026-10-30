package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yt-dlp-gui/internal/settings"
)

// fakeDesktop is a home with browsers in it, and a PATH holding only fake commands.
func fakeDesktop(t *testing.T, defaultEntry string) string {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	mk := func(rel, data string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []string{"helium", "google-chrome-stable", "flatpak"} {
		mk("bin/"+cmd, "#!/bin/sh\n", 0o755)
	}
	mk("bin/xdg-settings", "#!/bin/sh\necho "+defaultEntry+"\n", 0o755)
	mk(".mozilla/firefox/profiles.ini", "", 0o644)
	helium := ".config/net.imput.helium/"
	mk(helium+"Local State", `{"profile":{"info_cache":{"Default":{"name":"Me"},"Profile 1":{"name":"Work"}}}}`, 0o644)
	mk(helium+"Default/Network/Cookies", "", 0o644)
	mk(helium+"Profile 1/Cookies", "", 0o644)
	mk(helium+"System Profile/Preferences", "", 0o644) // no cookies: not a profile to read
	mk(".local/share/flatpak/exports/share/applications/com.google.Chrome.desktop", "", 0o644)
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	return home
}

func TestDetectBrowsers(t *testing.T) {
	home := fakeDesktop(t, "helium.desktop")
	browsers := detectBrowsers()
	byID := map[string]Browser{}
	var order []string
	for _, b := range browsers {
		byID[b.ID] = b
		order = append(order, b.ID)
	}
	// Installed ones first: Firefox (its profiles), Chrome (its command), Helium, Flatpak Chrome.
	if got := strings.Join(order[:4], " "); got != "firefox chrome helium flatpak:com.google.Chrome" {
		t.Fatalf("order = %v", order)
	}
	if _, ok := byID["brave"]; !ok {
		t.Fatal("browsers that aren't installed should still be offered (yt-dlp may find them)")
	}
	if b := byID["firefox"]; b.Helper {
		t.Fatal("Firefox can't run the helper")
	}
	if b := byID["chrome"]; !b.Helper || b.command[0] != filepath.Join(home, "bin", "google-chrome-stable") || b.sandboxed || b.Root != "" {
		t.Fatalf("chrome = %+v", b)
	}

	h := byID["helium"]
	if h.Family != "chromium" || h.Root != filepath.Join(home, ".config/net.imput.helium") || !h.Helper || !h.Default {
		t.Fatalf("helium = %+v", h)
	}
	var profiles []string
	for _, p := range h.Profiles {
		profiles = append(profiles, filepath.Base(p.Path)+"="+p.Name)
	}
	if got := strings.Join(profiles, ", "); got != "Default=Me, Profile 1=Work" {
		t.Fatalf("profiles = %s", got)
	}

	f := byID["flatpak:com.google.Chrome"]
	if f.Family != "chrome" || !f.sandboxed || strings.Join(f.command, " ") != filepath.Join(home, "bin", "flatpak")+" run com.google.Chrome" {
		t.Fatalf("flatpak chrome = %+v", f)
	}
}

func TestHelperBrowserChoice(t *testing.T) {
	home := fakeDesktop(t, "helium.desktop")
	store, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, dataDir: t.TempDir()}

	b, ok := a.helperBrowser()
	if !ok || b.ID != "helium" {
		t.Fatalf("the default browser should run the helper: %+v", b)
	}
	if dir := a.chromeHelperDir(); dir != filepath.Join(a.dataDir, "chrome-helper") {
		t.Fatalf("helper dir = %s", dir)
	}

	// Chosen in Settings; a Flatpak gets the helper in a folder it can open.
	_, _ = store.Update(func(s *settings.Settings) { s.HelperBrowser = "flatpak:com.google.Chrome" })
	if b, _ := a.helperBrowser(); b.ID != "flatpak:com.google.Chrome" {
		t.Fatalf("chose %s", b.ID)
	}
	if dir := a.chromeHelperDir(); dir != filepath.Join(home, "Prem-dl helper") {
		t.Fatalf("helper dir for a Flatpak = %s", dir)
	}

	// A choice that's gone falls back to the default.
	_, _ = store.Update(func(s *settings.Settings) { s.HelperBrowser = "brave" })
	if b, _ := a.helperBrowser(); b.ID != "helium" {
		t.Fatalf("chose %s", b.ID)
	}

	// A default that isn't Chromium-based: the first that can run it.
	fakeDesktop(t, "firefox.desktop")
	if b, _ := a.helperBrowser(); b.ID != "chrome" {
		t.Fatalf("chose %s", b.ID)
	}
}

func TestNoChromiumBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	store, _ := settings.Open(t.TempDir())
	a := &App{store: store, dataDir: t.TempDir()}
	if b, ok := a.helperBrowser(); ok {
		t.Fatalf("found %+v", b)
	}
	if err := a.OpenChromeExtensions(); err == nil || !strings.Contains(err.Error(), "Chrome-based browser") {
		t.Fatalf("err = %v", err)
	}
}
