// Package queue runs downloads: it schedules yt-dlp processes, tracks their progress and
// persists the list so it survives a restart.
package queue

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/sys"
	"yt-dlp-gui/internal/ytdlp"
)

// Status is where a job is in its life.
type Status string

const (
	StatusQueued      Status = "queued"
	StatusStarting    Status = "starting"
	StatusDownloading Status = "downloading"
	StatusProcessing  Status = "processing"
	StatusPaused      Status = "paused"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusCancelled   Status = "cancelled"
)

func (s Status) active() bool {
	return s == StatusStarting || s == StatusDownloading || s == StatusProcessing
}

func (s Status) finished() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

// Job is one download. It is sent to the frontend as JSON.
type Job struct {
	ID        string  `json:"id"`
	Seq       int64   `json:"seq"` // position in the queue; new jobs count up, MoveToTop goes below the lowest
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Uploader  string  `json:"uploader"`
	Thumbnail string  `json:"thumbnail"`
	Duration  float64 `json:"duration"`

	Status     Status  `json:"status"`
	Stage      string  `json:"stage"`
	Percent    float64 `json:"percent"` // 0-100, or -1 when unknown
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Speed      float64 `json:"speed"`
	ETA        int     `json:"eta"` // seconds, -1 when unknown

	PlaylistTitle string `json:"playlistTitle"`
	PlaylistIndex int    `json:"playlistIndex"`
	PlaylistCount int    `json:"playlistCount"`

	Options    settings.DownloadOptions `json:"options"`
	OutputFile string                   `json:"outputFile"`
	Note       string                   `json:"note"`
	Error      *apperr.AppError         `json:"error"`
	Warnings   []string                 `json:"warnings"`
	Attempts   int                      `json:"attempts"`
	// RetryAt is set while a job that failed with a passing problem (a dropped connection, a
	// 403, rate limiting) waits to be retried automatically; Error says what went wrong.
	// AutoRetries counts those retries since the user last started the job.
	RetryAt     time.Time `json:"retryAt" ts_type:"string"`
	AutoRetries int       `json:"autoRetries"`
	// WaitUntil is set while yt-dlp waits for a scheduled stream to start (--wait-for-video):
	// when it looks again. A waiting job doesn't take a download slot.
	WaitUntil time.Time `json:"waitUntil" ts_type:"string"`

	CreatedAt  time.Time `json:"createdAt" ts_type:"string"`
	StartedAt  time.Time `json:"startedAt" ts_type:"string"`
	FinishedAt time.Time `json:"finishedAt" ts_type:"string"`
}

// NewJob is what the caller supplies to enqueue a download.
type NewJob struct {
	URL           string  `json:"url"`
	Title         string  `json:"title"`
	Uploader      string  `json:"uploader"`
	Thumbnail     string  `json:"thumbnail"`
	Duration      float64 `json:"duration"`
	PlaylistTitle string  `json:"playlistTitle"`
	PlaylistIndex int     `json:"playlistIndex"`
	PlaylistCount int     `json:"playlistCount"`
}

// Deps connects the queue to the rest of the app.
type Deps struct {
	// Settings returns the current settings (read at schedule time so changes apply immediately).
	Settings func() settings.Settings
	// Env prepares the environment for one run. The cleanup func releases per-run resources.
	Env func(ctx context.Context) (ytdlp.Env, func(), error)
	// Info, if set, returns a file holding a recent analysis of url to download from
	// (ytdlp.InfoCache.Take), or "". done removes it.
	Info func(url string) (path string, done func())
	// Emit delivers an event to the UI.
	Emit func(event string, payload any)
	// Observe, if set, gets the same changes as the UI: jobs that changed and IDs removed. It is
	// called from one goroutine, without the manager's lock held.
	Observe func(changed []Job, removed []string)
	// StatePath is where the job list is persisted ("" disables persistence).
	StatePath string
}

type stopReason int

const (
	stopNone stopReason = iota
	stopPause
	stopCancel
)

const (
	maxLog    = 400
	maxJobs   = 500
	flushTick = 200 * time.Millisecond
	saveEvery = time.Second // queue.json is rewritten at most this often
)

// retryDelays lists, per error code, how long to wait before each automatic retry. Only
// problems that often clear up by themselves are here: retrying a missing file or a full disk
// on a timer just fails again. Replaced in tests.
var retryDelays = map[string][]time.Duration{
	"network":      {30 * time.Second, 2 * time.Minute, 10 * time.Minute},
	"forbidden":    {30 * time.Second, 2 * time.Minute, 10 * time.Minute},
	"rate_limited": {2 * time.Minute, 10 * time.Minute, 30 * time.Minute},
}

