package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const noChromeMessage = "Couldn't find a Chrome-based browser on this computer"

// knownBrowser describes where a browser lives on Linux.
type knownBrowser struct {
	id, label, family string
	commands          []string // names on PATH, the first found wins
	desktop           []string // desktop entry IDs, to recognise the default browser
	dirs              []string // under home: its profiles, where yt-dlp looks for them itself
	root              string   // under home: its profiles, for a browser yt-dlp doesn't know
	chromium          bool
}

// The browsers yt-dlp finds by name, in the order the Cookies tab lists them.
var nativeBrowsers = []knownBrowser{
	{id: "firefox", label: "Firefox", family: "firefox", commands: []string{"firefox"},
		desktop: []string{"firefox.desktop", "firefox_firefox.desktop"},
		dirs:    []string{".mozilla/firefox", "snap/firefox/common/.mozilla/firefox"}},
	{id: "chrome", label: "Chrome", family: "chrome", commands: []string{"google-chrome-stable", "google-chrome"},
		desktop: []string{"google-chrome.desktop"}, dirs: []string{".config/google-chrome"}, chromium: true},
	{id: "chromium", label: "Chromium", family: "chromium", commands: []string{"chromium", "chromium-browser"},
		desktop: []string{"chromium.desktop", "chromium-browser.desktop", "chromium_chromium.desktop"},
		dirs:    []string{".config/chromium", "snap/chromium/common/chromium"}, chromium: true},
	{id: "brave", label: "Brave", family: "brave", commands: []string{"brave-browser", "brave"},
		desktop: []string{"brave-browser.desktop"}, dirs: []string{".config/BraveSoftware/Brave-Browser"}, chromium: true},
	{id: "edge", label: "Edge", family: "edge", commands: []string{"microsoft-edge-stable", "microsoft-edge"},
		desktop: []string{"microsoft-edge.desktop"}, dirs: []string{".config/microsoft-edge"}, chromium: true},
	{id: "opera", label: "Opera", family: "opera", commands: []string{"opera"},
		desktop: []string{"opera.desktop"}, dirs: []string{".config/opera"}, chromium: true},
	{id: "vivaldi", label: "Vivaldi", family: "vivaldi", commands: []string{"vivaldi-stable", "vivaldi"},
		desktop: []string{"vivaldi-stable.desktop"}, dirs: []string{".config/vivaldi"}, chromium: true},
	{id: "whale", label: "Whale", family: "whale", commands: []string{"naver-whale-stable", "whale"},
		desktop: []string{"naver-whale.desktop"}, dirs: []string{".config/naver-whale"}, chromium: true},
}

// Chromium browsers yt-dlp doesn't know by name: read as their family with the profile folder
// as a path. The family picks the keyring entry that decrypts the cookies.
var pathBrowsers = []knownBrowser{
	{id: "helium", label: "Helium", family: "chromium", commands: []string{"helium"},
		desktop: []string{"helium.desktop"}, root: ".config/net.imput.helium", chromium: true},
}

// Flatpak browsers keep their profiles under ~/.var/app/<app>/ and start with flatpak run.
var flatpakBrowsers = []struct{ app, label, family, root string }{
	{"com.google.Chrome", "Chrome (Flatpak)", "chrome", "config/google-chrome"},
	{"org.chromium.Chromium", "Chromium (Flatpak)", "chromium", "config/chromium"},
	{"com.brave.Browser", "Brave (Flatpak)", "brave", "config/BraveSoftware/Brave-Browser"},
	{"com.microsoft.Edge", "Edge (Flatpak)", "edge", "config/microsoft-edge"},
	{"io.github.ungoogled_software.ungoogled_chromium", "Ungoogled Chromium (Flatpak)", "chromium", "config/chromium"},
}

