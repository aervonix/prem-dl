package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in: downloads the real yt-dlp release into a temp dir.
//
//	$env:YTGUI_INTEGRATION = "1"; go test ./internal/ytdlp -run InstallIntegration -v
func TestInstallIntegration(t *testing.T) {
	if os.Getenv("YTGUI_INTEGRATION") != "1" {
		t.Skip("set YTGUI_INTEGRATION=1 to download a real yt-dlp release")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	client := HTTPClient("", 4*time.Minute)

	dest := filepath.Join(t.TempDir(), "bin", Name())
	var last int64
	v, err := Install(ctx, client, "stable", dest, func(done, total int64) { last = done })
	if err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	t.Logf("installed %s (%d bytes downloaded)", v, last)
	if got, err := Version(ctx, dest); err != nil || got != v {
		t.Fatalf("installed binary reports %q, %v; want %q", got, err, v)
	}

	// Installing over an existing file replaces it and leaves no stray files behind.
	if _, err := Install(ctx, client, "stable", dest, nil); err != nil {
		t.Fatalf("reinstall over existing: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Fatalf("expected only the binary after reinstall, found %d entries: %v", len(entries), entries)
	}

	latest, err := LatestVersion(ctx, client, "stable")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	t.Logf("latest stable tag: %s", latest)
	if CompareVersions(v, latest) < 0 {
		t.Errorf("installed %s is older than the latest tag %s", v, latest)
	}
}

// Opt-in: downloads yt-dlp's ffmpeg build (about 90 MB) into a temp dir, twice.
//
//	$env:YTGUI_INTEGRATION = "1"; go test ./internal/ytdlp -run FFmpegInstallIntegration -v
func TestFFmpegInstallIntegration(t *testing.T) {
	if os.Getenv("YTGUI_INTEGRATION") != "1" {
		t.Skip("set YTGUI_INTEGRATION=1 to download a real ffmpeg build")
	}
	if !CanInstallFFmpeg() {
		t.Skip("no ffmpeg build for this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := HTTPClient("", 10*time.Minute)
	managed := t.TempDir()

	var last, total int64
	v, err := InstallFFmpeg(ctx, client, managed, func(done, tot int64) { last, total = done, tot })
	if err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	t.Logf("installed ffmpeg %s (%d of %d bytes)", v, last, total)
	ff := FindFFmpeg(ctx, "", "", managed)
	if ff.Version != v || filepath.Dir(ff.Path) != FFmpegDir(managed) {
		t.Fatalf("detection found %+v, want version %s in %s", ff, v, FFmpegDir(managed))
	}

	// Installing again replaces the folder and leaves nothing beside it.
	if _, err := InstallFFmpeg(ctx, client, managed, nil); err != nil {
		t.Fatalf("reinstall over existing: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(managed, "bin"))
	if len(entries) != 1 || entries[0].Name() != "ffmpeg" {
		t.Fatalf("expected only the ffmpeg folder after reinstall, found %v", entries)
	}
}
