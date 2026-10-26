package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/follow"
	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/sys"
	"yt-dlp-gui/internal/ytdlp"
)

const appVersion = "0.1.2"

// App is bound to the frontend: every exported method is callable from TypeScript.
type App struct {
	ctxPtr  atomic.Pointer[context.Context] // set once in startup, read from any goroutine
	dataDir string
	store   *settings.Store
	q       *queue.Manager
	feeds   *follow.Manager // followed channels and playlists; see following.go
	quit    chan struct{}
	// root is cancelled at shutdown so probes, bulk adds, cookie tests and updates stop with the
	// app instead of leaving yt-dlp processes behind.
	root    context.Context
	stopAll context.CancelFunc

	toolsMu    sync.Mutex
	tools      ToolStatus
	toolsReady bool
	toolsGen   int64 // detection passes started
	toolsPub   int64 // the pass whose result is published
	versions   map[versionKey]string
	detectOnce sync.Once
	unpackOnce sync.Once // the tools built into the exe; see bundle.go
	firstTools sync.Once
	toolsFirst chan struct{} // closed once the first detection pass has published

	updating   atomic.Bool
	ffmpegBusy atomic.Bool

	// Prem-dl's own updates; see app_update.go.
	appUpdate    atomic.Pointer[AppUpdate] // the last successful check
	selfUpdating atomic.Bool

	probeMu     sync.Mutex
	probeCancel context.CancelFunc
	// jobProbe is ProbeJob's, apart from probeCancel so the Download page's analysis carries on.
	jobProbe context.CancelFunc

	browseMu     sync.Mutex
	browseCancel context.CancelFunc // set while BrowseFeed lists a channel

	bulkMu     sync.Mutex
	bulkCancel context.CancelFunc // set while EnqueueLinks runs

	// Wails' Windows clipboard calls aren't safe to overlap: a second OpenClipboard fails, so
	// concurrent reads came back empty.
	clipMu sync.Mutex

	// The close dialog's state; see beforeClose.
	closeMu                                sync.Mutex
	closeAsked, closeShown, closeConfirmed bool

	chromeMu     sync.Mutex
	chromeCancel context.CancelFunc // set while GetChromeCookies waits for the user

	// Running in the background; see background.go.
	launch   launchArgs
	act      *activity
	shell    atomic.Pointer[sys.Shell] // nil until started, or if there's no tray
	hidden   atomic.Bool               // the window is hidden, with the tray icon to bring it back
	quitting atomic.Bool               // Quit from the tray: closing quits instead of hiding
	toasts   atomic.Bool               // Windows notifications are set up
}

// NewApp loads settings and the saved queue. If the normal config folder can't be used it falls
// back to a temp folder so the app still starts.
func NewApp(launch launchArgs) *App {
	dir, err := settings.DefaultDir()
	var store *settings.Store
	if err == nil {
		store, err = settings.Open(dir)
	}
	if err != nil {
		dir = filepath.Join(os.TempDir(), settings.ConfigDirName)
		store, _ = settings.Open(dir)
	}
	a := &App{
		dataDir:    dir,
		store:      store,
		quit:       make(chan struct{}),
		versions:   map[versionKey]string{},
		toolsFirst: make(chan struct{}),
		launch:     launch,
	}
	a.root, a.stopAll = context.WithCancel(context.Background())
	a.q = queue.New(queue.Deps{
		Settings:  store.Get,
		Env:       a.buildEnv,
		Emit:      a.emit,
		Observe:   func(changed []queue.Job, removed []string) { a.act.observe(changed, removed) },
		StatePath: filepath.Join(dir, "queue.json"),
	})
	a.feeds = a.newFeeds()
	a.act = newActivity(a.q.List(), 1200*time.Millisecond, a.updateShell, a.notifyFinished)
	a.hidden.Store(launch.Background)
	return a
}

// rt returns the Wails runtime context, or nil before startup.
func (a *App) rt() context.Context {
	if p := a.ctxPtr.Load(); p != nil {
		return *p
	}
	return nil
}

func (a *App) emit(event string, payload any) {
	if ctx := a.rt(); ctx != nil {
		wr.EventsEmit(ctx, event, payload)
	}
}

