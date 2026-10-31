package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// Shutdown puts a job that waits for a scheduled stream "back in line" (queued), but load turns
// every queued job into paused "Interrupted", so it no longer waits after a restart.
func TestWaitingStreamWaitsAgainAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	m := New(Deps{Settings: settings.Default, StatePath: path})
	j := Job{ID: "w", Seq: 1, URL: "https://www.youtube.com/watch?v=x", Status: StatusStarting,
		Stage: "Waiting for the stream to start", WaitUntil: time.Now().Add(time.Hour), Warnings: []string{}}
	j.Options.WaitForVideo = true
	m.entries["w"] = &entry{job: j}
	m.order = []string{"w"}
	m.Shutdown()
	m2 := New(Deps{Settings: settings.Default, StatePath: path})
	got, _ := m2.Get("w")
	if got.Status != StatusQueued {
		t.Errorf("restored as %s/%q, want it queued to wait again", got.Status, got.Stage)
	}
}

// A crash while a job waits for a stream leaves WaitUntil in queue.json. load keeps it, so the
// resumed job doesn't count against the limit and a second download starts beside it.
func TestCrashMidWaitKeepsTheLimit(t *testing.T) {
	t.Setenv("FAKE_YTDLP", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	crashed := `{"version":1,"jobs":[{"id":"w","seq":1,"url":"https://fake.test/slow","status":"starting","stage":"Waiting for the stream to start","waitUntil":"` +
		time.Now().Add(time.Hour).Format(time.RFC3339) + `","options":{"preset":"best"},"warnings":[]}]}`
	if err := os.WriteFile(path, []byte(crashed), 0o600); err != nil {
		t.Fatal(err)
	}
	s := settings.Default()
	s.MaxConcurrent = 1
	s.DownloadDir = dir
	m := New(Deps{
		Settings: func() settings.Settings { return s },
		Env: func(ctx context.Context) (ytdlp.Env, func(), error) {
			return ytdlp.Env{Bin: os.Args[0], Settings: s}, func() {}, nil
		},
		StatePath: path,
	})
	m.Start()
	defer m.Shutdown()
	if j, _ := m.Get("w"); !j.WaitUntil.IsZero() {
		t.Errorf("restored job still carries WaitUntil %v (status %s)", j.WaitUntil, j.Status)
	}
	_ = m.Resume("w")
	jobs, _, _ := m.Add([]NewJob{{URL: "https://fake.test/b/slow"}}, settings.Default().Defaults)
	time.Sleep(300 * time.Millisecond)
	if n := m.ActiveCount(); n > 1 {
		t.Errorf("%d downloads run with a limit of 1 (second: %s)", n, jobs[0].ID)
	}
}

// One video under two link forms is queued twice, and both runs write the same file.
func TestSameVideoUnderTwoLinksQueuedOnce(t *testing.T) {
	m := New(Deps{Settings: settings.Default})
	m.Hold() // nothing starts
	added, skipped, err := m.Add([]NewJob{
		{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{URL: "https://youtu.be/dQw4w9WgXcQ?si=abc"},
		{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s"},
	}, settings.Default().Defaults)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Errorf("%d jobs for one video (%d skipped)", len(added), skipped)
	}
}
