package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// TestMain lets the test binary double as a fake yt-dlp: when FAKE_YTDLP=1 it behaves according
// to the URL it was given (the last argument) instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_YTDLP") == "1" {
		fakeYtdlp(os.Args[len(os.Args)-1])
		return
	}
	os.Exit(m.Run())
}

func fakeYtdlp(url string) {
	p := func(s string) { fmt.Println(s) }
	// A saved analysis (--load-info-json) here holds the URL whose behaviour to copy.
	if file, ok := strings.CutPrefix(url, "--load-info-json="); ok {
		data, _ := os.ReadFile(file)
		fakeYtdlp(strings.TrimSpace(string(data)))
		return
	}
	switch {
	case strings.HasSuffix(url, "/ok"):
		p(`YTGUI_PLAN {"id":"abc","title":"Fake Title","uploader":"Fake Chan","duration":10,"filesize_approx":2000}`)
		p(`YTGUI_STREAMS [{"filesize":1500,"vcodec":"avc1","acodec":"none"},{"filesize":500,"vcodec":"none","acodec":"opus"}]`)
		p(`YTGUI_P {"status":"downloading","downloaded_bytes":750,"total_bytes":1500,"filename":"v.f1.mp4","speed":1000,"eta":1}`)
		p(`YTGUI_P {"status":"finished","downloaded_bytes":1500,"total_bytes":1500,"filename":"v.f1.mp4","speed":1000}`)
		p(`YTGUI_P {"status":"downloading","downloaded_bytes":250,"total_bytes":500,"filename":"v.f2.webm","speed":1000,"eta":0}`)
		p("YTGUI_PP Merger|started")
		p("YTGUI_PP Merger|finished")
		p("YTGUI_FILE\t" + filepath.Join(os.TempDir(), "Fake Title.mkv"))
		fmt.Fprintln(os.Stderr, "WARNING: something mild")
	case strings.HasSuffix(url, "/ppfirst"):
		// Subtitle conversion runs before the media is downloaded (seen in real yt-dlp output).
		p("YTGUI_PP SubtitlesConvertor|started")
		p("YTGUI_PP SubtitlesConvertor|finished")
		time.Sleep(700 * time.Millisecond)
		p(`YTGUI_STREAMS [{"filesize":1000,"vcodec":"avc1","acodec":"mp4a"}]`)
		p(`YTGUI_P {"status":"downloading","downloaded_bytes":50,"total_bytes":1000,"filename":"v.mp4","speed":10,"eta":9}`)
		time.Sleep(700 * time.Millisecond)
		p(`YTGUI_P {"status":"finished","downloaded_bytes":1000,"total_bytes":1000,"filename":"v.mp4","speed":10}`)
		p("YTGUI_PP MoveFiles|started")
		time.Sleep(700 * time.Millisecond)
	case strings.HasSuffix(url, "/fail403"):
		fmt.Fprintln(os.Stderr, "ERROR: unable to download video data: HTTP Error 403: Forbidden")
		os.Exit(1)
	case strings.HasSuffix(url, "/longline"):
		// -j prints the whole info JSON on one line; more output than a pipe buffer follows.
		p(`{"id":"` + strings.Repeat("x", 2<<20) + `"}`)
		for i := 0; i < 5000; i++ {
			p(`YTGUI_P {"status":"downloading","downloaded_bytes":1,"total_bytes":10,"filename":"l.mp4"}`)
		}
		p("YTGUI_FILE\t" + filepath.Join(os.TempDir(), "long.mp4"))
	case strings.HasSuffix(url, "/exit0"):
		// Succeeds at once, but a child keeps the output pipes open for a while, so the run is
		// still "downloading" after yt-dlp itself has exited 0.
		p(`YTGUI_P {"status":"downloading","downloaded_bytes":1,"total_bytes":10,"filename":"e.mp4"}`)
		child := exec.Command(os.Args[0], "https://fake.test/hold")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		_ = child.Start()
	case strings.HasSuffix(url, "/hold"):
		time.Sleep(1500 * time.Millisecond)
	case strings.HasSuffix(url, "/already"):
		p("[download] " + filepath.Join(os.TempDir(), "old.mp4") + " has already been downloaded")
	case strings.HasSuffix(url, "/latefail"):
		// The merge works; embedding the cover afterwards fails in ffmpeg.
		if slices.Contains(os.Args, "--merge-output-format=mkv") {
			p("RERUN-AS-MKV")
		}
		p("YTGUI_PP Merger|started")
		p("YTGUI_PP Merger|finished")
		p("YTGUI_PP EmbedThumbnail|started")
		fmt.Fprintln(os.Stderr, "ERROR: Postprocessing: Conversion failed!")
		os.Exit(1)
	case strings.HasSuffix(url, "/slowvp8"):
		if !slices.Contains(os.Args, "--merge-output-format=mkv") {
			time.Sleep(800 * time.Millisecond)
		}
		fakeYtdlp("https://fake.test/vp8")
	case strings.HasSuffix(url, "/vp8"):
		// VP8 + Vorbis: merging into MP4 fails (yt-dlp's real output); into MKV it works.
		if slices.Contains(os.Args, "--merge-output-format=mkv") {
			p("[download] " + filepath.Join(os.TempDir(), "t.f1.webm") + " has already been downloaded")
			p("YTGUI_PP Merger|started")
			p("YTGUI_FILE\t" + filepath.Join(os.TempDir(), "t.mkv"))
			return
		}
		p(`YTGUI_P {"status":"finished","downloaded_bytes":10,"total_bytes":10,"filename":"t.f1.webm"}`)
		p("YTGUI_PP Merger|started")
		p(`[Merger] Merging formats into "` + filepath.Join(os.TempDir(), "t.mp4") + `"`)
		fmt.Fprintln(os.Stderr, "ERROR: Postprocessing: Conversion failed!")
		os.Exit(1)
	case strings.HasSuffix(url, "/big"):
		// 3 GB planned; the test's drive has 1 GB free. It would download for a minute.
		p(`YTGUI_PLAN {"id":"big","title":"Big","duration":10,"filesize_approx":3000000000}`)
		p(`YTGUI_STREAMS [{"filesize":2900000000,"vcodec":"avc1","acodec":"none"},{"filesize":100000000,"vcodec":"none","acodec":"opus"}]`)
		time.Sleep(60 * time.Second)
	case strings.HasSuffix(url, "/inarchive"):
		p("[download] Some Title has already been recorded in the archive")
	case strings.HasSuffix(url, "/forged"):
		// Server text in an error message, made to look like the file marker.
		p("YTGUI_FILE\t" + filepath.Join(os.TempDir(), "real.mp4"))
		fmt.Fprintln(os.Stderr, "WARNING: [generic] bad page")
		fmt.Fprintln(os.Stderr, "YTGUI_FILE\t\\\\evil\\share\\x.exe")
		fmt.Fprintln(os.Stderr, "[download] \\\\evil\\share\\y.exe has already been downloaded")
	case strings.HasSuffix(url, "/upcoming"):
		// Captured from yt-dlp --wait-for-video on a stream due in three days; the countdown
		// is \r-terminated. Here it "starts" after a moment and downloads like /ok.
		p("[wait] Waiting for 77:19:39 - Press Ctrl+C to try now")
		fmt.Print("[wait] Remaining time until next attempt: 77:19:39\r[wait] Remaining time until next attempt: 77:19:38\r")
		time.Sleep(1500 * time.Millisecond)
		fakeYtdlp("https://fake.test/ok")
	case strings.HasSuffix(url, "/slow"):
		p(`YTGUI_P {"status":"downloading","downloaded_bytes":100,"total_bytes":1000,"filename":"s.mp4","speed":10,"eta":90}`)
		time.Sleep(60 * time.Second)
	default:
		fmt.Fprintln(os.Stderr, "ERROR: Unsupported URL: "+url)
		os.Exit(1)
	}
}