// updateSettings saves a change the backend makes on its own and sends the result to the UI,
// whose copy would otherwise be stale and undo the change on the next save.
func (a *App) updateSettings(fn func(*settings.Settings)) (settings.Settings, error) {
	saved, err := a.store.Update(fn)
	if err == nil {
		a.emit("settings:changed", saved)
	}
	return saved, err
}

func (a *App) startup(ctx context.Context) {
	a.ctxPtr.Store(&ctx)
	// The window exists but isn't shown yet: move it to where it was last closed.
	if p, ok := a.savedWindow(); ok {
		sys.RestoreMainWindow(p)
	}
	a.q.Start()
	a.feeds.Start()
	a.applyTheme(a.store.Get().Theme)
	a.detectOnce.Do(func() { go a.detectTools() })
	go a.autoUpdateLoop()
	go a.appUpdateLoop()
	go a.startBackground(ctx)
}

// CloseRequest is sent as "app:confirm-close" when closing needs the user's say-so.
type CloseRequest struct {
	Downloads  int  `json:"downloads"`  // running downloads that would be paused
	Updating   bool `json:"updating"`   // yt-dlp is being replaced
	Background bool `json:"background"` // the window can hide in the tray instead (HideWindow)
}

// beforeClose asks for confirmation when downloads are still running (or yt-dlp is updating),
// and remembers where the window was when it does close. When the app is set to keep running in
// the tray, closing the window only hides it; the tray icon's Quit closes for real.
//
// The question is the app's own dialog: the window stays open, "app:confirm-close" goes to the
// page, and its buttons call ConfirmClose or CancelClose. A page that never confirms it showed
// the dialog (it failed to load) mustn't make the app impossible to close, so the next close
// asks with the system dialog instead.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	defer func() {
		// A hidden window's placement was saved when it was hidden.
		if !prevent && !a.hidden.Load() {
			a.saveWindow()
		}
	}()
	a.closeMu.Lock()
	confirmed := a.closeConfirmed
	a.closeMu.Unlock()
	if confirmed {
		return false
	}
	if !a.quitting.Load() && a.store.Get().CloseToTray && a.hideToTray() {
		return true
	}
	n := a.q.ActiveCount()
	updating := a.updating.Load()
	if n == 0 && !updating {
		return false
	}
	a.closeMu.Lock()
	unseen := a.closeAsked && !a.closeShown
	a.closeAsked, a.closeShown = true, false
	a.closeMu.Unlock()
	if !unseen {
		a.emit("app:confirm-close", CloseRequest{Downloads: n, Updating: updating, Background: a.shell.Load() != nil})
		return true
	}

	opts := wr.MessageDialogOptions{
		Type:          wr.QuestionDialog,
		Title:         "Downloads in progress",
		Message:       fmt.Sprintf("%d download(s) are still running. Quit anyway?\n\nThey'll be paused and you can resume them next time.", n),
		Buttons:       []string{"Quit", "Keep downloading"},
		DefaultButton: "Keep downloading",
		CancelButton:  "Keep downloading",
	}
	switch {
	case n > 0:
	case a.updating.Load():
		opts.Title = "Updating yt-dlp"
		opts.Message = "yt-dlp is being updated. Quit anyway?\n\nThe update will be abandoned and the current version kept."
		opts.Buttons = []string{"Quit", "Wait"}
		opts.DefaultButton, opts.CancelButton = "Wait", "Wait"
	default:
		return false
	}
	choice, err := wr.MessageDialog(ctx, opts)
	if err != nil {
		return false
	}
	return choice != "Quit"
}

// CloseShown tells beforeClose that the page is showing the close dialog.
func (a *App) CloseShown() {
	a.closeMu.Lock()
	a.closeShown = true
	a.closeMu.Unlock()
}

// CancelClose is the close dialog's "Keep downloading".
func (a *App) CancelClose() {
	a.closeMu.Lock()
	a.closeAsked, a.closeShown = false, false
	a.closeMu.Unlock()
	a.quitting.Store(false)
}

