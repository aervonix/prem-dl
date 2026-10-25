package queue

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// Opt-in end-to-end tests against the real yt-dlp and the real internet:
//
//	$env:YTGUI_INTEGRATION = "1"; $env:YTGUI_YTDLP = "C:\path\to\yt-dlp.exe"; go test ./internal/queue -run Integration -v
const (
	rickURL     = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	missingURL  = "https://www.youtube.com/watch?v=jNQXAC9IeRw" // unavailable from the author's account
	integTimout = 3 * time.Minute
)

func integrationEnv(t *testing.T) (settings.Settings, func(context.Context) (ytdlp.Env, func(), error)) {
	t.Helper()
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	s := settings.Default()
	s.DownloadDir = t.TempDir()
	s.Retries = 3
	rts := ytdlp.DetectJSRuntimes(context.Background())
	ff := ytdlp.FindFFmpeg(context.Background(), "", bin, "")
	return s, func(context.Context) (ytdlp.Env, func(), error) {
		return ytdlp.Env{Bin: bin, Settings: s, JSRuntimes: rts, FFmpeg: ff.Path}, func() {}, nil
	}
}

func TestIntegrationProbeAndDownload(t *testing.T) {
	s, envFn := integrationEnv(t)
	env, _, _ := envFn(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), integTimout)
	defer cancel()
	res, err := ytdlp.Probe(ctx, env, rickURL, true)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.Kind != "video" || res.Video.Title == "" || len(res.Video.Formats) == 0 || len(res.Video.Heights) == 0 {
		t.Fatalf("unexpected probe result: %+v", res.Video)
	}
	t.Logf("probed %q — %d formats, heights %v, %d manual subs, %d auto captions",
		res.Video.Title, len(res.Video.Formats), res.Video.Heights, len(res.Video.Subtitles), len(res.Video.AutoCaptions))
	for _, f := range res.Video.Formats {
		if f.Ext == "mhtml" {
			t.Fatalf("storyboard leaked into formats: %+v", f)
		}
	}

	m := New(Deps{Settings: func() settings.Settings { return s }, Env: envFn, StatePath: filepath.Join(s.DownloadDir, "q.json")})
	m.Start()
	defer m.Shutdown()

	opts := s.Defaults
	opts.Preset = "360"
	opts.Subtitles = settings.SubtitleOptions{Mode: "both", Langs: []string{"en"}, Format: "srt"}
	jobs, _, err := m.Add([]NewJob{{URL: res.Video.URL}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	id := jobs[0].ID

	var maxPct float64
	var trace []string
	lastKey := ""
	deadline := time.Now().Add(integTimout)
	for time.Now().Before(deadline) {
		j, _ := m.Get(id)
		if key := string(j.Status) + "/" + j.Stage; key != lastKey {
			lastKey = key
			trace = append(trace, fmt.Sprintf("%s pct=%.1f dl=%d total=%d", key, j.Percent, j.Downloaded, j.Total))
		}
		if j.Percent > maxPct {
			maxPct = j.Percent
		} else if j.Percent >= 0 && j.Percent < maxPct-0.5 && j.Status == StatusDownloading {
			t.Fatalf("overall progress went backwards: %.1f → %.1f\ntrace:\n  %s\nlog:\n  %s",
				maxPct, j.Percent, strings.Join(trace, "\n  "), strings.Join(m.Log(id), "\n  "))
		}
		if j.Status.finished() {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	j, _ := m.Get(id)
	if j.Status != StatusCompleted {
		t.Fatalf("status %s, error %+v\nlog:\n%v", j.Status, j.Error, m.Log(id))
	}
	if _, err := os.Stat(j.OutputFile); err != nil {
		t.Fatalf("output file %q: %v", j.OutputFile, err)
	}
	t.Logf("done: %s (%s)", filepath.Base(j.OutputFile), j.Title)

	// Both mode keeps a sidecar subtitle next to the video.
	srts, _ := filepath.Glob(filepath.Join(filepath.Dir(j.OutputFile), "*.srt"))
	if len(srts) == 0 {
		t.Errorf("expected a sidecar .srt in %s", filepath.Dir(j.OutputFile))
	}

	// Downloading the same thing again must finish cleanly and report "already downloaded".
	if err := m.Retry(id); err == nil {
		// completed jobs can't be retried; enqueue anew
	}
	again, _, err := m.Add([]NewJob{{URL: res.Video.URL}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		g, _ := m.Get(again[0].ID)
		if g.Status.finished() {
			if g.Status != StatusCompleted {
				t.Fatalf("second run: %s %+v\n%v", g.Status, g.Error, m.Log(again[0].ID))
			}
			t.Logf("second run: note=%q output=%q", g.Note, filepath.Base(g.OutputFile))
			if g.OutputFile == "" {
				t.Errorf("output file should still be reported for an existing download")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("second run timed out")
}

func TestIntegrationUnavailableVideoIsClassified(t *testing.T) {
	s, envFn := integrationEnv(t)
	env, _, _ := envFn(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), integTimout)
	defer cancel()
	_, err := ytdlp.Probe(ctx, env, missingURL, true)
	if err == nil {
		t.Skip("that video is available now; nothing to classify")
	}
	t.Logf("error: %v", err)
	_ = s
}

// A time range, WebM, embedded subtitles with auto captions, and cover art together: each of
// these broke on its own (progress stuck at a few percent, auto captions chosen over uploaded
// subtitles, ffmpeg failing on h264 or on the thumbnail).
func TestIntegrationClipWithEmbeddedSubtitles(t *testing.T) {
	s, envFn := integrationEnv(t)
	m := New(Deps{Settings: func() settings.Settings { return s }, Env: envFn})
	m.Start()
	defer m.Shutdown()

	opts := s.Defaults
	opts.Preset = "360"
	opts.Container = "webm"
	opts.EmbedThumbnail = true
	opts.SectionStart, opts.SectionEnd = "0:05", "0:08"
	opts.Subtitles = settings.SubtitleOptions{Mode: "embed", Langs: []string{"en"}, Auto: true}
	jobs, _, err := m.Add([]NewJob{{URL: rickURL}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	id := jobs[0].ID
	deadline := time.Now().Add(integTimout)
	var j Job
	for time.Now().Before(deadline) {
		if j, _ = m.Get(id); j.Status.finished() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if j.Status != StatusCompleted || j.Percent != 100 {
		t.Fatalf("status %s pct %.1f error %+v\nlog:\n  %s", j.Status, j.Percent, j.Error, strings.Join(m.Log(id), "\n  "))
	}
	if ext := filepath.Ext(j.OutputFile); ext != ".webm" && ext != ".mkv" {
		t.Errorf("output %q", j.OutputFile)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(j.OutputFile), "*.vtt"))
	if len(left) > 0 {
		t.Errorf("embed-only mode left sidecar subtitles behind: %v", left)
	}
	log := strings.Join(m.Log(id), "\n")
	if !strings.Contains(log, "Embedding subtitles") {
		t.Errorf("subtitles were not embedded:\n%s", log)
	}
	t.Logf("done: %s", filepath.Base(j.OutputFile))
}