// removeWait is how long Remove waits for a running job's process to exit (shortened in tests).
var removeWait = 5 * time.Second

type entry struct {
	job    Job
	log    []string
	cancel context.CancelFunc
	stop   stopReason
	// remove is set when the job was removed while running; finish deletes the entry once the
	// process has exited, so until then it still counts against the limit and blocks its URL.
	remove bool
	// fromInfo is set while the run downloads from a saved analysis (Deps.Info) rather than
	// the page. If that fails, the job runs again at once, and pageOnly makes that run read the page.
	fromInfo, pageOnly bool
}

// Manager owns every job.
type Manager struct {
	deps Deps

	mu      sync.Mutex
	seq     int64
	order   []string
	entries map[string]*entry
	dirty   map[string]bool
	removed []string // removed since the last flush; emitted by flush
	persist bool
	// nextSave spaces out saves (and backs off after a failed one); noSave is set when
	// queue.json exists but couldn't be read, so it is never overwritten.
	nextSave time.Time
	noSave   bool
	saveGen  int64 // snapshots taken
	held     bool  // nothing new starts (yt-dlp is being replaced); see Hold
	// wake runs the scheduler when the earliest automatic retry is due.
	wake *time.Timer

	// saveMu serialises writes to queue.json, which happen without mu held. savedGen is the
	// snapshot on disk, so an older one finishing late can't replace a newer one.
	saveMu   sync.Mutex
	savedGen int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a manager and restores any persisted jobs. Jobs that were running when the app
// last exited come back paused so nothing starts consuming bandwidth unannounced.
func New(deps Deps) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		deps: deps, ctx: ctx, cancel: cancel,
		entries: map[string]*entry{}, dirty: map[string]bool{},
	}
	m.load()
	return m
}

// Start launches the background flusher. Call once.
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.flusher()
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- public API ----

// List returns every job in queue order.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, cloneJob(m.entries[id].job))
	}
	return out
}

// Add enqueues downloads. Links that are already waiting or running are skipped; the count of
// skipped links is returned alongside the new jobs.
func (m *Manager) Add(items []NewJob, opts settings.DownloadOptions) ([]Job, int, error) {
	if err := settings.ValidateOptions(&opts); err != nil {
		return nil, 0, err
	}
	// Validate everything first so a bad link can't leave the batch half-added.
	links := make([]string, len(items))
	for i, it := range items {
		link, err := ytdlp.NormalizeURL(it.URL)
		if err != nil {
			return nil, 0, err
		}
		links[i] = link
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	live := map[string]bool{}
	for _, e := range m.entries {
		if !e.job.Status.finished() {
			live[e.job.URL] = true
		}
	}
	var added []Job
	skipped := 0
	for i, it := range items {
		link := links[i]
		if live[link] {
			skipped++
			continue
		}
		live[link] = true
		m.seq++
		j := Job{
			ID: newID(), Seq: m.seq, URL: link, Title: it.Title, Uploader: it.Uploader, Thumbnail: it.Thumbnail,
			Duration: it.Duration, Status: StatusQueued, Stage: "Waiting", Percent: 0, ETA: -1,
			PlaylistTitle: it.PlaylistTitle, PlaylistIndex: it.PlaylistIndex, PlaylistCount: it.PlaylistCount,
			Options: opts, Warnings: []string{}, CreatedAt: time.Now(),
		}
		if j.Title == "" {
			j.Title = link
		}
		m.entries[j.ID] = &entry{job: j}
		m.order = append(m.order, j.ID)
		m.dirty[j.ID] = true
		added = append(added, cloneJob(j))
	}
	m.trimLocked()
	m.persist = true
	m.scheduleLocked()
	return added, skipped, nil
}

// Pause stops a running job (keeping its partial files) or holds a queued one.
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return errNoJob
	}
	switch {
	case e.job.Status == StatusQueued:
		m.setLocked(e, func(j *Job) { j.Status = StatusPaused; j.Stage = "Paused"; j.RetryAt = time.Time{} })
		m.scheduleLocked() // the retry timer may no longer be needed
	case e.job.Status.active():
		e.stop = stopPause
		if e.cancel != nil {
			e.cancel()
		}
	}
	return nil
}