// ConfirmClose is the close dialog's "Quit": the app closes without asking again.
func (a *App) ConfirmClose() {
	a.closeMu.Lock()
	a.closeConfirmed = true
	a.closeMu.Unlock()
	if a.ready() {
		wr.Quit(a.rt())
	}
}

func (a *App) shutdown(ctx context.Context) {
	if sh := a.shell.Load(); sh != nil {
		sh.Close() // or the icon lingers in the tray until the pointer passes over it
	}
	a.stopAll()
	close(a.quit)
	a.cancelProbe()
	a.feeds.Shutdown() // before the queue, so a check can't add to it while it shuts down
	a.q.Shutdown()
}

// onSecondInstance gets the command line of a second launch: the user starting the app again, a
// prem-dl: link, or a link passed to the exe. It brings this window forward instead.
func (a *App) onSecondInstance(data options.SecondInstanceData) {
	if a.rt() == nil {
		return
	}
	l := parseLaunch(data.Args)
	if l.Background && l.Link == "" {
		return // the sign-in entry, while the app is already running
	}
	a.showWindow()
	if l.Link != "" {
		a.emit("app:open-url", l.Link)
	}
}

func (a *App) applyTheme(theme string) {
	ctx := a.rt()
	if ctx == nil {
		return
	}
	switch theme {
	case "light":
		wr.WindowSetLightTheme(ctx)
	case "dark":
		wr.WindowSetDarkTheme(ctx)
	default:
		wr.WindowSetSystemDefaultTheme(ctx)
	}
}

// ---- state ----

// AppState is everything the UI needs on first load.
type AppState struct {
	Version  string            `json:"version"`
	Settings settings.Settings `json:"settings"`
	Jobs     []queue.Job       `json:"jobs"`
	Feeds    []follow.Feed     `json:"feeds"`
	Tools    ToolStatus        `json:"tools"`
	DataDir  string            `json:"dataDir"`
	OpenLink string            `json:"openLink"` // a link the app was started with, for the Download page
	// AppUpdate is the last successful check for a new Prem-dl, nil until one has run.
	AppUpdate *AppUpdate `json:"appUpdate"`
}

// GetState returns settings, the queue and tool status in one call.
func (a *App) GetState() AppState {
	return AppState{
		Version:  appVersion,
		Settings: a.store.Get(),
		Jobs:     a.q.List(),
		Feeds:    a.feeds.List(),
		Tools:    a.currentTools(),
		DataDir:  a.dataDir,
		OpenLink: a.launch.Link,
		// The check runs after launch, so a page reloaded later still learns what it found.
		AppUpdate: a.appUpdate.Load(),
	}
}

// SettingsResponse carries either the saved settings or a message for the user.
type SettingsResponse struct {
	Settings settings.Settings `json:"settings"`
	Error    string            `json:"error"`
}

// SaveSettings validates and persists settings.
func (a *App) SaveSettings(s settings.Settings) SettingsResponse {
	if _, err := ytdlp.SplitArgs(s.ExtraArgs); err != nil {
		return SettingsResponse{Settings: a.store.Get(), Error: err.Error()}
	}
	prev := a.store.Get()
	// Not user-editable; a draft opened before a check must not roll them back.
	s.LastUpdateCheck = prev.LastUpdateCheck
	s.LatestYtdlp, s.LatestYtdlpChannel = prev.LatestYtdlp, prev.LatestYtdlpChannel
	// The sign-in entry is changed first, so a failure leaves the setting as it was.
	startChanged := s.StartWithWindows != prev.StartWithWindows
	if startChanged {
		if err := setStartWithWindows(s.StartWithWindows); err != nil {
			return SettingsResponse{Settings: prev, Error: "Couldn't change starting with Windows: " + err.Error()}
		}
	}
	saved, err := a.store.Set(s)
	if err != nil {
		if startChanged {
			_ = setStartWithWindows(prev.StartWithWindows)
		}
		return SettingsResponse{Settings: prev, Error: err.Error()}
	}
	if saved.CloseToTray != prev.CloseToTray {
		a.updateShell(a.act.current())
	}
	if saved.FollowEvery != prev.FollowEvery {
		a.feeds.Kick()
	}
	if saved.UpdateChannel != prev.UpdateChannel {
		a.republishUpToDate() // the last check was for the old channel
	}
	a.applyTheme(saved.Theme)
	a.q.Kick() // a changed concurrency limit takes effect immediately
	if saved.YtdlpPath != prev.YtdlpPath || saved.FfmpegPath != prev.FfmpegPath {
		go a.detectTools()
	}
	return SettingsResponse{Settings: saved}
}

