package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/selfupdate"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/sys"
	"yt-dlp-gui/internal/ytdlp"
)

// Updating Prem-dl itself, from the release server (internal/selfupdate). A release on the
// "stable" channel has one build per target, and each release build knows its own (baked in by
// the release scripts): windows-x64 (Prem-dl-Setup.exe), linux-x64-appimage, linux-x64-deb.
//
//   - A Windows copy the installer put in place downloads the new Prem-dl-Setup.exe, starts it
//     with /update and quits: the installer waits for the app to close, installs over it without
//     asking anything and starts it again (project.nsi).
//   - An AppImage downloads the new one next to itself, swaps it in and starts it again.
//   - Other copies (the portable exe, the .deb, dev builds) only point at the release page.
//
// Release builds carry a build secret, traded on first run for this install's key (kept in the
// data folder as update.key). A copy built from source has none and doesn't check.

// uninstallName is the installer's uninstall entry (UNINST_KEY_NAME in project.nsi).
const uninstallName = "Prem-dl"

// releasePage is where a version's release notes and files are, for copies that can't install.
func releasePage(version string) string {
	return "https://github.com/aervonix/prem-dl/releases/tag/v" + version
}

// AppUpdate is what the last check found, sent as "app:update".
type AppUpdate struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Page      string `json:"page"`  // the release's page
	Notes     string `json:"notes"` // its notes (Markdown)
	Size      int64  `json:"size"`  // the download's size
	// CanInstall: this copy can download the update and install it itself.
	CanInstall bool   `json:"canInstall"`
	Error      string `json:"error"`
}

// errFromSource: this copy has no build secret, so it can't use the release server.
var errFromSource = errors.New("this copy of Prem-dl was built from source, so it doesn't check for updates. Release builds do")

// updater holds this install's connection to the release server.
type updater struct {
	mu     sync.Mutex
	client *selfupdate.Client
}

// updateServer is the release server, build secret and target this copy uses. A dev build can be
// pointed at a local server with PREMDL_UPDATE_API, PREMDL_BUILD_SECRET and PREMDL_BUILD_TARGET,
// with a key file of its own.
func updateServer() (base, secret, target, keyName string) {
	base, secret, target, keyName = selfupdate.APIURL, selfupdate.BuildSecret(), selfupdate.BuildTarget(), "update.key"
	if devBuild {
		if b := os.Getenv("PREMDL_UPDATE_API"); b != "" {
			base, secret, target, keyName = b, os.Getenv("PREMDL_BUILD_SECRET"), os.Getenv("PREMDL_BUILD_TARGET"), "update-dev.key"
		}
	}
	return base, secret, target, keyName
}

// updateTarget is this copy's build target ("" built from source).
func updateTarget() string {
	_, _, target, _ := updateServer()
	return target
}

// updateClient returns a client with this install's key, enrolling first if there is none. A key
// is checked once per start; one the server has dropped (revoked, or unused for 90 days) is
// replaced by enrolling again. Offline, a stored key is used as it is.
func (a *App) updateClient(ctx context.Context) (*selfupdate.Client, error) {
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	if a.upd.client != nil {
		return a.upd.client, nil
	}
	base, secret, target, keyName := updateServer()
	pub, err := selfupdate.PublicKeyOf(selfupdate.ReleasePublicKey)
	if err != nil {
		return nil, err
	}
	hc := ytdlp.HTTPClient(a.store.Get().Proxy, 0)
	keyFile := filepath.Join(a.dataDir, keyName)
	c := &selfupdate.Client{Base: base, App: selfupdate.App, Target: target, PublicKey: pub, HTTP: hc}
	if raw, err := os.ReadFile(keyFile); err == nil && selfupdate.ValidKey(strings.TrimSpace(string(raw))) {
		c.Key = strings.TrimSpace(string(raw))
		ok, err := c.Ping(ctx)
		if err != nil {
			return c, nil // offline or busy: keep the key, and check it next start
		}
		if ok {
			a.upd.client = c
			return c, nil
		}
		_ = os.Remove(keyFile)
	}
	if secret == "" {
		return nil, errFromSource
	}
	key, err := selfupdate.Enrol(ctx, hc, base, secret, runtime.GOOS, appVersion)
	if err != nil {
		return nil, err
	}
	if err := settings.WriteFileAtomic(keyFile, []byte(key+"\n"), 0o600); err != nil {
		return nil, err
	}
	c.Key = key
	a.upd.client = c
	return c, nil
}

// updateDir holds a downloaded installer until the next start removes it.
func (a *App) updateDir() string { return filepath.Join(a.dataDir, "update") }