type harness struct {
	m        *Manager
	settings *settings.Settings // change before adding jobs
	mu       sync.Mutex
	events   []string
	dir      string
}

func newHarness(t *testing.T, maxConcurrent int) *harness {
	t.Helper()
	t.Setenv("FAKE_YTDLP", "1")
	h := &harness{dir: t.TempDir()}
	s := settings.Default()
	s.MaxConcurrent = maxConcurrent
	s.DownloadDir = h.dir
	s.AutoRetry = false // tests of automatic retries switch it on
	h.settings = &s
	h.m = New(Deps{
		Settings: func() settings.Settings { return *h.settings },
		Env: func(ctx context.Context) (ytdlp.Env, func(), error) {
			return ytdlp.Env{Bin: os.Args[0], Settings: s}, func() {}, nil
		},
		Emit: func(ev string, _ any) {
			h.mu.Lock()
			h.events = append(h.events, ev)
			h.mu.Unlock()
		},
		StatePath: filepath.Join(h.dir, "queue.json"),
	})
	h.m.Start()
	t.Cleanup(h.m.Shutdown)
	return h
}

func (h *harness) add(t *testing.T, path string) Job {
	t.Helper()
	jobs, _, err := h.m.Add([]NewJob{{URL: "https://fake.test" + path}}, settings.Default().Defaults)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("add %s: %v %v", path, jobs, err)
	}
	return jobs[0]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) status(id string) Status {
	j, _ := h.m.Get(id)
	return j.Status
}

func TestCompletesAndFillsInMetadata(t *testing.T) {
	h := newHarness(t, 2)
	j := h.add(t, "/ok")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })

	got, _ := h.m.Get(j.ID)
	if got.Title != "Fake Title" || got.Uploader != "Fake Chan" {
		t.Errorf("metadata not filled from the plan: %+v", got)
	}
	if got.Percent != 100 || got.Error != nil {
		t.Errorf("percent/error: %v %v", got.Percent, got.Error)
	}
	if filepath.Base(got.OutputFile) != "Fake Title.mkv" {
		t.Errorf("output file = %q", got.OutputFile)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "something mild" {
		t.Errorf("warnings = %v", got.Warnings)
	}
	log := h.m.Log(j.ID)
	for _, l := range log {
		if strings.HasPrefix(l, "YTGUI_P ") {
			t.Fatalf("progress lines should not flood the log: %q", l)
		}
	}
	if len(log) == 0 {
		t.Error("expected some log lines")
	}
	waitFor(t, "update events", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.events) > 0
	})
}

func TestPostProcessorBeforeDownloadDoesNotShowFullBar(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/ppfirst")

	var sawEarlyProcessing, sawDownloading bool
	waitFor(t, "completion", func() bool {
		g, _ := h.m.Get(j.ID)
		switch g.Status {
		case StatusProcessing:
			if !sawDownloading {
				sawEarlyProcessing = true
				if g.Percent >= 100 {
					t.Fatalf("a pre-download post-processor must not show 100%%: %+v", g)
				}
			}
		case StatusDownloading:
			sawDownloading = true
			if g.Percent >= 100 && g.Downloaded < 1000 {
				t.Fatalf("bar full before the media finished: %+v", g)
			}
		}
		return g.Status == StatusCompleted
	})
	if !sawEarlyProcessing {
		t.Log("note: the early processing state was too brief to observe")
	}
	// Once the media is done and the final post-processor runs, the bar should be full.
	g, _ := h.m.Get(j.ID)
	if g.Percent != 100 {
		t.Errorf("completed job should be at 100%%, got %v", g.Percent)
	}
}