// RefreshTools re-detects yt-dlp, ffmpeg and JavaScript runtimes.
func (a *App) RefreshTools() ToolStatus {
	a.detectTools()
	return a.currentTools()
}

// ---- analysing links ----

// ProbeResponse is the outcome of analysing a link.
type ProbeResponse struct {
	Result    *ytdlp.ProbeResult `json:"result"`
	Error     *apperr.AppError   `json:"error"`
	Cancelled bool               `json:"cancelled"`
}

// Probe reads a link's metadata (single video, or a playlist's entries) without downloading.
// Starting a new probe cancels the previous one.
func (a *App) Probe(link string, noPlaylist bool) ProbeResponse {
	return a.probe(&a.probeCancel, link, noPlaylist)
}

// ProbeJob reads a queued download's video (its formats, subtitles...) for changing its options.
// Starting another cancels the previous one; the Download page's analysis is left alone.
func (a *App) ProbeJob(id string) ProbeResponse {
	j, ok := a.q.Get(id)
	if !ok {
		return ProbeResponse{Error: apperr.New("no_job", "Download not found", "That download is no longer in the queue.", "", apperr.ActionNone)}
	}
	return a.probe(&a.jobProbe, j.URL, true)
}

// probe runs one analysis, cancelling the one before it in the same slot.
func (a *App) probe(slot *context.CancelFunc, link string, noPlaylist bool) ProbeResponse {
	ctx, cancel := context.WithCancel(a.root)
	a.probeMu.Lock()
	if *slot != nil {
		(*slot)()
	}
	*slot = cancel
	a.probeMu.Unlock()
	defer cancel()

	env, cleanup, err := a.buildEnv(ctx)
	if err != nil {
		return ProbeResponse{Error: toAppError(err)}
	}
	defer cleanup()

	res, err := ytdlp.Probe(ctx, env, link, noPlaylist)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ProbeResponse{Cancelled: true}
		}
		return ProbeResponse{Error: toAppError(err)}
	}
	return ProbeResponse{Result: res}
}

// CancelProbe aborts the analysis in progress, if any.
func (a *App) CancelProbe() { a.cancelProbe() }

func (a *App) cancelProbe() {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if a.probeCancel != nil {
		a.probeCancel()
		a.probeCancel = nil
	}
}

func toAppError(err error) *apperr.AppError {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return apperr.Classify(nil, err)
}

// ---- downloading ----

// EnqueueResponse reports how many downloads were added.
type EnqueueResponse struct {
	Added   int    `json:"added"`
	Skipped int    `json:"skipped"` // already queued or running
	Error   string `json:"error"`
}

// Enqueue adds already-analysed items (a video or selected playlist entries) to the queue.
func (a *App) Enqueue(items []queue.NewJob, opts settings.DownloadOptions) EnqueueResponse {
	if len(items) == 0 {
		return EnqueueResponse{Error: "nothing selected to download"}
	}
	jobs, skipped, err := a.q.Add(items, opts)
	if err != nil {
		return EnqueueResponse{Error: err.Error()}
	}
	return EnqueueResponse{Added: len(jobs), Skipped: skipped}
}

// LinkFailure names a link that couldn't be analysed during a bulk add.
type LinkFailure struct {
	URL   string           `json:"url"`
	Error *apperr.AppError `json:"error"`
}

// BulkResponse reports the outcome of adding many links at once.
type BulkResponse struct {
	Added   int           `json:"added"`
	Skipped int           `json:"skipped"`
	Failed  []LinkFailure `json:"failed"`
	Error   string        `json:"error"`
	// Cancelled is set when CancelBulk stopped the run; the links not queued are in Failed.
	Cancelled bool `json:"cancelled"`
}