// installDir is the folder the installer put this exe in, or "" when it didn't.
func (a *App) installDir() string {
	if devBuild {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return sys.InstalledDir(uninstallName, exe)
}

// forThisBuild reports whether r is an update for this kind of build. The server answers with
// this target's build or a build for any platform; the latter names no system, so its file has to
// fit (an .exe is no update for an AppImage).
func forThisBuild(r selfupdate.Release) bool {
	target := updateTarget()
	return target == "" || r.Target == target || selfupdate.FileFits(target, r.FileName)
}

// canInstall reports whether this copy can install r itself: an installed Windows copy, or an
// AppImage it can replace. A .deb needs root, and the portable exe has nothing to run.
func (a *App) canInstall(r selfupdate.Release) bool {
	target := updateTarget()
	if !forThisBuild(r) || !selfupdate.FileFits(target, r.FileName) {
		return false
	}
	switch target {
	case selfupdate.TargetWindows:
		return a.installDir() != ""
	case selfupdate.TargetAppImage:
		return appImagePath() != ""
	}
	return false
}

// CheckAppUpdate looks for a newer Prem-dl release.
func (a *App) CheckAppUpdate() AppUpdate {
	ctx, cancel := context.WithTimeout(a.root, 30*time.Second)
	defer cancel()
	return a.checkAppUpdate(ctx)
}

func (a *App) checkAppUpdate(ctx context.Context) AppUpdate {
	u := AppUpdate{Current: appVersion}
	r, err := a.latestRelease(ctx)
	switch {
	case errors.Is(err, selfupdate.ErrNoRelease):
		// Nothing published for this system yet: up to date.
	case err != nil:
		u.Error = updateMessage(err)
		return u
	default:
		u.Latest, u.Notes, u.Size, u.Page = r.Version, r.Notes, r.Size, releasePage(r.Version)
		u.Available = selfupdate.Newer(appVersion, r.Version) && forThisBuild(r)
		u.CanInstall = u.Available && a.canInstall(r)
	}
	a.appUpdate.Store(&u)
	a.emit("app:update", u)
	return u
}

func (a *App) latestRelease(ctx context.Context) (selfupdate.Release, error) {
	c, err := a.updateClient(ctx)
	if err != nil {
		return selfupdate.Release{}, err
	}
	return c.Latest(ctx, selfupdate.Channel)
}

// updateMessage says what went wrong with a check, for Settings.
func updateMessage(err error) string {
	var off *selfupdate.OfflineError
	switch {
	case errors.As(err, &off):
		return "couldn't reach the update server. Check your connection (and proxy) and try again."
	case errors.Is(err, selfupdate.ErrNotAccepted):
		return "the update server no longer accepts this copy. Download the newest Prem-dl from its release page."
	}
	return err.Error()
}

// appUpdateLoop checks for a new Prem-dl shortly after launch and twice a day after that.
// Failures stay quiet here (offline, a source build): Settings shows them when asked.
func (a *App) appUpdateLoop() {
	// The installer the last update ran from; it has finished by now, or the next start gets it.
	_ = os.RemoveAll(a.updateDir())
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-timer.C:
		}
		if a.store.Get().CheckAppUpdates {
			ctx, cancel := context.WithTimeout(a.root, 30*time.Second)
			a.checkAppUpdate(ctx)
			cancel()
		}
		timer.Reset(12 * time.Hour)
	}
}