// Resume puts a paused job back in the queue; yt-dlp continues from the partial file.
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return errNoJob
	}
	if e.job.Status == StatusPaused {
		// The user is starting it again: it gets a fresh set of automatic retries.
		m.setLocked(e, func(j *Job) { j.Status = StatusQueued; j.Stage = "Waiting"; j.Speed = 0; j.ETA = -1; j.AutoRetries = 0 })
		m.scheduleLocked()
	}
	return nil
}

// Cancel stops a job for good (partial files stay on disk so Retry can pick them up).
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return errNoJob
	}
	switch {
	case e.job.Status.active():
		e.stop = stopCancel
		if e.cancel != nil {
			e.cancel()
		}
	case e.job.Status == StatusQueued || e.job.Status == StatusPaused:
		m.setLocked(e, func(j *Job) {
			j.Status = StatusCancelled
			j.Stage = "Cancelled"
			j.FinishedAt = time.Now()
			j.Speed = 0
			j.RetryAt = time.Time{}
		})
		m.scheduleLocked()
	}
	return nil
}

// Retry requeues a failed or cancelled job from scratch (resuming partial files if present).
func (m *Manager) Retry(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return errNoJob
	}
	if e.job.Status == StatusQueued && !e.job.RetryAt.IsZero() {
		// "Retry now" on a job waiting for its automatic retry.
		m.setLocked(e, func(j *Job) { j.RetryAt = time.Time{}; j.Stage = "Waiting" })
		m.scheduleLocked()
		return nil
	}
	if e.job.Status == StatusFailed || e.job.Status == StatusCancelled {
		return m.requeueLocked(e, nil)
	}
	return nil
}

// RetryWith gives a failed or cancelled job new options and requeues it.
func (m *Manager) RetryWith(id string, opts settings.DownloadOptions) error {
	if err := settings.ValidateOptions(&opts); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return errNoJob
	}
	if e.job.Status != StatusFailed && e.job.Status != StatusCancelled {
		return errNotFinished
	}
	return m.requeueLocked(e, func(j *Job) { j.Options = opts })
}

// requeueLocked puts a failed or cancelled job back in line, after edit (if any) changes it.
func (m *Manager) requeueLocked(e *entry, edit func(*Job)) error {
	// Two runs of one link would write the same .part file.
	for _, o := range m.entries {
		if o != e && o.job.URL == e.job.URL && !o.job.Status.finished() {
			return errAlreadyQueued
		}
	}
	m.setLocked(e, func(j *Job) {
		if edit != nil {
			edit(j)
		}
		j.Status = StatusQueued
		j.Stage = "Waiting"
		j.Error = nil
		j.Percent = 0
		j.Downloaded = 0 // the new run reports its own sizes
		j.Total = 0
		j.OutputFile = ""
		j.Speed = 0
		j.ETA = -1
		j.Note = ""
		j.Warnings = []string{}
		j.Attempts++
		j.AutoRetries = 0
		j.RetryAt = time.Time{}
	})
	e.log = nil
	m.scheduleLocked()
	return nil
}

// Remove deletes a job from the list. A running job is cancelled first.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok {
		m.mu.Unlock()
		return errNoJob
	}
	if !e.job.Status.active() {
		m.removeLocked([]string{id})
		m.mu.Unlock()
		return nil
	}
	// finish deletes the entry once the process has exited. A process that is slow to die
	// keeps its slot until then, so the queue can't go over the concurrency limit.
	e.stop = stopCancel
	e.remove = true
	if e.cancel != nil {
		e.cancel()
	}
	m.mu.Unlock()
	// Usually that takes a moment; wait for it so the job is gone when this returns.
	deadline := time.Now().Add(removeWait)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, still := m.entries[id]
		m.mu.Unlock()
		if !still {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil
}

// RetryFailed retries every failed job (not cancelled ones) and returns how many went back in
// line. A job whose link is queued again elsewhere is left as it is.
func (m *Manager) RetryFailed() int {
	m.mu.Lock()
	var ids []string
	for _, id := range m.order {
		if m.entries[id].job.Status == StatusFailed {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	n := 0
	for _, id := range ids {
		if m.Retry(id) == nil {
			n++
		}
	}
	return n
}

// MoveToTop makes a waiting or paused job the first in the list and, once it is waiting, the
// next to start.
func (m *Manager) MoveToTop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return errNoJob
	}
	if e.job.Status != StatusQueued && e.job.Status != StatusPaused {
		return nil
	}
	lowest := e.job.Seq
	for _, o := range m.entries {
		if o.job.Seq < lowest {
			lowest = o.job.Seq
		}
	}
	if lowest < e.job.Seq || m.order[0] != id {
		m.setLocked(e, func(j *Job) { j.Seq = lowest - 1 })
		order := []string{id}
		for _, o := range m.order {
			if o != id {
				order = append(order, o)
			}
		}
		m.order = order
		m.persist = true
	}
	m.scheduleLocked()
	return nil
}