// bulkWorkers is how many links a bulk add analyses at once. Each is a yt-dlp process that
// mostly waits on the network, so a few in parallel cut the wait several times over.
const bulkWorkers = 4

// BulkProgress is sent as "bulk:progress" while a bulk add works through its links.
type BulkProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// BulkLink is one link of a bulk add, with the options it is queued with (a site's preset can
// differ from the rest).
type BulkLink struct {
	URL     string                   `json:"url"`
	Options settings.DownloadOptions `json:"options"`
}

// EnqueueLinks analyses each link, expands playlists in full, and queues everything. It is meant
// for pasting a batch of links without reviewing each one. Links are analysed a few at a time and
// queued in the order given, each as soon as it and the ones before it are ready.
func (a *App) EnqueueLinks(links []BulkLink) BulkResponse {
	out := BulkResponse{Failed: []LinkFailure{}}
	var clean []BulkLink
	for _, l := range links {
		if l.URL = strings.TrimSpace(l.URL); l.URL == "" {
			continue
		}
		if err := settings.ValidateOptions(&l.Options); err != nil {
			out.Error = err.Error()
			return out
		}
		clean = append(clean, l)
	}

	ctx, cancel := context.WithCancel(a.root)
	defer cancel()
	a.bulkMu.Lock()
	if a.bulkCancel != nil {
		a.bulkCancel()
	}
	a.bulkCancel = cancel
	a.bulkMu.Unlock()

	type result struct {
		items  []queue.NewJob
		failed []LinkFailure
		err    error
	}
	a.emit("bulk:progress", BulkProgress{Total: len(clean)})
	inOrder(len(clean), bulkWorkers, func(i int) result {
		if ctx.Err() != nil {
			return result{err: ctx.Err()}
		}
		items, failed, err := a.bulkItems(ctx, clean[i].URL, false)
		return result{items, failed, err}
	}, func(i int, r result) {
		defer a.emit("bulk:progress", BulkProgress{Done: i + 1, Total: len(clean)})
		l, opts := clean[i].URL, clean[i].Options
		if ctx.Err() != nil {
			// Cancelled: the links not yet queued stay in the box to be added later.
			out.Cancelled = true
			out.Failed = append(out.Failed, LinkFailure{URL: l, Error: apperr.New("cancelled", "Not added",
				"The bulk add was cancelled before this link was queued.", "", apperr.ActionNone)})
			return
		}
		out.Failed = append(out.Failed, r.failed...)
		if r.err != nil {
			out.Failed = append(out.Failed, LinkFailure{URL: l, Error: toAppError(r.err)})
			return
		}
		if len(r.items) == 0 {
			if len(r.failed) == 0 {
				out.Failed = append(out.Failed, LinkFailure{URL: l, Error: apperr.New("empty", "Nothing to download",
					"This link has no videos that can be downloaded (they may all be private or deleted).", "", apperr.ActionNone)})
			}
			return
		}
		jobs, skipped, err := a.q.Add(r.items, opts)
		if err != nil {
			out.Failed = append(out.Failed, LinkFailure{URL: l, Error: apperr.Classify(nil, err)})
			return
		}
		out.Added += len(jobs)
		out.Skipped += skipped
	})
	return out
}

// CancelBulk stops a running EnqueueLinks. Links already queued stay queued.
func (a *App) CancelBulk() {
	a.bulkMu.Lock()
	defer a.bulkMu.Unlock()
	if a.bulkCancel != nil {
		a.bulkCancel()
		a.bulkCancel = nil
	}
}

// inOrder runs work for inputs 0..n-1, at most limit at a time, and passes each result to use in
// input order, as soon as it and every earlier one are done. use runs on the calling goroutine.
func inOrder[T any](n, limit int, work func(i int) T, use func(i int, r T)) {
	results := make([]T, n)
	ready := make([]chan struct{}, n)
	for i := range ready {
		ready[i] = make(chan struct{})
	}
	slots := make(chan struct{}, limit)
	go func() {
		for i := 0; i < n; i++ {
			slots <- struct{}{}
			go func(i int) {
				defer func() { <-slots }()
				results[i] = work(i)
				close(ready[i])
			}(i)
		}
	}()
	for i := 0; i < n; i++ {
		<-ready[i]
		use(i, results[i])
	}
}

