package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// ToolStatus describes the external programs the app depends on.
type ToolStatus struct {
	Checking    bool              `json:"checking"`
	Ytdlp       *ytdlp.Binary     `json:"ytdlp"` // nil when no executable was found
	YtdlpError  string            `json:"ytdlpError"`
	FFmpeg      *ytdlp.FFmpeg     `json:"ffmpeg"` // nil when ffmpeg was not found
	JSRuntimes  []ytdlp.JSRuntime `json:"jsRuntimes"`
	ManagedPath string            `json:"managedPath"`
	// FFmpegManaged is set when the ffmpeg in use is the one InstallFFmpeg put in the app's
	// folder; CanInstallFFmpeg when there is a build for this computer.
	FFmpegManaged    bool `json:"ffmpegManaged"`
	CanInstallFFmpeg bool `json:"canInstallFfmpeg"`
}

// UpdateInfo is the result of comparing the installed yt-dlp with the newest release.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Channel   string `json:"channel"`
	Error     string `json:"error"`
}

// UpdateResult is the outcome of installing or updating yt-dlp.
type UpdateResult struct {
	OK       bool             `json:"ok"`
	Previous string           `json:"previous"`
	Version  string           `json:"version"`
	Path     string           `json:"path"`
	Message  string           `json:"message"`
	Error    *apperr.AppError `json:"error"`
}

type versionKey struct {
	path string
	mod  time.Time
	size int64
}

func (a *App) managedDir() string { return a.dataDir }

// detectTools looks for yt-dlp, ffmpeg and JavaScript runtimes and publishes the result.
// Passes can overlap (startup, a settings change, a refresh); only the newest one to finish
// publishes, so a slow pass using old settings can't overwrite a newer result.
func (a *App) detectTools() {
	a.toolsMu.Lock()
	a.toolsGen++
	gen := a.toolsGen
	a.toolsMu.Unlock()
	s := a.store.Get()
	ctx, cancel := context.WithTimeout(a.root, 90*time.Second)
	defer cancel()

	st := ToolStatus{ManagedPath: ytdlp.ManagedPath(a.managedDir()), JSRuntimes: []ytdlp.JSRuntime{}}

	bin, err := ytdlp.Resolve(s.YtdlpPath, a.managedDir())
	if err != nil {
		st.YtdlpError = err.Error()
	} else {
		bin.Version = a.versionOf(ctx, bin.Path)
		st.Ytdlp = &bin
		if bin.Version == "" {
			st.YtdlpError = "yt-dlp was found but didn't run. The file may be damaged. Reinstall it from Settings → Updates."
		}
	}
	ytPath := ""
	if st.Ytdlp != nil {
		ytPath = st.Ytdlp.Path
	}
	if ff := ytdlp.FindFFmpeg(ctx, s.FfmpegPath, ytPath, a.managedDir()); ff.Path != "" {
		st.FFmpeg = &ff
		st.FFmpegManaged = strings.EqualFold(filepath.Dir(ff.Path), ytdlp.FFmpegDir(a.managedDir()))
	}
	st.CanInstallFFmpeg = ytdlp.CanInstallFFmpeg()
	st.JSRuntimes = append(st.JSRuntimes, ytdlp.DetectJSRuntimes(ctx)...)

	a.toolsMu.Lock()
	stale := gen < a.toolsPub
	if !stale {
		a.tools = st
		a.toolsReady = true
		a.toolsPub = gen
	}
	a.toolsMu.Unlock()
	if stale {
		return
	}
	a.firstTools.Do(func() { close(a.toolsFirst) })
	a.emit("tools:status", st)
}

// versionOf asks yt-dlp for its version, caching by file identity because a cold start of the
// packaged executable takes a second or more.
func (a *App) versionOf(ctx context.Context, path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	key := versionKey{path: path, mod: fi.ModTime(), size: fi.Size()}
	a.toolsMu.Lock()
	if v, ok := a.versions[key]; ok {
		a.toolsMu.Unlock()
		return v
	}
	a.toolsMu.Unlock()

	v, err := ytdlp.Version(ctx, path)
	if err != nil {
		return ""
	}
	a.toolsMu.Lock()
	a.versions[key] = v
	a.toolsMu.Unlock()
	return v
}