// ClearFinished removes completed, failed and cancelled jobs. It returns the removed IDs.
func (m *Manager) ClearFinished(onlyCompleted bool) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, id := range m.order {
		st := m.entries[id].job.Status
		if st == StatusCompleted || (!onlyCompleted && st.finished()) {
			ids = append(ids, id)
		}
	}
	m.removeLocked(ids)
	return ids
}

// PauseAll pauses every queued and running job.
func (m *Manager) PauseAll() {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Pause(id)
	}
}

// ResumeAll requeues every paused job.
func (m *Manager) ResumeAll() {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Resume(id)
	}
}

// Get returns one job.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return Job{}, false
	}
	return cloneJob(e.job), true
}

// Log returns the most recent output lines of a job.
func (m *Manager) Log(id string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil
	}
	return append([]string{}, e.log...)
}

// ActiveCount is the number of jobs currently running yt-dlp.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.entries {
		if e.job.Status.active() {
			n++
		}
	}
	return n
}

// Hold stops new downloads from starting, and reports how many are running, which a caller
// that needs none (replacing the yt-dlp binary) must wait for or refuse. Release resumes
// scheduling.
func (m *Manager) Hold() (active int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.held = true
	for _, e := range m.entries {
		if e.job.Status.active() {
			active++
		}
	}
	return active
}

// Release undoes Hold and starts whatever has queued up meanwhile.
func (m *Manager) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.held = false
	m.scheduleLocked()
}

// Kick re-evaluates the schedule, e.g. after the concurrency setting changed.
func (m *Manager) Kick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduleLocked()
}

// Shutdown kills every running download (they come back paused next launch) and saves state.
func (m *Manager) Shutdown() {
	m.cancel()
	m.mu.Lock()
	m.wakeAtLocked(time.Time{})
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
	}
	m.mu.Lock()
	var removing []string
	for id, e := range m.entries {
		if e.remove {
			removing = append(removing, id)
			continue
		}
		switch {
		case e.job.Status.active() && !e.job.WaitUntil.IsZero():
			e.job.Status, e.job.Stage, e.job.WaitUntil = StatusQueued, "Waiting", time.Time{}
		case e.job.Status.active():
			e.job.Status = StatusPaused
			e.job.Stage = "Interrupted"
		}
	}
	m.removeLocked(removing)
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.writeSnapshot(snap)
}

var (
	errNoJob         = errors.New("that download no longer exists")
	errAlreadyQueued = errors.New("that link is already in the queue")
	errNotFinished   = errors.New("only a failed or cancelled download can be changed and run again")
)

// ---- scheduling and running ----

func (m *Manager) scheduleLocked() {
	if m.ctx.Err() != nil || m.held {
		return
	}
	limit := m.deps.Settings().MaxConcurrent
	now := time.Now()
	running := 0
	var nextRetry time.Time
	for _, e := range m.entries {
		if e.job.Status.active() && e.job.WaitUntil.IsZero() {
			running++
		}
		if r := e.job.RetryAt; e.job.Status == StatusQueued && r.After(now) && (nextRetry.IsZero() || r.Before(nextRetry)) {
			nextRetry = r
		}
	}
	m.wakeAtLocked(nextRetry)
	for _, id := range m.order {
		if running >= limit {
			break
		}
		e := m.entries[id]
		if e.job.Status != StatusQueued || e.job.RetryAt.After(now) {
			continue
		}
		running++
		m.setLocked(e, func(j *Job) {
			j.Status = StatusStarting
			j.Stage = "Starting"
			j.StartedAt = time.Now()
			j.Error = nil
			j.RetryAt = time.Time{}
		})
		ctx, cancel := context.WithCancel(m.ctx)
		e.cancel = cancel
		e.stop = stopNone
		m.wg.Add(1)
		go m.run(ctx, cancel, e)
	}
}

