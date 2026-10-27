package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/selfupdate"
	"yt-dlp-gui/internal/sys"
	"yt-dlp-gui/internal/ytdlp"
)

// Updating Prem-dl itself. A copy the installer put in place updates by downloading the new
// Prem-dl-Setup.exe, starting it with /update and quitting: the installer waits for the app to
// close, installs over it without asking anything and starts it again (project.nsi). Other
// copies (the portable exe, dev builds) only point at the release page.

// uninstallName is the installer's uninstall entry (UNINST_KEY_NAME in project.nsi).
const uninstallName = "Prem-dl"

// AppUpdate is what the last check found, sent as "app:update".
type AppUpdate struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Page      string `json:"page"`  // the release on GitHub
	Notes     string `json:"notes"` // its description (Markdown)
	Size      int64  `json:"size"`  // the installer's size
	// CanInstall: this copy came from the installer, so the update can install itself.
	CanInstall bool   `json:"canInstall"`
	Error      string `json:"error"`
}

func init() {
	if base := os.Getenv("PREMDL_UPDATE_API"); devBuild && base != "" {
		selfupdate.UseAPI(base)
	}
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

// CheckAppUpdate looks for a newer Prem-dl release.
func (a *App) CheckAppUpdate() AppUpdate {
	ctx, cancel := context.WithTimeout(a.root, 30*time.Second)
	defer cancel()
	return a.checkAppUpdate(ctx)
}

func (a *App) checkAppUpdate(ctx context.Context) AppUpdate {
	u := AppUpdate{Current: appVersion}
	r, err := selfupdate.Latest(ctx, ytdlp.HTTPClient(a.store.Get().Proxy, 20*time.Second))
	if err != nil {
		u.Error = err.Error()
		return u
	}
	u.Latest, u.Page, u.Notes, u.Size = r.Version, r.Page, r.Notes, r.Size
	u.Available = selfupdate.Newer(appVersion, r.Version)
	u.CanInstall = u.Available && r.SetupURL != "" && r.SumsURL != "" && a.installDir() != ""
	a.appUpdate.Store(&u)
	a.emit("app:update", u)
	return u
}

// appUpdateLoop checks for a new Prem-dl shortly after launch and twice a day after that.
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

// InstallAppUpdate downloads the newest installer, starts it and quits; the installer starts
// the new version when it's done. Downloads must not be running, since quitting stops them.
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

	dir := a.installDir()
	if dir == "" {
		return UpdateResult{Error: apperr.New("not_installed", "Can't update this copy",
			"This copy of Prem-dl wasn't installed with Prem-dl-Setup.exe, so it can't replace itself.",
			"Download the new version from its release page.", apperr.ActionNone)}
	}
	if busy := a.selfUpdateBusy(); busy != nil {
		return UpdateResult{Error: busy}
	}

	ctx, cancel := context.WithTimeout(a.root, 20*time.Minute)
	defer cancel()
	client := ytdlp.HTTPClient(a.store.Get().Proxy, 20*time.Minute)
	r, err := selfupdate.Latest(ctx, client)
	if err != nil {
		return UpdateResult{Error: selfUpdateError(err)}
	}
	res := UpdateResult{Previous: appVersion, Version: r.Version}
	if !selfupdate.Newer(appVersion, r.Version) {
		res.Error = apperr.New("up_to_date", "Already up to date", "Prem-dl "+appVersion+" is the newest version.", "", apperr.ActionNone)
		return res
	}
	path, err := selfupdate.Download(ctx, client, r, a.updateDir(), func(done, total int64) {
		a.emit("app:update-progress", map[string]int64{"done": done, "total": total})
	})
	if err != nil {
		res.Error = selfUpdateError(err)
		return res
	}
	res.Path = path

	// Downloads may have started while the installer was downloading. Nothing new starts from
	// here on; the hold is only released if the update can't go ahead.
	if busy := a.selfUpdateBusy(); busy != nil {
		res.Error = busy
		return res
	}
	if n := a.q.Hold(); n > 0 {
		a.q.Release()
		res.Error = downloadsRunning(n)
		return res
	}
	// /D= must come last and stay unquoted, even with spaces in it.
	if err := sys.StartCommandLine(path, `"`+path+`" /update /D=`+dir); err != nil {
		a.q.Release()
		res.Error = apperr.New("update_failed", "Couldn't start the installer", err.Error(),
			"Run "+path+" yourself to install the update.", apperr.ActionNone)
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
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return apperr.New("update_timeout", "Update timed out", "Downloading the update took too long.", "Check your connection and try again.", apperr.ActionNone)
	case errors.Is(err, os.ErrPermission):
		return apperr.New("update_permission", "Can't save the update", err.Error(),
			"Check that the app's data folder is writable and not locked by antivirus.", apperr.ActionNone)
	}
	return apperr.New("update_failed", "Couldn't update Prem-dl", err.Error(),
		"Check your internet connection (and proxy) and try again.", apperr.ActionNone)
}