func (a *App) currentTools() ToolStatus {
	a.toolsMu.Lock()
	defer a.toolsMu.Unlock()
	t := a.tools
	t.Checking = !a.toolsReady
	if t.JSRuntimes == nil {
		t.JSRuntimes = []ytdlp.JSRuntime{}
	}
	if t.ManagedPath == "" {
		t.ManagedPath = ytdlp.ManagedPath(a.managedDir())
	}
	return t
}

// ensureTools waits for the first detection pass (started at startup) rather than running one
// of its own: every download starting before it finished used to spawn a full pass. It gives up
// when ctx ends, so pausing a job that is still starting takes effect at once.
func (a *App) ensureTools(ctx context.Context) ToolStatus {
	a.detectOnce.Do(func() { go a.detectTools() })
	select {
	case <-a.toolsFirst:
	case <-ctx.Done():
	}
	return a.currentTools()
}

var errBinaryMissing = apperr.New("binary_missing", "yt-dlp isn't installed",
	"The app couldn't find a yt-dlp executable to run.",
	"Download it from Settings → Updates, or point the app at an existing yt-dlp.exe.", apperr.ActionInstallBin)

// buildEnv prepares one yt-dlp invocation. The returned cleanup removes per-run temp files.
func (a *App) buildEnv(ctx context.Context) (ytdlp.Env, func(), error) {
	s := a.store.Get()
	bin, err := ytdlp.Resolve(s.YtdlpPath, a.managedDir())
	if err != nil {
		return ytdlp.Env{}, func() {}, errBinaryMissing
	}
	tools := a.ensureTools(ctx)
	if err := ctx.Err(); err != nil {
		return ytdlp.Env{}, func() {}, err
	}
	env := ytdlp.Env{Bin: bin.Path, Settings: s, JSRuntimes: tools.JSRuntimes}
	if tools.FFmpeg != nil {
		env.FFmpeg = tools.FFmpeg.Path
	}
	if s.UseArchive {
		env.ArchiveFile = filepath.Join(a.dataDir, "archive.txt")
	}
	cleanup := func() {}
	if s.Cookies.Mode == "file" {
		info := ytdlp.InspectCookieFile(s.Cookies.File)
		if !info.Valid {
			return ytdlp.Env{}, func() {}, apperr.New("cookie_file", "Cookies file problem", info.Problem,
				"Fix or re-import the file under Settings → Cookies.", apperr.ActionCookies)
		}
		path, done, err := ytdlp.PrivateCookieCopy(s.Cookies.File)
		if err != nil {
			return ytdlp.Env{}, func() {}, apperr.New("cookie_file", "Cookies file problem", err.Error(),
				"Fix or re-import the file under Settings → Cookies.", apperr.ActionCookies)
		}
		env.CookieFile = path
		cleanup = done
	}
	return env, cleanup, nil
}

// ---- updates ----

func (a *App) checkUpdate(ctx context.Context) UpdateInfo {
	s := a.store.Get()
	info := UpdateInfo{Channel: s.UpdateChannel}
	tools := a.ensureTools(ctx)
	if tools.Ytdlp != nil {
		info.Current = tools.Ytdlp.Version
	}
	latest, err := ytdlp.LatestVersion(ctx, ytdlp.HTTPClient(s.Proxy, 20*time.Second), s.UpdateChannel)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Latest = latest
	info.Available = ytdlp.UpdateAvailable(info.Current, latest, s.UpdateChannel)
	_, _ = a.updateSettings(func(v *settings.Settings) { v.LastUpdateCheck = time.Now() })
	return info
}