// bulkItems analyses one link and returns what to queue. Playlists listed inside it (a
// channel's Playlists tab) are analysed in turn, one level deep; those that fail are reported
// in failed without stopping the rest.
func (a *App) bulkItems(ctx context.Context, link string, nested bool) (items []queue.NewJob, failed []LinkFailure, err error) {
	env, cleanup, err := a.buildEnv(ctx)
	if err != nil {
		return nil, nil, err
	}
	res, err := ytdlp.Probe(ctx, env, link, false)
	cleanup()
	if err != nil {
		return nil, nil, err
	}
	switch res.Kind {
	case "video":
		v := res.Video
		u := v.URL
		if u == "" {
			u = link
		}
		items = append(items, queue.NewJob{URL: u, Title: v.Title, Uploader: v.Uploader, Thumbnail: v.ThumbnailSmall, Duration: v.Duration})
	case "playlist":
		pl := res.Playlist
		for _, e := range pl.Entries {
			switch {
			case e.Unavailable, e.IsPlaylist && nested:
			case e.IsPlaylist:
				sub, subFailed, err := a.bulkItems(ctx, e.URL, true)
				failed = append(failed, subFailed...)
				if err != nil {
					failed = append(failed, LinkFailure{URL: e.URL, Error: toAppError(err)})
				}
				items = append(items, sub...)
			default:
				items = append(items, queue.NewJob{
					URL: e.URL, Title: e.Title, Uploader: e.Uploader, Thumbnail: e.Thumbnail, Duration: e.Duration,
					PlaylistTitle: pl.Title, PlaylistIndex: e.Index, PlaylistCount: len(pl.Entries),
				})
			}
		}
	}
	return items, failed, nil
}

// ---- queue control ----

func (a *App) PauseJob(id string) error     { return a.q.Pause(id) }
func (a *App) ResumeJob(id string) error    { return a.q.Resume(id) }
func (a *App) CancelJob(id string) error    { return a.q.Cancel(id) }
func (a *App) RetryJob(id string) error     { return a.q.Retry(id) }
func (a *App) RemoveJob(id string) error    { return a.q.Remove(id) }
func (a *App) PauseAll()                    { a.q.PauseAll() }
func (a *App) ResumeAll()                   { a.q.ResumeAll() }
func (a *App) GetJobLog(id string) []string { return a.q.Log(id) }

// RetryJobWith gives a failed or cancelled download new options and runs it again.
func (a *App) RetryJobWith(id string, opts settings.DownloadOptions) error {
	return a.q.RetryWith(id, opts)
}

// ClearFinished removes finished jobs from the list (files on disk are never touched).
func (a *App) ClearFinished(onlyCompleted bool) []string { return a.q.ClearFinished(onlyCompleted) }

// RetryFailed retries every failed download and returns how many went back in line.
func (a *App) RetryFailed() int { return a.q.RetryFailed() }

// MoveJobToTop makes a waiting download the next to start.
func (a *App) MoveJobToTop(id string) error { return a.q.MoveToTop(id) }

// ---- files and folders ----

// OpenFile opens a finished download in the default player.
func (a *App) OpenFile(id string) error {
	path, err := a.jobFile(id)
	if err != nil {
		return err
	}
	return sys.Open(path)
}

// RevealFile shows a finished download in Explorer.
func (a *App) RevealFile(id string) error {
	path, err := a.jobFile(id)
	if err != nil {
		return err
	}
	return sys.Reveal(path)
}

func (a *App) jobFile(id string) (string, error) {
	j, ok := a.q.Get(id)
	if !ok {
		return "", errors.New("that download no longer exists")
	}
	if j.OutputFile == "" {
		return "", errors.New("this download has no file yet")
	}
	if _, err := os.Stat(j.OutputFile); err != nil {
		return "", errors.New("the file has been moved or deleted")
	}
	return j.OutputFile, nil
}