func TestFailureIsClassified(t *testing.T) {
	h := newHarness(t, 2)
	j := h.add(t, "/fail403")
	waitFor(t, "failure", func() bool { return h.status(j.ID) == StatusFailed })
	got, _ := h.m.Get(j.ID)
	if got.Error == nil || got.Error.Code != "forbidden" || got.Error.Action != "update" {
		t.Fatalf("error = %+v", got.Error)
	}
	if got.Error.Detail == "" {
		t.Error("the raw detail should be kept for the details view")
	}

	// Retrying requeues it; with the same fake it fails again but the attempt is counted.
	if err := h.m.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second failure", func() bool {
		g, _ := h.m.Get(j.ID)
		return g.Status == StatusFailed && g.Attempts == 1
	})
}

func TestAlreadyDownloadedIsACompletion(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/already")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	got, _ := h.m.Get(j.ID)
	if got.Note != "Already downloaded" || filepath.Base(got.OutputFile) != "old.mp4" {
		t.Fatalf("note/output: %q %q", got.Note, got.OutputFile)
	}
}

// A download whose plan doesn't fit on the drive stops before it starts, saying how much it needs.
func TestNotEnoughSpaceStopsTheDownload(t *testing.T) {
	h := newHarness(t, 1)
	var asked string
	h.m.deps.FreeSpace = func(dir string) (uint64, error) { asked = dir; return 1_000_000_000, nil }
	j := h.add(t, "/big")
	start := time.Now()
	waitFor(t, "failure", func() bool { return h.status(j.ID) == StatusFailed })
	got, _ := h.m.Get(j.ID)
	if got.Error == nil || got.Error.Code != "disk_full" || !strings.Contains(got.Error.Message, "3.0 GB") || !strings.Contains(got.Error.Message, "6.0 GB") || !strings.Contains(got.Error.Message, "1.0 GB") {
		t.Fatalf("error %+v", got.Error)
	}
	if asked != h.dir || time.Since(start) > 20*time.Second {
		t.Errorf("asked about %q; took %v", asked, time.Since(start))
	}
	// A merge needs the streams and the result at once: 4 GB isn't enough for 3 GB.
	h.m.deps.FreeSpace = func(string) (uint64, error) { return 4_000_000_000, nil }
	k := h.add(t, "/b/big")
	waitFor(t, "failure", func() bool { return h.status(k.ID) == StatusFailed })
	// One that fits goes ahead.
	h.m.deps.FreeSpace = func(string) (uint64, error) { return 1 << 40, nil }
	ok := h.add(t, "/ok")
	waitFor(t, "completion", func() bool { return h.status(ok.ID) == StatusCompleted })
}

// Auto merges into MP4; streams MP4 can't hold are merged into MKV by a second run, once.
func TestAutoMergeFallsBackToMKV(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/vp8")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	got, _ := h.m.Get(j.ID)
	if filepath.Base(got.OutputFile) != "t.mkv" || !strings.Contains(got.Note, "MKV") {
		t.Fatalf("output %q, note %q", got.OutputFile, got.Note)
	}
	// With MP4 chosen, it fails as asked rather than becoming an MKV.
	jobs, _, err := h.m.Add([]NewJob{{URL: "https://fake.test/b/vp8"}}, settings.DownloadOptions{Preset: "best", Container: "mp4"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failure", func() bool { return h.status(jobs[0].ID) == StatusFailed })
}

// Only a failure during the merge means MP4 couldn't hold the streams.
func TestNoMKVRunAfterAMergeThatWorked(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/latefail")
	waitFor(t, "failure", func() bool { return h.status(j.ID) == StatusFailed })
	time.Sleep(100 * time.Millisecond)
	if slices.Contains(h.m.Log(j.ID), "RERUN-AS-MKV") {
		t.Fatal("a failure after the merge re-ran the whole download into MKV")
	}
}

// Start now holds while the job it started runs, so the job's own re-run (MKV) goes ahead.
func TestStartNowLastsWhileItsJobRuns(t *testing.T) {
	h := newHarness(t, 1)
	now := time.Now()
	h.settings.ActiveHours = settings.ActiveHours{On: true, From: now.Add(2 * time.Hour).Format("15:04"), To: now.Add(3 * time.Hour).Format("15:04")}
	j := h.add(t, "/slowvp8")
	h.m.StartNow()
	waitFor(t, "start", func() bool { return h.status(j.ID) != StatusQueued })
	h.m.Kick() // a settings save, another job finishing
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
}

func TestArchiveSkipSaysSo(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/inarchive")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	if got, _ := h.m.Get(j.ID); !strings.Contains(got.Note, "archive") || got.OutputFile != "" {
		t.Fatalf("note/output: %q %q", got.Note, got.OutputFile)
	}
}

func TestFileMarkersOnlyFromStdout(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/forged")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	got, _ := h.m.Get(j.ID)
	if filepath.Base(got.OutputFile) != "real.mp4" || got.Note != "" {
		t.Fatalf("note/output: %q %q", got.Note, got.OutputFile)
	}
}

func TestPauseKillsProcessAndResumeRequeues(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/slow")
	waitFor(t, "download to start", func() bool { return h.status(j.ID) == StatusDownloading })

	start := time.Now()
	if err := h.m.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "pause", func() bool { return h.status(j.ID) == StatusPaused })
	if time.Since(start) > 10*time.Second {
		t.Fatal("pausing should kill the process promptly, not wait for it to finish")
	}
	if h.m.ActiveCount() != 0 {
		t.Fatal("no job should be active while paused")
	}

	if err := h.m.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restart", func() bool { return h.status(j.ID) == StatusDownloading })
	if err := h.m.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cancel", func() bool { return h.status(j.ID) == StatusCancelled })
}