// installOrUpdate downloads the newest yt-dlp over the current one (or into the managed folder).
func (a *App) installOrUpdate(ctx context.Context) UpdateResult {
	if !a.updating.CompareAndSwap(false, true) {
		return UpdateResult{Error: apperr.New("busy", "Update already running", "An update is already in progress.", "", apperr.ActionNone)}
	}
	defer a.updating.Store(false)

	// Nothing new may start while the binary is swapped: a run starting between the renames
	// would find no yt-dlp, and one holding the old file open would leave it behind.
	n := a.q.Hold()
	defer a.q.Release()
	if n > 0 {
		return UpdateResult{Error: apperr.New("busy", "Downloads are running",
			fmt.Sprintf("%d download(s) are using yt-dlp, which can't be replaced while it runs.", n),
			"Pause or finish the active downloads, then update.", apperr.ActionNone)}
	}

	s := a.store.Get()
	tools := a.ensureTools(ctx)
	res := UpdateResult{}
	dest := ytdlp.ManagedPath(a.managedDir())
	inPlace := false
	if tools.Ytdlp != nil {
		res.Previous = tools.Ytdlp.Version
		// Binaries the user placed (or configured) are updated in place. One found on PATH belongs to
		// a package manager, so a private managed copy is installed instead of touching it.
		if tools.Ytdlp.Source != ytdlp.SourcePath && tools.Ytdlp.Source != "" {
			dest = tools.Ytdlp.Path
			inPlace = true
		}
	}

	client := ytdlp.HTTPClient(s.Proxy, 5*time.Minute)
	progress := func(done, total int64) {
		a.emit("tools:progress", map[string]int64{"done": done, "total": total})
	}
	version, err := ytdlp.Install(ctx, client, s.UpdateChannel, dest, progress)
	if err != nil && inPlace && errors.Is(err, os.ErrPermission) {
		// The folder isn't writable (e.g. Program Files): fall back to the managed copy, which
		// then takes precedence through the explicit path setting.
		dest = ytdlp.ManagedPath(a.managedDir())
		version, err = ytdlp.Install(ctx, client, s.UpdateChannel, dest, progress)
		if err == nil {
			if _, serr := a.updateSettings(func(v *settings.Settings) { v.YtdlpPath = dest }); serr != nil {
				res.Message = "The original location isn't writable, so yt-dlp was installed to the app's own folder, but the setting couldn't be saved (" + serr.Error() + "). Set the yt-dlp path in Advanced settings to " + dest + ". "
			} else {
				res.Message = "The original location isn't writable, so yt-dlp was installed to the app's own folder and will be used from now on. "
			}
		}
	}
	if err != nil {
		res.Error = classifyUpdateError(err)
		return res
	}

	res.OK = true
	res.Version = version
	res.Path = dest
	switch {
	case res.Previous == "":
		res.Message += "yt-dlp " + version + " installed."
	case ytdlp.CompareVersions(res.Previous, version) == 0:
		res.Message += "yt-dlp " + version + " reinstalled (already the newest version)."
	default:
		res.Message += "yt-dlp updated from " + res.Previous + " to " + version + "."
	}
	_, _ = a.updateSettings(func(v *settings.Settings) { v.LastUpdateCheck = time.Now() })
	a.detectTools()
	return res
}