// wakeAtLocked (re)arms the timer that runs the scheduler when the next automatic retry is due;
// a zero time disarms it.
func (m *Manager) wakeAtLocked(at time.Time) {
	if m.wake != nil {
		m.wake.Stop()
		m.wake = nil
	}
	if !at.IsZero() {
		m.wake = time.AfterFunc(time.Until(at), m.Kick)
	}
}

// retryDelay is how long to wait before retrying a job that failed with ae automatically, and
// false when it shouldn't be: the problem won't pass by itself, the retries are used up, or the
// user turned automatic retries off.
func (m *Manager) retryDelay(j Job, ae *apperr.AppError) (time.Duration, bool) {
	if ae == nil || !m.deps.Settings().AutoRetry {
		return 0, false
	}
	delays := retryDelays[ae.Code]
	if j.AutoRetries >= len(delays) {
		return 0, false
	}
	return delays[j.AutoRetries], true
}

// setLocked mutates a job and marks it for the next UI flush.
func (m *Manager) setLocked(e *entry, fn func(*Job)) {
	before := e.job.Status
	fn(&e.job)
	m.dirty[e.job.ID] = true
	if e.job.Status != before {
		m.persist = true
	}
}

func (m *Manager) set(e *entry, fn func(*Job)) {
	m.mu.Lock()
	m.setLocked(e, fn)
	m.mu.Unlock()
}

func (m *Manager) run(ctx context.Context, cancel context.CancelFunc, e *entry) {
	defer m.wg.Done()
	defer cancel()

	m.mu.Lock()
	job := cloneJob(e.job)
	m.mu.Unlock()

	env, cleanup, err := m.deps.Env(ctx)
	if err != nil {
		m.finish(e, ctx, err, nil)
		return
	}
	defer cleanup()

	target := ytdlp.Target{
		URL: job.URL, PlaylistTitle: job.PlaylistTitle,
		PlaylistIndex: job.PlaylistIndex, PlaylistCount: job.PlaylistCount,
	}
	m.mu.Lock()
	pageOnly := e.pageOnly
	e.pageOnly = false
	m.mu.Unlock()
	// A stream being waited for, or recorded from its start, is read when it goes live.
	if m.deps.Info != nil && !pageOnly && !job.Options.WaitForVideo && !job.Options.LiveFromStart {
		path, done := m.deps.Info(job.URL)
		defer done()
		target.InfoFile = path
	}
	m.mu.Lock()
	e.fromInfo = target.InfoFile != ""
	m.mu.Unlock()
	args := env.DownloadArgs(job.Options, target)

	cmd := exec.CommandContext(ctx, env.Bin, args...)
	sys.Quiet(cmd)
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			sys.KillTree(cmd.Process.Pid)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.finish(e, ctx, err, nil)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.finish(e, ctx, err, nil)
		return
	}
	if err := cmd.Start(); err != nil {
		m.finish(e, ctx, fmt.Errorf("starting yt-dlp: %w", err), nil)
		return
	}
	sys.Contain(cmd)

	h := &lineHandler{m: m, e: e}
	h.tracker.Cut = job.Options.SectionStart != "" || job.Options.SectionEnd != "" || len(job.Options.Chapters) > 0
	h.tracker.Sections = len(job.Options.Chapters)
	var rw sync.WaitGroup
	rw.Add(2)
	go func() { defer rw.Done(); h.consume(stdout, false) }()
	go func() { defer rw.Done(); h.consume(stderr, true) }()

	// Watchdog: if a stray grandchild keeps the pipes open after the process tree was killed,
	// closing them unblocks the readers instead of leaving the job "running" forever.
	readersDone := make(chan struct{})
	go func() {
		select {
		case <-readersDone:
		case <-ctx.Done():
			select {
			case <-readersDone:
			case <-time.After(10 * time.Second):
				stdout.Close()
				stderr.Close()
			}
		}
	}()
	rw.Wait()
	close(readersDone)
	waitErr := cmd.Wait()
	if cmd.ProcessState != nil && cmd.ProcessState.Success() {
		// yt-dlp exited 0 just before a pause or cancel killed it. Wait still reports the
		// cancelled context, but the file is complete.
		waitErr = nil
	}

	m.finish(e, ctx, waitErr, h)
}