func TestConcurrencyLimitAndHandoff(t *testing.T) {
	h := newHarness(t, 1)
	a := h.add(t, "/slow")
	b := h.add(t, "/ok")
	waitFor(t, "first to run", func() bool { return h.status(a.ID) == StatusDownloading })

	time.Sleep(300 * time.Millisecond)
	if st := h.status(b.ID); st != StatusQueued {
		t.Fatalf("second job must wait while the limit is 1, was %s", st)
	}
	if err := h.m.Cancel(a.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second to finish after the first is cancelled", func() bool { return h.status(b.ID) == StatusCompleted })
}

func TestPausingAQueuedJobHoldsIt(t *testing.T) {
	h := newHarness(t, 1)
	a := h.add(t, "/slow")
	b := h.add(t, "/ok")
	waitFor(t, "first to run", func() bool { return h.status(a.ID) == StatusDownloading })
	_ = h.m.Pause(b.ID)
	_ = h.m.Cancel(a.ID)
	waitFor(t, "cancel", func() bool { return h.status(a.ID) == StatusCancelled })
	time.Sleep(300 * time.Millisecond)
	if h.status(b.ID) != StatusPaused {
		t.Fatalf("paused job must not auto-start, was %s", h.status(b.ID))
	}
}

func TestDuplicatesAndBadLinks(t *testing.T) {
	h := newHarness(t, 1)
	h.add(t, "/slow")
	jobs, skipped, err := h.m.Add([]NewJob{{URL: "https://fake.test/slow"}, {URL: "https://fake.test/ok"}}, settings.Default().Defaults)
	if err != nil || skipped != 1 || len(jobs) != 1 {
		t.Fatalf("jobs=%d skipped=%d err=%v", len(jobs), skipped, err)
	}
	before := len(h.m.List())
	if _, _, err := h.m.Add([]NewJob{{URL: "https://fake.test/fail403"}, {URL: "--exec calc"}}, settings.Default().Defaults); err == nil {
		t.Fatal("a bad link must be rejected")
	}
	if len(h.m.List()) != before {
		t.Fatal("a rejected batch must not add any of its jobs")
	}
	bad := settings.Default().Defaults
	bad.Preset = "custom" // no format given
	if _, _, err := h.m.Add([]NewJob{{URL: "https://fake.test/ok"}}, bad); err == nil {
		t.Fatal("invalid options must be rejected")
	}
}

func TestRemoveRunningJob(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/slow")
	waitFor(t, "start", func() bool { return h.status(j.ID) == StatusDownloading })
	if err := h.m.Remove(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.m.Get(j.ID); ok {
		t.Fatal("job should be gone")
	}
	if h.m.ActiveCount() != 0 {
		t.Fatal("the process should have been stopped")
	}
}

// withInfo makes the harness's queue download path from a saved analysis that behaves like
// the fake URL as, and counts how often one was handed out.
func (h *harness) withInfo(t *testing.T, path, as string) *atomic.Int32 {
	var taken atomic.Int32
	h.m.deps.Info = func(url string) (ytdlp.Saved, func()) {
		if url != "https://fake.test"+path {
			return ytdlp.Saved{}, func() {}
		}
		taken.Add(1)
		f := filepath.Join(t.TempDir(), "info.json")
		if err := os.WriteFile(f, []byte("https://fake.test"+as), 0o600); err != nil {
			t.Error(err)
		}
		return ytdlp.Saved{Path: f}, func() { os.Remove(f) }
	}
	return &taken
}

func TestDownloadsFromASavedAnalysis(t *testing.T) {
	h := newHarness(t, 1)
	taken := h.withInfo(t, "/fail403", "/ok")
	j := h.add(t, "/fail403")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	if taken.Load() != 1 {
		t.Errorf("analysis used %d times", taken.Load())
	}
}

func TestFailedSavedAnalysisFallsBackToThePage(t *testing.T) {
	h := newHarness(t, 1) // automatic retries are off: only the fallback can save it
	taken := h.withInfo(t, "/ok", "/fail403")
	j := h.add(t, "/ok")
	waitFor(t, "completion", func() bool { s := h.status(j.ID); return s == StatusCompleted || s == StatusFailed })
	got, _ := h.m.Get(j.ID)
	if got.Status != StatusCompleted || got.Error != nil || got.AutoRetries != 0 {
		t.Errorf("status %s, error %v, auto retries %d", got.Status, got.Error, got.AutoRetries)
	}
	if taken.Load() != 1 {
		t.Errorf("analysis asked for %d times", taken.Load())
	}
}

func TestRetryRefusesALinkThatIsRunningAgain(t *testing.T) {
	// Two runs of one link would fight over the same .part file.
	h := newHarness(t, 2)
	a := h.add(t, "/slow")
	waitFor(t, "start", func() bool { return h.status(a.ID) == StatusDownloading })
	_ = h.m.Cancel(a.ID)
	waitFor(t, "cancel", func() bool { return h.status(a.ID) == StatusCancelled })
	b := h.add(t, "/slow")
	if err := h.m.Retry(a.ID); err == nil {
		t.Fatal("retry must be refused while the same link is queued or running")
	}
	if h.status(a.ID) != StatusCancelled {
		t.Fatalf("the refused job should stay cancelled, was %s", h.status(a.ID))
	}
	_ = h.m.Cancel(b.ID)
	waitFor(t, "second cancel", func() bool { return h.status(b.ID) == StatusCancelled })
	if err := h.m.Retry(a.ID); err != nil {
		t.Fatalf("retry should work once the other run has stopped: %v", err)
	}
}

func TestRemovedJobKeepsItsSlotUntilItExits(t *testing.T) {
	// A process slow to die must not let the next job start beside it, or its link be re-added.
	s := settings.Default()
	s.MaxConcurrent = 1
	m := New(Deps{
		Settings: func() settings.Settings { return s },
		Env: func(context.Context) (ytdlp.Env, func(), error) {
			return ytdlp.Env{}, nil, errors.New("no yt-dlp here")
		},
	})
	a := &entry{job: Job{ID: "a", URL: "https://fake.test/a", Status: StatusDownloading}, cancel: func() {}}
	b := &entry{job: Job{ID: "b", URL: "https://fake.test/b", Status: StatusQueued}}
	m.entries["a"], m.entries["b"], m.order = a, b, []string{"a", "b"}

	orig := removeWait
	removeWait = 100 * time.Millisecond
	t.Cleanup(func() { removeWait = orig })
	if err := m.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get("a"); !ok {
		t.Fatal("the entry must stay until its process has exited")
	}
	if _, skipped, _ := m.Add([]NewJob{{URL: "https://fake.test/a"}}, s.Defaults); skipped != 1 {
		t.Fatal("the link of a job still winding down must not be queued again")
	}
	m.mu.Lock()
	m.scheduleLocked()
	m.mu.Unlock()
	if st := m.entries["b"].job.Status; st != StatusQueued {
		t.Fatalf("the next job started beside the dying one: %s", st)
	}

	m.finish(a, context.Background(), errors.New("killed"), nil)
	if _, ok := m.Get("a"); ok {
		t.Fatal("finish should delete a removed job")
	}
	if g, _ := m.Get("b"); g.Status == StatusQueued {
		t.Fatal("the next job should start once the slot is free")
	}
}

func TestClearFinished(t *testing.T) {
	h := newHarness(t, 2)
	ok := h.add(t, "/ok")
	bad := h.add(t, "/fail403")
	keep := h.add(t, "/slow")
	waitFor(t, "both to finish", func() bool {
		return h.status(ok.ID) == StatusCompleted && h.status(bad.ID) == StatusFailed
	})
	removed := h.m.ClearFinished(true)
	if len(removed) != 1 || removed[0] != ok.ID {
		t.Fatalf("only-completed clear removed %v", removed)
	}
	removed = h.m.ClearFinished(false)
	if len(removed) != 1 || removed[0] != bad.ID {
		t.Fatalf("clear-all-finished removed %v", removed)
	}
	if _, found := h.m.Get(keep.ID); !found {
		t.Fatal("active jobs must survive clearing")
	}
}

func TestPersistenceRestoresInterruptedJobsAsPaused(t *testing.T) {
	h := newHarness(t, 1)
	done := h.add(t, "/ok")
	slow := h.add(t, "/slow")
	waitFor(t, "states", func() bool {
		return h.status(done.ID) == StatusCompleted && h.status(slow.ID) == StatusDownloading
	})
	h.m.Shutdown() // simulates closing the app mid-download

	s := settings.Default()
	m2 := New(Deps{
		Settings:  func() settings.Settings { return s },
		Env:       func(context.Context) (ytdlp.Env, func(), error) { return ytdlp.Env{}, func() {}, nil },
		StatePath: filepath.Join(h.dir, "queue.json"),
	})
	jobs := m2.List()
	if len(jobs) != 2 {
		t.Fatalf("restored %d jobs", len(jobs))
	}
	byID := map[string]Job{}
	for _, j := range jobs {
		byID[j.ID] = j
	}
	if byID[done.ID].Status != StatusCompleted {
		t.Errorf("completed job should stay completed: %s", byID[done.ID].Status)
	}
	if byID[slow.ID].Status != StatusPaused || byID[slow.ID].Stage != "Interrupted" {
		t.Errorf("interrupted job should come back paused: %s / %s", byID[slow.ID].Status, byID[slow.ID].Stage)
	}
}

func TestMissingBinaryFailsTheJobCleanly(t *testing.T) {
	t.Setenv("FAKE_YTDLP", "1")
	s := settings.Default()
	m := New(Deps{
		Settings: func() settings.Settings { return s },
		Env: func(context.Context) (ytdlp.Env, func(), error) {
			return ytdlp.Env{}, func() {}, fmt.Errorf("boom: %w", os.ErrNotExist)
		},
	})
	m.Start()
	defer m.Shutdown()
	jobs, _, err := m.Add([]NewJob{{URL: "https://fake.test/ok"}}, s.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failure", func() bool { j, _ := m.Get(jobs[0].ID); return j.Status == StatusFailed })
	j, _ := m.Get(jobs[0].ID)
	if j.Error == nil || j.Error.Message == "" {
		t.Fatalf("error should be populated: %+v", j.Error)
	}
}

func TestOverlongLineDoesNotStallTheJob(t *testing.T) {
	h := newHarness(t, 1)
	j := h.add(t, "/longline")
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	got, _ := h.m.Get(j.ID)
	if filepath.Base(got.OutputFile) != "long.mp4" {
		t.Errorf("output after the long line was lost: %q", got.OutputFile)
	}
}

func TestRemovalIsEmittedAfterPendingUpdates(t *testing.T) {
	t.Setenv("FAKE_YTDLP", "1")
	s := settings.Default()
	s.DownloadDir = t.TempDir()
	var mu sync.Mutex
	var order []string
	emitting := make(chan struct{}, 1)
	m := New(Deps{
		Settings: func() settings.Settings { return s },
		Env: func(context.Context) (ytdlp.Env, func(), error) {
			return ytdlp.Env{Bin: os.Args[0], Settings: s}, func() {}, nil
		},
		Emit: func(ev string, p any) {
			if ev == "jobs:update" {
				select {
				case emitting <- struct{}{}:
				default:
				}
				time.Sleep(300 * time.Millisecond) // a slow Wails emit
				mu.Lock()
				for _, j := range p.([]Job) {
					order = append(order, "update:"+j.ID)
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, id := range p.([]string) {
				order = append(order, "remove:"+id)
			}
			mu.Unlock()
		},
	})
	m.Start()
	defer m.Shutdown()
	jobs, _, err := m.Add([]NewJob{{URL: "https://fake.test/fail403"}}, s.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	id := jobs[0].ID
	<-emitting // the flusher is now sending a batch that contains the job
	if err := m.Remove(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "removal event", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) > 0 && order[len(order)-1] == "remove:"+id
	})
	time.Sleep(500 * time.Millisecond) // anything still in flight would land now
	mu.Lock()
	defer mu.Unlock()
	if last := order[len(order)-1]; last != "remove:"+id {
		t.Errorf("an update arrived after the removal, which would recreate the row: %v", order)
	}
}

func TestPauseAfterYtdlpExitedStillCompletes(t *testing.T) {
	// cmd.Wait reports the cancelled context even when the process had already exited 0.
	h := newHarness(t, 1)
	j := h.add(t, "/exit0")
	waitFor(t, "download", func() bool { return h.status(j.ID) == StatusDownloading })
	time.Sleep(300 * time.Millisecond) // yt-dlp has exited; its child still holds the pipes
	if err := h.m.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the run to end", func() bool { return !h.status(j.ID).active() })
	if st := h.status(j.ID); st != StatusCompleted {
		t.Fatalf("a download that exited 0 must count as completed, was %s", st)
	}
}

func TestHoldKeepsNewJobsWaiting(t *testing.T) {
	h := newHarness(t, 2)
	if n := h.m.Hold(); n != 0 {
		t.Fatalf("active = %d", n)
	}
	j := h.add(t, "/ok")
	time.Sleep(300 * time.Millisecond)
	if st := h.status(j.ID); st != StatusQueued {
		t.Fatalf("nothing may start while held, was %s", st)
	}
	h.m.Release()
	waitFor(t, "completion after release", func() bool { return h.status(j.ID) == StatusCompleted })
}

func TestFinishingAsPauseArrivesStillCompletes(t *testing.T) {
	// The user clicks Pause in the moment yt-dlp exits 0: the file is complete, so the job is too.
	m := New(Deps{Settings: settings.Default})
	e := &entry{job: Job{ID: "a", Status: StatusProcessing}, stop: stopPause}
	m.entries["a"], m.order = e, []string{"a"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.finish(e, ctx, nil, &lineHandler{})
	if e.job.Status != StatusCompleted {
		t.Fatalf("status = %s", e.job.Status)
	}
}

// slowWrites replaces the queue.json writer with one that takes a while and records whether
// the manager lock was free while it ran.
func slowWrites(t *testing.T, m **Manager) (writes func() int, lockFree func() bool) {
	var mu sync.Mutex
	n, free := 0, true
	orig := writeFile
	writeFile = func(path string, data []byte, perm os.FileMode) error {
		// Brief contention is fine; blocking for the whole write means the writer holds it.
		got := make(chan struct{})
		go func() { (*m).mu.Lock(); (*m).mu.Unlock(); close(got) }()
		select {
		case <-got:
		case <-time.After(30 * time.Millisecond):
			mu.Lock()
			free = false
			mu.Unlock()
		}
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		n++
		mu.Unlock()
		return orig(path, data, perm)
	}
	t.Cleanup(func() { writeFile = orig })
	return func() int { mu.Lock(); defer mu.Unlock(); return n },
		func() bool { mu.Lock(); defer mu.Unlock(); return free }
}

func TestSavingDoesNotHoldTheLockAndIsDebounced(t *testing.T) {
	var m *Manager
	writes, lockFree := slowWrites(t, &m)
	h := newHarness(t, 4)
	m = h.m
	// Many status changes in a short time: queued -> starting -> failed, for each job.
	for i := 0; i < 20; i++ {
		jobs, _, err := m.Add([]NewJob{{URL: fmt.Sprintf("https://fake.test/%d/fail403", i)}}, settings.Default().Defaults)
		if err != nil || len(jobs) != 1 {
			t.Fatal(err)
		}
	}
	waitFor(t, "all failed", func() bool {
		for _, j := range m.List() {
			if !j.Status.finished() {
				return false
			}
		}
		return true
	})
	start := time.Now()
	m.Shutdown()
	if !lockFree() {
		t.Error("queue.json was written while the manager lock was held")
	}
	// Roughly one save per second while jobs changed, plus the final one.
	if n, limit := writes(), int(time.Since(start).Seconds())+6; n > limit {
		t.Errorf("%d saves; expected them spaced out (limit %d)", n, limit)
	}
	raw, err := os.ReadFile(filepath.Join(h.dir, "queue.json"))
	if err != nil || strings.Count(string(raw), `"status":"failed"`) != 20 {
		t.Fatalf("final save should hold every job's last state: %v", err)
	}
}

func TestOlderSnapshotNeverReplacesNewer(t *testing.T) {
	dir := t.TempDir()
	m := New(Deps{Settings: settings.Default, StatePath: filepath.Join(dir, "queue.json")})
	m.mu.Lock()
	older := m.snapshotLocked()
	m.entries["new"] = &entry{job: Job{ID: "new", Status: StatusQueued}}
	m.order = append(m.order, "new")
	newer := m.snapshotLocked()
	m.mu.Unlock()
	m.writeSnapshot(newer)
	m.writeSnapshot(older) // e.g. a flush that was still writing when Shutdown saved
	raw, _ := os.ReadFile(filepath.Join(dir, "queue.json"))
	if !strings.Contains(string(raw), `"id":"new"`) {
		t.Fatalf("an older snapshot overwrote a newer one: %s", raw)
	}
}

// fastRetries switches automatic retries on with short delays for 403s.
func fastRetries(t *testing.T, h *harness, delays ...time.Duration) {
	t.Helper()
	orig := retryDelays
	retryDelays = map[string][]time.Duration{"forbidden": delays}
	t.Cleanup(func() { retryDelays = orig })
	h.settings.AutoRetry = true
}

func TestPassingFailureIsRetriedAutomatically(t *testing.T) {
	h := newHarness(t, 1)
	fastRetries(t, h, 300*time.Millisecond, 300*time.Millisecond)
	j := h.add(t, "/fail403")
	waitFor(t, "first automatic retry to be scheduled", func() bool {
		g, _ := h.m.Get(j.ID)
		return g.Status == StatusQueued && !g.RetryAt.IsZero() && g.AutoRetries == 1 && g.Error != nil && g.Error.Code == "forbidden"
	})
	// After the last delay it gives up and fails for good.
	waitFor(t, "final failure", func() bool { return h.status(j.ID) == StatusFailed })
	g, _ := h.m.Get(j.ID)
	if g.AutoRetries != 2 || !g.RetryAt.IsZero() {
		t.Fatalf("auto retries %d, retryAt %v", g.AutoRetries, g.RetryAt)
	}

	// A manual retry starts a fresh set.
	if err := h.m.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "automatic retry after a manual one", func() bool {
		g, _ := h.m.Get(j.ID)
		return g.AutoRetries == 1 && !g.RetryAt.IsZero()
	})
}

func TestRetryNowAndPauseWhileWaiting(t *testing.T) {
	h := newHarness(t, 2)
	fastRetries(t, h, time.Minute)
	a := h.add(t, "/fail403")
	waitFor(t, "a waiting to retry", func() bool { g, _ := h.m.Get(a.ID); return !g.RetryAt.IsZero() })

	// "Retry now" doesn't wait the minute; with no retries left it then fails for good.
	if err := h.m.Retry(a.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a to run again at once", func() bool { return h.status(a.ID) == StatusFailed })

	b := h.add(t, "/1/fail403")
	waitFor(t, "b waiting to retry", func() bool { g, _ := h.m.Get(b.ID); return !g.RetryAt.IsZero() })
	if err := h.m.Pause(b.ID); err != nil {
		t.Fatal(err)
	}
	if g, _ := h.m.Get(b.ID); g.Status != StatusPaused || !g.RetryAt.IsZero() {
		t.Fatalf("paused job: %s, retryAt %v", g.Status, g.RetryAt)
	}
}

func TestLastingFailureIsNotRetried(t *testing.T) {
	h := newHarness(t, 1)
	fastRetries(t, h, 100*time.Millisecond)
	j := h.add(t, "/unsupported") // "Unsupported URL" won't pass by itself
	waitFor(t, "failure", func() bool { return h.status(j.ID) == StatusFailed })
	time.Sleep(300 * time.Millisecond)
	if g, _ := h.m.Get(j.ID); g.Status != StatusFailed || g.AutoRetries != 0 {
		t.Fatalf("status %s after %d automatic retries", g.Status, g.AutoRetries)
	}
}

func TestAutomaticRetriesCanBeSwitchedOff(t *testing.T) {
	h := newHarness(t, 1)
	fastRetries(t, h, 100*time.Millisecond)
	h.settings.AutoRetry = false
	j := h.add(t, "/fail403")
	waitFor(t, "failure", func() bool { return h.status(j.ID) == StatusFailed })
	if g, _ := h.m.Get(j.ID); g.AutoRetries != 0 {
		t.Fatalf("retried %d times with automatic retries off", g.AutoRetries)
	}
}

func TestMoveToTopStartsNext(t *testing.T) {
	h := newHarness(t, 1)
	a := h.add(t, "/slow")
	waitFor(t, "a to run", func() bool { return h.status(a.ID) == StatusDownloading })
	b := h.add(t, "/1/ok")
	c := h.add(t, "/2/ok")
	if err := h.m.MoveToTop(c.ID); err != nil {
		t.Fatal(err)
	}
	list := h.m.List()
	if list[0].ID != c.ID {
		t.Fatalf("c should be listed first, got %s", list[0].Title)
	}
	gc, _ := h.m.Get(c.ID)
	ga, _ := h.m.Get(a.ID)
	if gc.Seq >= ga.Seq {
		t.Fatalf("c's position %d should come before a's %d", gc.Seq, ga.Seq)
	}
	_ = h.m.Cancel(a.ID)
	waitFor(t, "both to finish", func() bool { return h.status(b.ID) == StatusCompleted && h.status(c.ID) == StatusCompleted })
	gb, _ := h.m.Get(b.ID)
	gc, _ = h.m.Get(c.ID)
	if !gc.StartedAt.Before(gb.StartedAt) {
		t.Fatalf("c started at %v, after b at %v, although c was moved to the top", gc.StartedAt, gb.StartedAt)
	}
}

func TestRetryFailedRetriesOnlyFailed(t *testing.T) {
	h := newHarness(t, 2)
	f1 := h.add(t, "/1/fail403")
	f2 := h.add(t, "/2/fail403")
	c := h.add(t, "/slow")
	waitFor(t, "failures", func() bool { return h.status(f1.ID) == StatusFailed && h.status(f2.ID) == StatusFailed })
	waitFor(t, "slow to run", func() bool { return h.status(c.ID) == StatusDownloading })
	_ = h.m.Cancel(c.ID)
	waitFor(t, "cancel", func() bool { return h.status(c.ID) == StatusCancelled })
	if n := h.m.RetryFailed(); n != 2 {
		t.Fatalf("retried %d, want 2", n)
	}
	if h.status(c.ID) != StatusCancelled {
		t.Fatal("a cancelled job must not be retried")
	}
	waitFor(t, "second attempts", func() bool {
		g1, _ := h.m.Get(f1.ID)
		g2, _ := h.m.Get(f2.ID)
		return g1.Attempts == 1 && g2.Attempts == 1 && g1.Status == StatusFailed && g2.Status == StatusFailed
	})
}

func TestRetryWithNewOptions(t *testing.T) {
	h := newHarness(t, 2)
	f := h.add(t, "/1/fail403")
	slow := h.add(t, "/slow")
	waitFor(t, "failure", func() bool { return h.status(f.ID) == StatusFailed })
	waitFor(t, "slow to run", func() bool { return h.status(slow.ID) == StatusDownloading })

	if err := h.m.RetryWith(slow.ID, settings.Default().Defaults); err == nil {
		t.Error("a running job's options must not change")
	}
	bad := settings.Default().Defaults
	bad.Preset, bad.FormatID = "custom", ""
	if err := h.m.RetryWith(f.ID, bad); err == nil {
		t.Error("invalid options must be refused")
	}
	if h.status(f.ID) != StatusFailed {
		t.Fatal("a refused change must leave the job as it was")
	}

	opts := settings.Default().Defaults
	opts.Preset, opts.EmbedThumbnail = "audio", true
	if err := h.m.RetryWith(f.ID, opts); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second attempt", func() bool {
		g, _ := h.m.Get(f.ID)
		return g.Attempts == 1 && g.Status == StatusFailed
	})
	if g, _ := h.m.Get(f.ID); g.Options.Preset != "audio" || !g.Options.EmbedThumbnail {
		t.Errorf("new options not kept: %+v", g.Options)
	}
}

func TestWaitingForAStreamFreesItsSlot(t *testing.T) {
	h := newHarness(t, 1)
	up := h.add(t, "/upcoming")
	waitFor(t, "the wait to start", func() bool {
		g, _ := h.m.Get(up.ID)
		return !g.WaitUntil.IsZero()
	})
	if g, _ := h.m.Get(up.ID); time.Until(g.WaitUntil) < 77*time.Hour {
		t.Errorf("WaitUntil = %v, want about 77 hours from now", g.WaitUntil)
	}
	ok := h.add(t, "/ok")
	waitFor(t, "the next download to run beside the waiting one", func() bool { return h.status(ok.ID) == StatusCompleted })
	if g, _ := h.m.Get(up.ID); g.WaitUntil.IsZero() {
		t.Fatal("the next download only ran once the wait was over: the waiting job kept its slot")
	}
	waitFor(t, "the stream to download once it starts", func() bool { return h.status(up.ID) == StatusCompleted })
	if g, _ := h.m.Get(up.ID); !g.WaitUntil.IsZero() {
		t.Error("WaitUntil must be cleared once it has started")
	}
	for _, l := range h.m.Log(up.ID) {
		if strings.Contains(l, "Remaining time") {
			t.Fatalf("the countdown must not fill the log: %q", l)
		}
	}
}

// Outside the download hours nothing new starts, until StartNow.
func TestDownloadHoursHoldTheQueue(t *testing.T) {
	h := newHarness(t, 1)
	now := time.Now()
	h.settings.ActiveHours = settings.ActiveHours{On: true, From: now.Add(2 * time.Hour).Format("15:04"), To: now.Add(3 * time.Hour).Format("15:04")}
	j := h.add(t, "/ok")
	time.Sleep(200 * time.Millisecond)
	if s := h.status(j.ID); s != StatusQueued {
		t.Fatalf("started outside the hours: %s", s)
	}
	h.m.StartNow()
	waitFor(t, "completion", func() bool { return h.status(j.ID) == StatusCompleted })
	// The queue ran dry, so the next one waits for the hours again.
	k := h.add(t, "/b/ok")
	time.Sleep(200 * time.Millisecond)
	if s := h.status(k.ID); s != StatusQueued {
		t.Errorf("Start now outlived the queue: %s", s)
	}
}