// InstallAppUpdate downloads the newest release, checks it against its signed manifest, and
// installs it: Windows runs the installer and quits (it starts the new version when it's done),
// an AppImage swaps itself and starts again. Downloads must not be running, since quitting
// stops them.
func (a *App) InstallAppUpdate() UpdateResult {
	if !a.selfUpdating.CompareAndSwap(false, true) {
		return UpdateResult{Error: apperr.New("busy", "Update already running", "Prem-dl is already being updated.", "", apperr.ActionNone)}
	}
	quitting := false
	defer func() {
		if !quitting {
			a.selfUpdating.Store(false)
		}
	}()
	if busy := a.selfUpdateBusy(); busy != nil {
		return UpdateResult{Error: busy}
	}

	ctx, cancel := context.WithTimeout(a.root, 30*time.Minute)
	defer cancel()
	r, err := a.latestRelease(ctx)
	if errors.Is(err, selfupdate.ErrNoRelease) {
		err = errors.New("no newer Prem-dl has been published for this system")
	}
	if err != nil {
		return UpdateResult{Error: selfUpdateError(err)}
	}
	res := UpdateResult{Previous: appVersion, Version: r.Version}
	if !selfupdate.Newer(appVersion, r.Version) {
		res.Error = apperr.New("up_to_date", "Already up to date", "Prem-dl "+appVersion+" is the newest version.", "", apperr.ActionNone)
		return res
	}
	if !a.canInstall(r) {
		res.Error = apperr.New("not_installed", "Can't update this copy",
			"This copy of Prem-dl can't replace itself (only the installed Windows version and the AppImage can).",
			"Download the new version from its release page.", apperr.ActionNone)
		return res
	}
	c, err := a.updateClient(ctx)
	if err != nil {
		res.Error = selfUpdateError(err)
		return res
	}
	dest, err := a.updateDest(r)
	if err != nil {
		res.Error = selfUpdateError(err)
		return res
	}
	if err := c.Download(ctx, r, dest, func(done, total int64) {
		a.emit("app:update-progress", map[string]int64{"done": done, "total": total})
	}); err != nil {
		res.Error = selfUpdateError(err)
		return res
	}
	res.Path = dest

	// Downloads may have started meanwhile. Nothing new starts from here on; the hold is only
	// released if the update can't go ahead.
	if busy := a.selfUpdateBusy(); busy != nil {
		res.Error = busy
		return res
	}
	if n := a.q.Hold(); n > 0 {
		a.q.Release()
		res.Error = downloadsRunning(n)
		return res
	}
	if err := a.startUpdate(dest); err != nil {
		a.q.Release()
		res.Error = apperr.New("update_failed", "Couldn't install the update", err.Error(),
			"Download the new version from its release page.", apperr.ActionNone)
		return res
	}
	quitting = true
	res.OK = true
	res.Message = "Installing Prem-dl " + r.Version + ". It starts again when it's done."
	// Give the page a moment to show that before the window goes.
	go func() {
		time.Sleep(700 * time.Millisecond)
		if ctx := a.rt(); ctx != nil {
			a.quitting.Store(true)
			wr.Quit(ctx)
		}
	}()
	return res
}

// updateDest is where the update downloads to: the installer in the update folder, or a new
// AppImage beside the running one (the swap is a rename, so it must be on the same drive).
func (a *App) updateDest(r selfupdate.Release) (string, error) {
	if runtime.GOOS == "linux" {
		img := appImagePath()
		return filepath.Join(filepath.Dir(img), ".Prem-dl-"+r.Version+".AppImage"), nil
	}
	if err := os.MkdirAll(a.updateDir(), 0o700); err != nil {
		return "", err
	}
	return filepath.Join(a.updateDir(), "Prem-dl-Setup-"+r.Version+".exe"), nil
}

// startUpdate hands over to the downloaded update; the app quits after it.
func (a *App) startUpdate(path string) error {
	if runtime.GOOS == "linux" {
		return swapAppImage(path, appImagePath())
	}
	// /D= must come last and stay unquoted, even with spaces in it.
	return sys.StartCommandLine(path, `"`+path+`" /update /D=`+a.installDir())
}

// selfUpdateBusy says why Prem-dl can't quit for an update right now, or returns nil.
func (a *App) selfUpdateBusy() *apperr.AppError {
	if n := a.q.ActiveCount(); n > 0 {
		return downloadsRunning(n)
	}
	if a.repairing.Load() {
		return apperr.New("busy", "Tools are being repaired", "Prem-dl is unpacking its download tools again.",
			"Wait for it to finish, then update Prem-dl.", apperr.ActionNone)
	}
	return nil
}

func downloadsRunning(n int) *apperr.AppError {
	return apperr.New("busy", "Downloads are running",
		fmt.Sprintf("%d download(s) are running, and updating restarts Prem-dl.", n),
		"Pause or finish them, then update.", apperr.ActionNone)
}

func selfUpdateError(err error) *apperr.AppError {
	var off *selfupdate.OfflineError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return apperr.New("update_timeout", "Update timed out", "Downloading the update took too long.", "Check your connection and try again; it carries on where it stopped.", apperr.ActionNone)
	case errors.Is(err, os.ErrPermission):
		return apperr.New("update_permission", "Can't save the update", err.Error(),
			"Check that the folder is writable and not locked by antivirus.", apperr.ActionNone)
	case errors.As(err, &off):
		return apperr.New("update_offline", "Couldn't reach the update server", err.Error(),
			"Check your connection (and proxy) and try again; a download carries on where it stopped.", apperr.ActionNone)
	case errors.Is(err, selfupdate.ErrVerify):
		return apperr.New("update_unverified", "Update refused", err.Error(),
			"Nothing was installed. Try again later, or download Prem-dl from its release page.", apperr.ActionNone)
	}
	return apperr.New("update_failed", "Couldn't update Prem-dl", updateMessage(err), "Try again in a while.", apperr.ActionNone)
}