// detectBrowsers lists the browsers yt-dlp can read cookies from: the ones installed here
// first, then the rest of yt-dlp's (it may still find them). Each says whether the helper can
// run in it.
func detectBrowsers() []Browser {
	home, _ := os.UserHomeDir()
	def := defaultBrowserEntry()
	var installed, others []Browser
	for _, k := range nativeBrowsers {
		b := Browser{ID: k.id, Label: k.label, Family: k.family, Default: slices.Contains(k.desktop, def)}
		cmd := lookCommand(k.commands)
		found := cmd != ""
		for _, d := range k.dirs {
			found = found || isDir(filepath.Join(home, d))
		}
		if cmd != "" && k.chromium {
			b.Helper, b.command = true, []string{cmd}
			b.sandboxed = strings.HasPrefix(cmd, "/snap/")
		}
		if found {
			b.Installed = true
			installed = append(installed, b)
		} else {
			others = append(others, b)
		}
	}
	for _, k := range pathBrowsers {
		root := filepath.Join(home, k.root)
		if !isDir(root) {
			continue
		}
		b := Browser{ID: k.id, Label: k.label, Family: k.family, Root: root, Profiles: chromiumProfiles(root),
			Installed: true, Default: slices.Contains(k.desktop, def)}
		if cmd := lookCommand(k.commands); cmd != "" {
			b.Helper, b.command = true, []string{cmd}
		}
		installed = append(installed, b)
	}
	for _, f := range flatpakBrowsers {
		root := filepath.Join(home, ".var", "app", f.app, f.root)
		exported := isFile(filepath.Join("/var/lib/flatpak/exports/share/applications", f.app+".desktop")) ||
			isFile(filepath.Join(home, ".local/share/flatpak/exports/share/applications", f.app+".desktop"))
		if !exported && !isDir(root) {
			continue
		}
		b := Browser{ID: "flatpak:" + f.app, Label: f.label, Family: f.family, Root: root, Profiles: chromiumProfiles(root),
			Installed: true, Default: def == f.app+".desktop"}
		if flatpak, err := exec.LookPath("flatpak"); err == nil && exported {
			b.Helper, b.command, b.sandboxed = true, []string{flatpak, "run", f.app}, true
		}
		installed = append(installed, b)
	}
	return append(installed, others...)
}

func lookCommand(names []string) string {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// defaultBrowserEntry is the default browser's desktop entry, such as helium.desktop.
func defaultBrowserEntry() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xdg-settings", "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// chromiumProfiles lists a Chromium browser's profiles that hold cookies, named as the browser
// names them (Local State), Default first.
func chromiumProfiles(root string) []BrowserProfile {
	var names map[string]string
	if raw, err := os.ReadFile(filepath.Join(root, "Local State")); err == nil {
		var state struct {
			Profile struct {
				InfoCache map[string]struct {
					Name string `json:"name"`
				} `json:"info_cache"`
			} `json:"profile"`
		}
		if json.Unmarshal(raw, &state) == nil {
			names = map[string]string{}
			for dir, p := range state.Profile.InfoCache {
				names[dir] = p.Name
			}
		}
	}
	entries, _ := os.ReadDir(root)
	profiles := []BrowserProfile{}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if !e.IsDir() || !(isFile(filepath.Join(dir, "Cookies")) || isFile(filepath.Join(dir, "Network", "Cookies"))) {
			continue
		}
		name := names[e.Name()]
		if name == "" {
			name = e.Name()
		}
		profiles = append(profiles, BrowserProfile{Path: dir, Name: name})
	}
	slices.SortFunc(profiles, func(a, b BrowserProfile) int {
		if filepath.Base(a.Path) == "Default" {
			return -1
		}
		if filepath.Base(b.Path) == "Default" {
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return profiles
}

// startBrowser starts the browser in a session of its own, so it outlives the app and doesn't get
// the app's terminal signals, and reaps it when it exits (at once, when a running browser takes
// the URL).
func startBrowser(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// sandboxedHelperDir is where the helper goes for a snap or Flatpak browser: those can't open
// hidden folders like the data folder.
func sandboxedHelperDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Prem-dl helper")
}