// finish records how a run ended and starts whatever is next in line.
func (m *Manager) finish(e *entry, ctx context.Context, runErr error, h *lineHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stop := e.stop
	e.cancel = nil
	fromInfo := e.fromInfo
	e.fromInfo = false
	// Every branch below saves the job (setLocked), which carries this along.
	waited := !e.job.WaitUntil.IsZero()
	e.job.WaitUntil = time.Time{}
	if e.remove {
		m.removeLocked([]string{e.job.ID})
		m.scheduleLocked()
		return
	}
	now := time.Now()
	if runErr == nil {
		// yt-dlp finished before a pause, cancel or shutdown could stop it: the file is complete.
		stop = stopNone
		ctx = nil
	}
	switch {
	case stop == stopPause:
		m.setLocked(e, func(j *Job) { j.Status = StatusPaused; j.Stage = "Paused"; j.Speed = 0; j.ETA = -1 })
	case stop == stopCancel:
		m.setLocked(e, func(j *Job) {
			j.Status = StatusCancelled
			j.Stage = "Cancelled"
			j.Speed = 0
			j.ETA = -1
			j.FinishedAt = now
		})
	case ctx != nil && ctx.Err() != nil && waited:
		// The app is shutting down while it waits for a stream: wait again next time it starts.
		m.setLocked(e, func(j *Job) { j.Status = StatusQueued; j.Stage = "Waiting"; j.Speed = 0; j.ETA = -1 })
	case ctx != nil && ctx.Err() != nil:
		// The app is shutting down: leave it resumable.
		m.setLocked(e, func(j *Job) { j.Status = StatusPaused; j.Stage = "Interrupted"; j.Speed = 0; j.ETA = -1 })
	case runErr == nil:
		m.setLocked(e, func(j *Job) {
			j.Status = StatusCompleted
			j.Stage = "Done"
			j.Percent = 100
			j.Speed = 0
			j.ETA = 0
			j.FinishedAt = now
			if j.Total > 0 {
				j.Downloaded = j.Total
			}
			if j.OutputFile == "" && h != nil {
				j.OutputFile = h.alreadyPath
			}
			if h != nil && h.alreadyPath != "" {
				j.Note = "Already downloaded"
			}
		})
	case fromInfo:
		// The saved analysis didn't work (the network changed, say): run again at once from the page.
		e.pageOnly = true
		m.setLocked(e, func(j *Job) { j.Status = StatusQueued; j.Stage = "Starting"; j.Speed = 0; j.ETA = -1 })
	default:
		var lines []string
		if h != nil {
			lines = h.errLines
		}
		ae := asAppError(runErr)
		if ae == nil {
			ae = apperr.Classify(lines, runErr)
		}
		if delay, ok := m.retryDelay(e.job, ae); ok {
			// It goes back in line, held until RetryAt; scheduleLocked below arms the timer.
			m.setLocked(e, func(j *Job) {
				j.Status = StatusQueued
				j.Stage = "Retrying soon"
				j.Speed = 0
				j.ETA = -1
				j.Error = ae
				j.AutoRetries++
				j.RetryAt = now.Add(delay)
			})
			break
		}
		m.setLocked(e, func(j *Job) {
			j.Status = StatusFailed
			j.Stage = "Failed"
			j.Speed = 0
			j.ETA = -1
			j.Error = ae
			j.FinishedAt = now
		})
	}
	m.scheduleLocked()
}