// OpenDownloadFolder opens the configured download folder, creating it if needed.
func (a *App) OpenDownloadFolder() error {
	dir := a.store.Get().DownloadDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return sys.Open(dir)
}

// OpenDataFolder opens the folder holding settings and the queue file.
func (a *App) OpenDataFolder() error { return sys.Open(a.dataDir) }

// ---- download archive ----

func (a *App) archivePath() string { return filepath.Join(a.dataDir, "archive.txt") }

// ArchiveInfo describes the download archive behind "Remember downloaded videos".
type ArchiveInfo struct {
	Path  string `json:"path"`
	Count int    `json:"count"` // videos it lists
	Error string `json:"error"`
}

// GetArchiveInfo counts the videos in the download archive (one "extractor id" per line).
func (a *App) GetArchiveInfo() ArchiveInfo {
	info := ArchiveInfo{Path: a.archivePath()}
	raw, err := settings.ReadFileRetry(info.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		info.Error = err.Error()
	default:
		for _, l := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(l) != "" {
				info.Count++
			}
		}
	}
	return info
}

// ClearArchive forgets every downloaded video, so they can all be downloaded again. Downloads
// already running keep their own handle on the file, so it is refused while any run.
func (a *App) ClearArchive() ArchiveInfo {
	if n := a.q.ActiveCount(); n > 0 {
		info := a.GetArchiveInfo()
		info.Error = "Downloads are running. Pause or finish them first, then clear the list."
		return info
	}
	if err := os.Remove(a.archivePath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		info := a.GetArchiveInfo()
		info.Error = err.Error()
		return info
	}
	return a.GetArchiveInfo()
}

// RevealArchive shows the archive file in Explorer.
func (a *App) RevealArchive() error {
	p := a.archivePath()
	if _, err := os.Stat(p); err != nil {
		return errors.New("no videos have been remembered yet")
	}
	return sys.Reveal(p)
}

// ready reports whether Wails has handed us its runtime context. The wails runtime helpers call
// log.Fatal when given a bad context — which would kill the whole app — so every method that
// uses them must check this first (the frontend can call in before OnStartup has run).
func (a *App) ready() bool { return a.rt() != nil }

// PickDirectory shows a native folder chooser. It returns "" if the user cancelled.
func (a *App) PickDirectory(title, start string) string {
	if !a.ready() {
		return ""
	}
	dir, err := wr.OpenDirectoryDialog(a.rt(), wr.OpenDialogOptions{Title: title, DefaultDirectory: start})
	if err != nil {
		return ""
	}
	return dir
}

// PickFile shows a native file chooser filtered by pattern (e.g. "*.txt"). "" means cancelled.
func (a *App) PickFile(title, filterName, pattern string) string {
	if !a.ready() {
		return ""
	}
	opts := wr.OpenDialogOptions{Title: title}
	if pattern != "" {
		opts.Filters = []wr.FileFilter{{DisplayName: filterName, Pattern: pattern}, {DisplayName: "All files", Pattern: "*.*"}}
	}
	f, err := wr.OpenFileDialog(a.rt(), opts)
	if err != nil {
		return ""
	}
	return f
}

// OpenExternal opens a web page in the user's browser.
func (a *App) OpenExternal(link string) error {
	if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
		return errors.New("only web links can be opened")
	}
	if !a.ready() {
		return errors.New("the app is still starting")
	}
	wr.BrowserOpenURL(a.rt(), link)
	return nil
}

// ReadClipboard returns the clipboard text.
func (a *App) ReadClipboard() string {
	if !a.ready() {
		return ""
	}
	a.clipMu.Lock()
	defer a.clipMu.Unlock()
	t, err := wr.ClipboardGetText(a.rt())
	if err != nil {
		return ""
	}
	return t
}

// CopyText puts text on the clipboard.
func (a *App) CopyText(text string) error {
	if !a.ready() {
		return errors.New("the app is still starting")
	}
	a.clipMu.Lock()
	defer a.clipMu.Unlock()
	return wr.ClipboardSetText(a.rt(), text)
}

// ---- cookies ----

