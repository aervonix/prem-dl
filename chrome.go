package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yt-dlp-gui/internal/cookiebridge"
	"yt-dlp-gui/internal/sys"
)

// The companion extension that hands YouTube cookies over from Chrome (see internal/cookiebridge).
//
//go:embed chromehelper
var chromeHelper embed.FS

// ChromeHelperInfo says where the helper extension was written for "Load unpacked".
type ChromeHelperInfo struct {
	Path        string `json:"path"`
	ExtensionID string `json:"extensionId"`
	ChromeFound bool   `json:"chromeFound"`
	Browser     string `json:"browser"` // the browser the helper runs in: "Chrome" on Windows
	Error       string `json:"error"`
}

// Browser is a browser installed here, as the Cookies tab offers it: one to read cookies from,
// and maybe one to run the helper in.
type Browser struct {
	ID    string `json:"id"` // chrome, helium, flatpak:com.google.Chrome, …
	Label string `json:"label"`
	// Family is the name yt-dlp knows it by (--cookies-from-browser), which also picks the key
	// that decrypts its cookies.
	Family string `json:"family"`
	// Root is its profiles' folder when yt-dlp can't find it by Family; profiles are then passed
	// as paths. Empty for browsers yt-dlp finds itself.
	Root      string           `json:"root"`
	Profiles  []BrowserProfile `json:"profiles"`  // found under Root
	Installed bool             `json:"installed"` // found here; yt-dlp may still find one that isn't
	Helper    bool             `json:"helper"`    // a Chromium browser the helper can run in
	Default   bool             `json:"default"`   // the system's default browser

	command   []string // what starts it, with a URL appended
	sandboxed bool     // a snap or Flatpak: it can't open hidden folders
}

// BrowserProfile is one profile folder of a browser.
type BrowserProfile struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// helperBrowser is the browser the helper runs in: the one chosen in Settings, else the
// system's default if it is Chromium-based, else the first found.
func (a *App) helperBrowser() (Browser, bool) {
	var first *Browser
	var def *Browser
	want := a.store.Get().HelperBrowser
	browsers := detectBrowsers()
	for i := range browsers {
		b := &browsers[i]
		if !b.Helper {
			continue
		}
		if b.ID == want {
			return *b, true
		}
		if first == nil {
			first = b
		}
		if b.Default && def == nil {
			def = b
		}
	}
	switch {
	case def != nil:
		return *def, true
	case first != nil:
		return *first, true
	}
	return Browser{Label: "Chrome"}, false
}

// open starts the browser on url.
func (b Browser) open(url string) error {
	return startBrowser(exec.Command(b.command[0], append(b.command[1:], url)...))
}

// GetBrowsers lists the browsers installed here.
func (a *App) GetBrowsers() []Browser {
	browsers := detectBrowsers()
	if browsers == nil {
		browsers = []Browser{}
	}
	return browsers
}

func (a *App) chromeHelperDir() string {
	if b, ok := a.helperBrowser(); ok && b.sandboxed {
		if dir := sandboxedHelperDir(); dir != "" {
			return dir
		}
	}
	return filepath.Join(a.dataDir, "chrome-helper")
}

// writeChromeHelper copies the embedded extension to the data folder, replacing an older copy.
func (a *App) writeChromeHelper() (string, error) {
	dir := a.chromeHelperDir()
	err := fs.WalkDir(chromeHelper, "chromehelper", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := chromeHelper.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p[len("chromehelper/"):]))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	return dir, err
}

// ChromeHelperSetup writes the helper extension to the data folder and opens that folder, ready
// for chrome://extensions → Developer mode → Load unpacked.
func (a *App) ChromeHelperSetup() ChromeHelperInfo {
	b, found := a.helperBrowser()
	info := ChromeHelperInfo{ExtensionID: cookiebridge.ExtensionID, ChromeFound: found, Browser: b.Label}
	dir, err := a.writeChromeHelper()
	info.Path = dir
	if err != nil {
		info.Error = err.Error()
		return info
	}
	_ = sys.Open(dir)
	return info
}

// OpenChromeExtensions opens chrome://extensions, which can't be opened as a plain link.
func (a *App) OpenChromeExtensions() error {
	b, ok := a.helperBrowser()
	if !ok {
		return errors.New(strings.ToLower(noChromeMessage[:1]) + noChromeMessage[1:])
	}
	return b.open("chrome://extensions")
}

// GetChromeCookies opens the helper in Chrome, waits for the user to approve, and saves the
// YouTube cookies it sends as the app's cookies.txt. CancelChromeCookies stops the wait.
func (a *App) GetChromeCookies() CookieResponse {
	fail := func(msg string) CookieResponse { return CookieResponse{Settings: a.store.Get(), Error: msg} }
	chrome, ok := a.helperBrowser()
	if !ok {
		return fail(noChromeMessage + ". Export a cookies.txt file instead.")
	}
	name := chrome.Label
	// Keep the helper on disk current, so an older loaded copy picks up changes on reload.
	if _, err := a.writeChromeHelper(); err != nil {
		return fail("Couldn't prepare the Chrome helper: " + err.Error())
	}

	ctx, cancel := context.WithCancel(a.root)
	a.chromeMu.Lock()
	if a.chromeCancel != nil {
		a.chromeMu.Unlock()
		cancel()
		return fail("Already waiting for " + name + ".")
	}
	a.chromeCancel = cancel
	a.chromeMu.Unlock()
	defer func() {
		a.chromeMu.Lock()
		a.chromeCancel = nil
		a.chromeMu.Unlock()
		cancel()
	}()

	session, err := cookiebridge.Listen()
	if err != nil {
		return fail("Couldn't start listening for " + name + ": " + err.Error())
	}
	if err := chrome.open(session.HelperURL()); err != nil {
		return fail("Couldn't open " + name + ": " + err.Error())
	}
	cookies, err := session.Wait(ctx, 20*time.Second, 2*time.Minute)
	switch {
	case errors.Is(err, cookiebridge.ErrNotOpened):
		return fail(name + " didn't show the cookie helper. Set it up first (steps below), in the " + name + " profile you use for YouTube.")
	case errors.Is(err, cookiebridge.ErrDeclined):
		return fail("Nothing was shared.")
	case errors.Is(err, cookiebridge.ErrTimedOut):
		return fail("No answer from " + name + " within 2 minutes.")
	case errors.Is(err, context.Canceled):
		return fail("Cancelled.")
	case err != nil:
		return fail(err.Error())
	}
	data, n := cookiebridge.Netscape(cookies)
	if n == 0 {
		return fail(name + " had no YouTube cookies. Sign in to YouTube in " + name + ", then try again.")
	}
	return a.importCookies(data)
}

// CancelChromeCookies stops a GetChromeCookies that is waiting.
func (a *App) CancelChromeCookies() {
	a.chromeMu.Lock()
	defer a.chromeMu.Unlock()
	if a.chromeCancel != nil {
		a.chromeCancel()
	}
}
