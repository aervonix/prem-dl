package main

import (
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/sys"
)

func TestParseLaunch(t *testing.T) {
	const video = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	for _, c := range []struct {
		args []string
		want launchArgs
	}{
		{nil, launchArgs{}},
		{[]string{"--background"}, launchArgs{Background: true}},
		{[]string{video}, launchArgs{Link: video}},
		// What Windows passes for a prem-dl: link: the whole link, its own link URL-encoded.
		{[]string{"prem-dl://open?url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3DdQw4w9WgXcQ"}, launchArgs{Link: video}},
		{[]string{"PREM-DL://open/?url=" + "https%3A%2F%2Fexample.com%2Fa%26b"}, launchArgs{Link: "https://example.com/a&b"}},
		// Only http(s) links get through, and nothing that could become a yt-dlp flag.
		{[]string{"prem-dl://open?url=file%3A%2F%2F%2FC%3A%2Fx"}, launchArgs{}},
		{[]string{"prem-dl://open?url=--exec%3Dcalc"}, launchArgs{}},
		{[]string{"prem-dl://open"}, launchArgs{}},
		// The Chrome helper's quick actions; anything else opens the link.
		{[]string{"prem-dl://download?url=https%3A%2F%2Fyoutu.be%2Fx&preset=Music"}, launchArgs{Link: "https://youtu.be/x", Action: "download", Preset: "Music"}},
		{[]string{"prem-dl://follow?url=https%3A%2F%2Fwww.youtube.com%2F%40chan"}, launchArgs{Link: "https://www.youtube.com/@chan", Action: "follow"}},
		{[]string{"prem-dl://erase?url=https%3A%2F%2Fyoutu.be%2Fx"}, launchArgs{Link: "https://youtu.be/x"}},
		// Windows adds its own arguments when a notification starts the app.
		{[]string{"-Embedding"}, launchArgs{}},
		{[]string{"--background", video, "https://example.com/second"}, launchArgs{Link: video, Background: true}},
	} {
		if got := parseLaunch(c.args); got != c.want {
			t.Errorf("parseLaunch(%q) = %+v, want %+v", c.args, got, c.want)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func job(id string, s queue.Status, pct float64) queue.Job {
	return queue.Job{ID: id, Title: "Video " + id, Status: s, Percent: pct}
}

// tracker records what an activity reports.
type tracker struct {
	mu       sync.Mutex
	last     overview
	finished [][]queue.Job
	got      chan struct{}
}

func newTracker(jobs []queue.Job) (*activity, *tracker) {
	tr := &tracker{got: make(chan struct{}, 10)}
	a := newActivity(jobs, 20*time.Millisecond, func(ov overview) {
		tr.mu.Lock()
		tr.last = ov
		tr.mu.Unlock()
	}, func(js []queue.Job) {
		tr.mu.Lock()
		tr.finished = append(tr.finished, js)
		tr.mu.Unlock()
		tr.got <- struct{}{}
	})
	return a, tr
}

func (tr *tracker) overview() overview {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.last
}

func TestBatchProgressFillsUpAndEnds(t *testing.T) {
	a, tr := newTracker(nil)
	a.observe([]queue.Job{job("a", queue.StatusQueued, -1), job("b", queue.StatusQueued, -1)}, nil)
	if st, _ := taskbarProgress(tr.overview()); st != sys.ProgressIndeterminate {
		t.Fatalf("nothing known yet: state %v, want indeterminate", st)
	}

	a.observe([]queue.Job{job("a", queue.StatusDownloading, 50)}, nil)
	ov := tr.overview()
	if st, f := taskbarProgress(ov); st != sys.ProgressNormal || !near(f, 0.25) {
		t.Fatalf("one of two half done: %v %.2f, want normal 0.25", st, f)
	}
	if ov.Downloading != 1 || ov.Queued != 1 {
		t.Fatalf("counts: %+v", ov)
	}

	// The finished job keeps counting, so the bar doesn't jump back.
	a.observe([]queue.Job{job("a", queue.StatusCompleted, 100), job("b", queue.StatusDownloading, 10)}, nil)
	if _, f := taskbarProgress(tr.overview()); !near(f, 0.55) {
		t.Fatalf("after the first finished: %.2f, want 0.55", f)
	}

	a.observe([]queue.Job{job("b", queue.StatusCompleted, 100)}, nil)
	if st, _ := taskbarProgress(tr.overview()); st != sys.ProgressNone {
		t.Fatalf("all done: state %v, want none", st)
	}
	// A new download starts a new batch from zero.
	a.observe([]queue.Job{job("c", queue.StatusDownloading, 40)}, nil)
	if _, f := taskbarProgress(tr.overview()); !near(f, 0.4) {
		t.Fatalf("new batch: %.2f, want 0.4", f)
	}
}

func TestBatchShowsFailuresAndPauses(t *testing.T) {
	a, tr := newTracker(nil)
	a.observe([]queue.Job{job("a", queue.StatusDownloading, 20), job("b", queue.StatusDownloading, 20)}, nil)
	a.observe([]queue.Job{job("a", queue.StatusFailed, 20)}, nil)
	if st, _ := taskbarProgress(tr.overview()); st != sys.ProgressError {
		t.Fatalf("one failed while the other runs: %v, want error", st)
	}
	a.observe([]queue.Job{job("b", queue.StatusPaused, 60)}, nil)
	if st, f := taskbarProgress(tr.overview()); st != sys.ProgressPaused || !near(f, 0.8) {
		t.Fatalf("only a paused job left: %v %.2f, want paused 0.80", st, f)
	}
	// Cancelling or removing what's left ends the batch.
	a.observe([]queue.Job{job("b", queue.StatusCancelled, 60)}, nil)
	if ov := tr.overview(); ov.Batch || ov.Failed != 0 {
		t.Fatalf("batch should be over: %+v", ov)
	}
}

func TestRestoredJobsAreQuiet(t *testing.T) {
	// The queue comes back with old finished and paused jobs: no batch, no notifications.
	a, tr := newTracker([]queue.Job{job("old", queue.StatusCompleted, 100), job("p", queue.StatusPaused, 30)})
	if ov := a.current(); ov.Batch || ov.Paused != 1 {
		t.Fatalf("restored queue: %+v", ov)
	}
	a.observe([]queue.Job{job("old", queue.StatusCompleted, 100)}, nil)
	time.Sleep(60 * time.Millisecond)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.finished) != 0 {
		t.Fatalf("notified about a download that finished before: %v", tr.finished)
	}
}

func TestFinishesAreGatheredAndCancelsIgnored(t *testing.T) {
	a, tr := newTracker(nil)
	a.observe([]queue.Job{job("a", queue.StatusDownloading, 10), job("b", queue.StatusDownloading, 10), job("c", queue.StatusDownloading, 10)}, nil)
	a.observe([]queue.Job{job("a", queue.StatusCompleted, 100)}, nil)
	a.observe([]queue.Job{job("b", queue.StatusFailed, 10), job("c", queue.StatusCancelled, 10)}, nil)
	select {
	case <-tr.got:
	case <-time.After(2 * time.Second):
		t.Fatal("no notification")
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.finished) != 1 || len(tr.finished[0]) != 2 {
		t.Fatalf("want one notification for a and b, got %v", tr.finished)
	}
	if got := tr.finished[0]; got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("finished: %v", got)
	}
}

func TestTrayTooltip(t *testing.T) {
	if got := trayTooltip(overview{}); got != "Prem-dl" {
		t.Errorf("idle: %q", got)
	}
	got := trayTooltip(overview{Downloading: 2, Queued: 3, Failed: 1, Batch: true, Running: true, Fraction: 0.456})
	if got != "Prem-dl\n2 downloading, 3 queued, 1 failed (45%)" {
		t.Errorf("busy: %q", got)
	}
	if got := trayTooltip(overview{Queued: 1, Batch: true, Running: true, Fraction: -1}); strings.Contains(got, "%") {
		t.Errorf("unknown progress shown as a number: %q", got)
	}
}

func TestFinishToast(t *testing.T) {
	done := job("a", queue.StatusCompleted, 100)
	done.OutputFile = `C:\x\Video a.mp4`
	n := finishToast([]queue.Job{done})
	if n.Title != "Download finished" || n.Body != "Video a" || n.CategoryID != toastDone || n.Data["job"] != "a" {
		t.Errorf("one done: %+v", n)
	}

	failed := job("b", queue.StatusFailed, 0)
	failed.Error = &apperr.AppError{Title: "Video unavailable"}
	n = finishToast([]queue.Job{failed})
	if n.Title != "Download failed" || n.Body != "Video b\nVideo unavailable" || n.CategoryID != toastFailed {
		t.Errorf("one failed: %+v", n)
	}

	// The catch-all error is titled "Download failed" too; its message says what went wrong.
	odd := job("d", queue.StatusFailed, 0)
	odd.Error = &apperr.AppError{Title: "Download failed", Message: "Postprocessing: Error opening output files: Encoder not found"}
	if n = finishToast([]queue.Job{odd}); n.Body != "Video d\nPostprocessing: Error opening output files: Encoder not found" {
		t.Errorf("catch-all error: %q", n.Body)
	}

	n = finishToast([]queue.Job{done, failed, job("c", queue.StatusCompleted, 100)})
	if n.Title != "Downloads finished with problems" || n.Body != "2 done, 1 failed." || n.CategoryID != "" {
		t.Errorf("mixed: %+v", n)
	}
	if n = finishToast([]queue.Job{done, done}); n.Title != "2 downloads finished" {
		t.Errorf("two done: %+v", n)
	}
}