// installFFmpeg downloads yt-dlp's ffmpeg build into the app's folder, or replaces the copy it
// installed before.
func (a *App) installFFmpeg(ctx context.Context) UpdateResult {
	if !a.ffmpegBusy.CompareAndSwap(false, true) {
		return UpdateResult{Error: apperr.New("busy", "Already installing", "ffmpeg is already being installed.", "", apperr.ActionNone)}
	}
	defer a.ffmpegBusy.Store(false)

	res := UpdateResult{}
	dir := ytdlp.FFmpegDir(a.managedDir())
	if _, err := os.Stat(dir); err == nil {
		// Replacing a copy that downloads are using fails on its DLLs, so nothing new may start.
		n := a.q.Hold()
		defer a.q.Release()
		if n > 0 {
			return UpdateResult{Error: apperr.New("busy", "Downloads are running",
				fmt.Sprintf("%d download(s) may be using ffmpeg, which can't be replaced while it runs.", n),
				"Pause or finish the active downloads, then try again.", apperr.ActionNone)}
		}
		if t := a.currentTools(); t.FFmpeg != nil && t.FFmpegManaged {
			res.Previous = t.FFmpeg.Version
		}
	}

	s := a.store.Get()
	progress := func(done, total int64) {
		a.emit("ffmpeg:progress", map[string]int64{"done": done, "total": total})
	}
	version, err := ytdlp.InstallFFmpeg(ctx, ytdlp.HTTPClient(s.Proxy, 15*time.Minute), a.managedDir(), progress)
	if err != nil {
		res.Error = classifyFFmpegError(err)
		return res
	}
	res.OK = true
	res.Version = version
	res.Path = filepath.Join(dir, "ffmpeg.exe")
	if res.Previous != "" {
		res.Message = "ffmpeg updated from " + res.Previous + " to " + version + "."
	} else {
		res.Message = "ffmpeg " + version + " installed."
	}
	if s.FfmpegPath != "" {
		res.Message += " Your ffmpeg location setting still points to another copy; clear it under Settings → Advanced to use this one."
	}
	a.detectTools()
	return res
}

func classifyFFmpegError(err error) *apperr.AppError {
	switch {
	case errors.Is(err, os.ErrPermission):
		return apperr.New("install_permission", "Can't write ffmpeg", err.Error(),
			"Check that the app's data folder is writable and not locked by antivirus.", apperr.ActionNone)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return apperr.New("install_timeout", "Download timed out", "Downloading ffmpeg took too long.", "Check your connection and try again.", apperr.ActionNone)
	}
	return apperr.New("install_failed", "Couldn't install ffmpeg", err.Error(),
		"Check your internet connection (and proxy) and try again.", apperr.ActionNone)
}

func classifyUpdateError(err error) *apperr.AppError {
	switch {
	case errors.Is(err, os.ErrPermission):
		return apperr.New("update_permission", "Can't write yt-dlp", err.Error(),
			"Move yt-dlp.exe to a folder you own, or run the app as administrator once.", apperr.ActionSettings)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return apperr.New("update_timeout", "Update timed out", "Downloading yt-dlp took too long.", "Check your connection and try again.", apperr.ActionNone)
	default:
		ae := apperr.Classify([]string{"ERROR: " + err.Error()}, err)
		if ae.Code == "unknown" {
			ae = apperr.New("update_failed", "Update failed", err.Error(),
				"Check your internet connection (and proxy) and try again.", apperr.ActionNone)
		}
		return ae
	}
}

// autoUpdateLoop checks for a new yt-dlp shortly after launch and every few hours after that.
func (a *App) autoUpdateLoop() {
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-timer.C:
		}
		next := 6 * time.Hour
		if a.autoUpdateOnce() {
			next = 10 * time.Minute
		}
		timer.Reset(next)
	}
}

// autoUpdateOnce updates yt-dlp if a newer release exists. It reports whether it should be
// retried soon because downloads were running and the update had to wait.
func (a *App) autoUpdateOnce() (retrySoon bool) {
	s := a.store.Get()
	if !s.AutoUpdate {
		return false
	}
	tools := a.ensureTools(a.root)
	if tools.Ytdlp == nil {
		return false // first-run install is offered in the UI rather than done silently
	}
	if time.Since(s.LastUpdateCheck) < 5*time.Hour {
		return false
	}
	ctx, cancel := context.WithTimeout(a.root, 30*time.Second)
	info := a.checkUpdate(ctx)
	cancel()
	if info.Error != "" || !info.Available {
		return false
	}
	if a.q.ActiveCount() > 0 {
		a.emit("tools:update-available", info)
		// Forget the check so the retry isn't skipped as "recently checked".
		_, _ = a.updateSettings(func(v *settings.Settings) { v.LastUpdateCheck = time.Time{} })
		return true
	}
	ctx, cancel = context.WithTimeout(a.root, 6*time.Minute)
	defer cancel()
	res := a.installOrUpdate(ctx)
	a.emit("tools:auto-updated", res)
	return false
}