// CookieResponse is the outcome of importing or inspecting cookies.
type CookieResponse struct {
	Info     ytdlp.CookieInfo  `json:"info"`
	Settings settings.Settings `json:"settings"`
	Error    string            `json:"error"`
}

// InspectCookieFile summarises a cookies.txt file without importing it.
func (a *App) InspectCookieFile(path string) ytdlp.CookieInfo { return ytdlp.InspectCookieFile(path) }

// ImportCookieFile validates a cookies.txt file, stores a private copy and switches the cookie
// source to it.
func (a *App) ImportCookieFile(path string) CookieResponse {
	data, err := os.ReadFile(path)
	if err != nil {
		return CookieResponse{Settings: a.store.Get(), Error: ytdlp.InspectCookieFile(path).Problem}
	}
	return a.importCookies(data)
}

// ImportCookieText does the same for cookies pasted as text.
func (a *App) ImportCookieText(text string) CookieResponse { return a.importCookies([]byte(text)) }

func (a *App) importCookies(data []byte) CookieResponse {
	info, err := ytdlp.ImportCookies(data, a.dataDir)
	if err != nil {
		return CookieResponse{Info: info, Settings: a.store.Get(), Error: err.Error()}
	}
	saved, err := a.updateSettings(func(s *settings.Settings) {
		s.Cookies.Mode = "file"
		s.Cookies.File = info.Path
	})
	if err != nil {
		return CookieResponse{Info: info, Settings: a.store.Get(), Error: err.Error()}
	}
	return CookieResponse{Info: info, Settings: saved}
}

// RemoveCookieFile deletes the imported cookies and turns cookies off.
func (a *App) RemoveCookieFile() CookieResponse {
	managed := filepath.Join(a.dataDir, "cookies.txt")
	_ = os.Remove(managed)
	saved, err := a.updateSettings(func(s *settings.Settings) {
		if s.Cookies.File == managed || s.Cookies.Mode == "file" {
			s.Cookies.Mode = "none"
			s.Cookies.File = ""
		}
	})
	if err != nil {
		return CookieResponse{Settings: a.store.Get(), Error: err.Error()}
	}
	return CookieResponse{Settings: saved}
}

// TestCookies checks that the configured cookie source can actually be read.
func (a *App) TestCookies() ytdlp.CookieTest {
	s := a.store.Get()
	switch s.Cookies.Mode {
	case "file":
		info := ytdlp.InspectCookieFile(s.Cookies.File)
		t := ytdlp.CookieTest{Browser: "cookies.txt", Count: info.Count, OK: info.Valid}
		if !info.Valid {
			t.Error = apperr.New("cookie_file", "Cookies file problem", info.Problem,
				"Re-export the cookies as cookies.txt (Netscape format) and import them again.", apperr.ActionCookies)
		}
		return t
	case "browser":
		env, cleanup, err := a.buildEnv(a.root)
		if err != nil {
			return ytdlp.CookieTest{Browser: s.Cookies.Browser, Error: toAppError(err)}
		}
		defer cleanup()
		return ytdlp.TestBrowserCookies(a.root, env)
	default:
		return ytdlp.CookieTest{Error: apperr.New("cookies_off", "Cookies are turned off",
			"No cookie source is selected.", "Choose a browser or import a cookies.txt file first.", apperr.ActionNone)}
	}
}

// ---- updates ----

// CheckUpdate compares the installed yt-dlp with the newest release.
func (a *App) CheckUpdate() UpdateInfo {
	ctx, cancel := context.WithTimeout(a.root, 30*time.Second)
	defer cancel()
	return a.checkUpdate(ctx)
}

// InstallFFmpeg downloads ffmpeg into the app's folder, or updates the copy it installed before.
func (a *App) InstallFFmpeg() UpdateResult {
	ctx, cancel := context.WithTimeout(a.root, 15*time.Minute)
	defer cancel()
	return a.installFFmpeg(ctx)
}

// UpdateYtdlp installs the newest yt-dlp (or installs it for the first time).
func (a *App) UpdateYtdlp() UpdateResult {
	ctx, cancel := context.WithTimeout(a.root, 6*time.Minute)
	defer cancel()
	return a.installOrUpdate(ctx)
}
