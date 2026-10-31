package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/ytdlp"
)

// ToolStatus describes the external programs the app depends on.
type ToolStatus struct {
	Checking   bool              `json:"checking"`
	Ytdlp      *ytdlp.Binary     `json:"ytdlp"` // nil when no executable was found
	YtdlpError string            `json:"ytdlpError"`
	FFmpeg     *ytdlp.FFmpeg     `json:"ffmpeg"` // nil when ffmpeg was not found
	JSRuntimes []ytdlp.JSRuntime `json:"jsRuntimes"`
	// BuiltIn is set when this build carries its own yt-dlp and ffmpeg and runs only those.
	BuiltIn bool `json:"builtIn"`
}

// UpdateResult is the outcome of updating Prem-dl or repairing its tools.
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
	ctx, cancel := context.WithTimeout(a.root, 90*time.Second)
	defer cancel()
	// Unpacking ffmpeg takes a few seconds; it must not eat into detection's own time.
	a.unpackOnce.Do(func() {
		if err := a.unpackBundle(a.root, false); err != nil {
			a.warn("couldn't unpack the built-in tools: " + err.Error())
		}
	})

	st := ToolStatus{JSRuntimes: []ytdlp.JSRuntime{}, BuiltIn: bundleFS != nil}

	bin, err := a.findYtdlp()
	ytPath := ""
	var version chan string
	if err != nil {
		st.YtdlpError = errToolsMissing.Message
	} else {
		st.Ytdlp = &bin
		ytPath = bin.Path
		// Starting yt-dlp takes 0.3 s or more; ffmpeg and the JS runtimes are looked at meanwhile.
		version = make(chan string, 1)
		go func() { version <- a.versionOf(ctx, bin.Path) }()
	}
	var wg sync.WaitGroup
	var ff ytdlp.FFmpeg
	wg.Add(1)
	go func() {
		defer wg.Done()
		ff = a.findFFmpeg(ctx, ytPath)
	}()
	js := ytdlp.DetectJSRuntimes(ctx)
	wg.Wait()
	if ff.Path != "" {
		st.FFmpeg = &ff
	}
	st.JSRuntimes = append(st.JSRuntimes, js...)

	if version == nil {
		a.publishTools(gen, st)
		return
	}
	// Downloads need only the paths, so they start now; yt-dlp's version (whether it runs at
	// all) follows, for Settings and the "didn't start" banner.
	select {
	case v := <-version:
		st.Ytdlp.Version = v
	default:
		a.publishTools(gen, st)
		v := <-version
		st = cloneTools(st)
		st.Ytdlp.Version = v
	}
	if st.Ytdlp.Version == "" {
		st.YtdlpError = "Prem-dl's download tools didn't start. The files may be damaged or blocked by antivirus."
	}
	a.publishTools(gen, st)
}

// cloneTools copies st so a published status isn't changed afterwards.
func cloneTools(st ToolStatus) ToolStatus {
	if st.Ytdlp != nil {
		b := *st.Ytdlp
		st.Ytdlp = &b
	}
	st.JSRuntimes = append([]ytdlp.JSRuntime{}, st.JSRuntimes...)
	return st
}

// publishTools makes st the current status, unless a newer pass has published already.
func (a *App) publishTools(gen int64, st ToolStatus) {
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

var errToolsMissing = apperr.New("binary_missing", "Download tools are missing",
	"Prem-dl's download tools are missing from its folder.",
	"Repair them from Settings → Updates. If that doesn't help, install Prem-dl again.", apperr.ActionRepair)

// findYtdlp locates the yt-dlp to run. A build with the tools built in runs only its own copy,
// so yt-dlp changes only with a Prem-dl release. Other builds (wails dev, a plain wails build)
// look next to the app, in the working folder, in the app's folder, then on PATH.
func (a *App) findYtdlp() (ytdlp.Binary, error) {
	if bundleFS == nil {
		return ytdlp.Resolve("", a.managedDir())
	}
	p := ytdlp.ManagedPath(a.managedDir())
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ytdlp.Binary{}, ytdlp.ErrNotFound
	}
	return ytdlp.Binary{Path: p, Source: ytdlp.SourceManaged}, nil
}

// findFFmpeg is findYtdlp for ffmpeg. A bundle built without ffmpeg looks for one like other builds.
func (a *App) findFFmpeg(ctx context.Context, ytdlpPath string) ytdlp.FFmpeg {
	if bundleHas("ffmpeg.zip") {
		return ytdlp.FFmpegAt(ctx, filepath.Join(ytdlp.FFmpegDir(a.managedDir()), ytdlp.FFmpegName()))
	}
	return ytdlp.FindFFmpeg(ctx, "", ytdlpPath, a.managedDir())
}

// buildEnv prepares one yt-dlp invocation. The returned cleanup removes per-run temp files.
func (a *App) buildEnv(ctx context.Context) (ytdlp.Env, func(), error) {
	// Detection first: the yt-dlp built into the app is unpacked by then.
	tools := a.ensureTools(ctx)
	if err := ctx.Err(); err != nil {
		return ytdlp.Env{}, func() {}, err
	}
	s := a.store.Get()
	bin, err := a.findYtdlp()
	if err != nil {
		return ytdlp.Env{}, func() {}, errToolsMissing
	}
	env := ytdlp.Env{Bin: bin.Path, Settings: s, JSRuntimes: tools.JSRuntimes}
	env.PluginDir, env.Vars = a.challengeSolver(bin.Path, tools.JSRuntimes)
	if tools.FFmpeg != nil {
		env.FFmpeg = tools.FFmpeg.Path
	}
	if s.UseArchive {
		env.ArchiveFile = a.archivePath()
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

// ---- repair ----

// repairTools unpacks the built-in tools again, over whatever the app's folder holds, and
// re-detects them. Builds without built-in tools only re-detect.
func (a *App) repairTools(ctx context.Context) UpdateResult {
	if !a.repairing.CompareAndSwap(false, true) {
		return UpdateResult{Error: apperr.New("busy", "Already repairing", "The tools are already being repaired.", "", apperr.ActionNone)}
	}
	defer a.repairing.Store(false)
	a.ensureTools(ctx) // the first unpack is done by then

	if bundleFS != nil {
		// Nothing new may start while the programs are swapped, and running ones hold them open.
		n := a.q.Hold()
		defer a.q.Release()
		if n > 0 {
			return UpdateResult{Error: apperr.New("busy", "Downloads are running",
				fmt.Sprintf("%d download(s) are using the tools, which can't be replaced while they run.", n),
				"Pause or finish the active downloads, then try again.", apperr.ActionNone)}
		}
		if err := a.unpackBundle(ctx, true); err != nil {
			hint := "Check that the app's data folder is writable and not locked by antivirus, or install Prem-dl again."
			return UpdateResult{Error: apperr.New("repair_failed", "Couldn't repair the tools", err.Error(), hint, apperr.ActionNone)}
		}
	}
	a.detectTools()
	if t := a.currentTools(); t.YtdlpError != "" {
		return UpdateResult{Error: apperr.New("repair_failed", "The tools still don't work", t.YtdlpError,
			"Install Prem-dl again from its latest release.", apperr.ActionNone)}
	}
	return UpdateResult{OK: true, Message: "Prem-dl's download tools are ready."}
}