func asAppError(err error) *apperr.AppError {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

// lineHandler digests yt-dlp's output for one run.
type lineHandler struct {
	m *Manager
	e *entry

	mu          sync.Mutex // guards the fields below (stdout and stderr are read concurrently)
	tracker     ytdlp.Tracker
	errLines    []string
	alreadyPath string
}

var alreadyRe = regexp.MustCompile(`^\[download\] (.+) has already been downloaded`)

// maxLine is the longest output line we look at. Longer ones (-j prints megabytes of JSON on
// one line) are skipped: a Scanner that stops on them would leave yt-dlp blocked on a full pipe.
const maxLine = 256 * 1024

// splitLines splits on \n or \r and drops lines longer than maxLine.
func splitLines() bufio.SplitFunc {
	skipping := false
	return func(data []byte, atEOF bool) (int, []byte, error) {
		for i, b := range data {
			if b == '\n' || b == '\r' {
				if skipping {
					skipping = false
					return i + 1, nil, nil
				}
				return i + 1, data[:i], nil
			}
		}
		if skipping || len(data) >= maxLine {
			skipping = !atEOF
			return len(data), nil, nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
}

func (h *lineHandler) consume(r io.Reader, isErr bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 2*maxLine)
	sc.Split(splitLines())
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		h.handle(line, isErr)
	}
	// If reading stopped early, keep draining so the process can still exit.
	_, _ = io.Copy(io.Discard, r)
}

func (h *lineHandler) handle(line string, isErr bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	parsed := ytdlp.ParseLine(line)
	if parsed.Kind != ytdlp.KindProgress && parsed.Kind != ytdlp.KindWaitTick { // these would flood the log
		h.m.mu.Lock()
		h.e.log = append(h.e.log, line)
		if len(h.e.log) > maxLog {
			h.e.log = h.e.log[len(h.e.log)-maxLog:]
		}
		h.m.mu.Unlock()
	}
	if isErr {
		h.errLines = append(h.errLines, line)
		if len(h.errLines) > 100 {
			h.errLines = h.errLines[len(h.errLines)-100:]
		}
	}

	switch parsed.Kind {
	case ytdlp.KindWait:
		// Waiting for a scheduled stream: its slot goes to the next download meanwhile. Once the
		// stream starts it runs alongside the others, even if that is one over the limit.
		until := time.Now().Add(parsed.Wait)
		h.m.mu.Lock()
		h.m.setLocked(h.e, func(j *Job) { j.WaitUntil = until; j.Stage = "Waiting for the stream to start" })
		h.m.scheduleLocked()
		h.m.mu.Unlock()
	case ytdlp.KindPlan:
		h.tracker.SetPlan(parsed.Plan)
		h.m.set(h.e, func(j *Job) {
			j.WaitUntil = time.Time{} // it has started (the plan is printed just before downloading)
			p := parsed.Plan
			if p.Title != "" && (j.Title == "" || j.Title == j.URL) {
				j.Title = p.Title
			}
			if j.Uploader == "" {
				j.Uploader = p.Uploader
			}
			if j.Thumbnail == "" {
				j.Thumbnail = p.Thumbnail
			}
			if j.Duration == 0 {
				j.Duration = p.Duration
			}
		})
	case ytdlp.KindStreams:
		h.tracker.SetStreams(parsed.Streams)
	case ytdlp.KindProgress:
		snap, ok := h.tracker.Update(parsed.Progress)
		if !ok {
			return
		}
		h.m.set(h.e, func(j *Job) {
			j.Status = StatusDownloading
			j.Stage = snap.Stage
			j.Percent = snap.Percent
			j.Downloaded = snap.Downloaded
			j.Total = snap.Total
			j.Speed = snap.Speed
			j.ETA = snap.ETA
		})
	case ytdlp.KindPost:
		if parsed.PostStat == "started" {
			stage := ytdlp.PostStage(parsed.PostName)
			// Some post-processors (subtitle conversion) run before the media is fetched, so only
			// show a full bar once every stream has really finished.
			mediaDone := h.tracker.AllDone()
			h.m.set(h.e, func(j *Job) {
				j.Status = StatusProcessing
				j.Stage = stage
				if mediaDone {
					j.Percent = 100
					if j.Total > 0 {
						j.Downloaded = j.Total
					}
				}
				j.Speed = 0
				j.ETA = -1
			})
		}
	case ytdlp.KindFile:
		h.m.set(h.e, func(j *Job) { j.OutputFile = filepath.Clean(parsed.File) })
	default:
		if m := alreadyRe.FindStringSubmatch(line); m != nil {
			h.alreadyPath = m[1]
		}
		if isErr && strings.HasPrefix(line, "WARNING:") {
			w := strings.TrimSpace(strings.TrimPrefix(line, "WARNING:"))
			h.m.set(h.e, func(j *Job) {
				if len(j.Warnings) < 5 && !contains(j.Warnings, w) {
					j.Warnings = append(j.Warnings, w)
				}
			})
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ---- events and persistence ----

func (m *Manager) flusher() {
	defer m.wg.Done()
	t := time.NewTicker(flushTick)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			m.flush()
			return
		case <-t.C:
			m.flush()
		}
	}
}

func (m *Manager) flush() {
	m.mu.Lock()
	var batch []Job
	for id := range m.dirty {
		if e, ok := m.entries[id]; ok {
			batch = append(batch, cloneJob(e.job))
		}
	}
	clear(m.dirty)
	removed := m.removed
	m.removed = nil
	var snap *snapshot
	if now := time.Now(); m.persist && now.After(m.nextSave) {
		m.persist = false
		m.nextSave = now.Add(saveEvery)
		snap = m.snapshotLocked()
	}
	m.mu.Unlock()
	if m.deps.Emit != nil {
		// Removals go out after updates, from this goroutine only: an update that arrived after
		// its removal would put the job back in the UI.
		if len(batch) > 0 {
			m.deps.Emit("jobs:update", batch)
		}
		if len(removed) > 0 {
			m.deps.Emit("jobs:remove", removed)
		}
	}
	if m.deps.Observe != nil && (len(batch) > 0 || len(removed) > 0) {
		m.deps.Observe(batch, removed)
	}
	m.writeSnapshot(snap)
}

func (m *Manager) removeLocked(ids []string) {
	if len(ids) == 0 {
		return
	}
	gone := map[string]bool{}
	for _, id := range ids {
		delete(m.entries, id)
		delete(m.dirty, id)
		gone[id] = true
	}
	kept := m.order[:0]
	for _, id := range m.order {
		if !gone[id] {
			kept = append(kept, id)
		}
	}
	m.order = kept
	m.persist = true
	m.removed = append(m.removed, ids...)
}

// trimLocked drops the oldest finished jobs once the list grows past maxJobs.
func (m *Manager) trimLocked() {
	over := len(m.order) - maxJobs
	if over <= 0 {
		return
	}
	var drop []string
	for _, id := range m.order {
		if over == 0 {
			break
		}
		if m.entries[id].job.Status.finished() {
			drop = append(drop, id)
			over--
		}
	}
	m.removeLocked(drop)
}

type persisted struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// writeFile is replaced in tests to observe saves.
var writeFile = settings.WriteFileAtomic

type snapshot struct {
	gen  int64
	data persisted
}

// snapshotLocked copies the job list for saving. Encoding and writing it (several MB for a
// large queue) happen in writeSnapshot, after mu is released, so progress updates and UI calls
// don't wait on the disk.
func (m *Manager) snapshotLocked() *snapshot {
	if m.deps.StatePath == "" || m.noSave {
		return nil
	}
	m.saveGen++
	p := persisted{Version: 1, Jobs: make([]Job, 0, len(m.order))}
	for _, id := range m.order {
		p.Jobs = append(p.Jobs, cloneJob(m.entries[id].job))
	}
	return &snapshot{gen: m.saveGen, data: p}
}

// writeSnapshot saves a snapshot unless a newer one is already on disk. Call it without mu.
func (m *Manager) writeSnapshot(s *snapshot) {
	if s == nil {
		return
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	if s.gen <= m.savedGen {
		return
	}
	raw, err := json.Marshal(s.data)
	if err == nil {
		err = writeFile(m.deps.StatePath, raw, 0o600)
	}
	if err != nil {
		// Usually a file briefly held open by antivirus: try again on a later flush.
		m.mu.Lock()
		m.persist = true
		m.nextSave = time.Now().Add(2 * time.Second)
		m.mu.Unlock()
		return
	}
	m.savedGen = s.gen
}

func (m *Manager) load() {
	if m.deps.StatePath == "" {
		return
	}
	raw, err := settings.ReadFileRetry(m.deps.StatePath)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		// Saving now would replace the user's queue with an empty one.
		m.noSave = true
		return
	}
	var p persisted
	if json.Unmarshal(raw, &p) != nil {
		_ = os.Rename(m.deps.StatePath, m.deps.StatePath+".corrupt")
		return
	}
	for _, j := range p.Jobs {
		if j.ID == "" {
			continue
		}
		switch j.Status {
		case StatusQueued, StatusStarting, StatusDownloading, StatusProcessing:
			j.Status = StatusPaused
			j.Stage = "Interrupted"
			j.Speed = 0
			j.ETA = -1
			j.RetryAt = time.Time{}
		}
		if j.Warnings == nil {
			j.Warnings = []string{}
		}
		if j.Seq > m.seq {
			m.seq = j.Seq
		}
		m.entries[j.ID] = &entry{job: j}
		m.order = append(m.order, j.ID)
	}
}

func cloneJob(j Job) Job {
	j.Warnings = append([]string{}, j.Warnings...)
	j.Options.Subtitles.Langs = append([]string{}, j.Options.Subtitles.Langs...)
	j.Options.SponsorBlock = append([]string{}, j.Options.SponsorBlock...)
	if j.Error != nil {
		e := *j.Error
		j.Error = &e
	}
	return j
}
