//go:build bundled

package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// The tools really built into the exe unpack and run (go test -tags bundled ., after go run ./cmd/bundle).
func TestBuiltInToolsUnpack(t *testing.T) {
	want, err := fs.ReadFile(bundleFS, "yt-dlp.version")
	if err != nil {
		t.Fatal(err)
	}
	_, ffErr := fs.Stat(bundleFS, "ffmpeg.zip")
	// Nowhere else to find either: not the repository's yt-dlp.exe, not one on PATH.
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")

	dir := t.TempDir()
	store, err := settings.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: dir, store: store, versions: map[versionKey]string{}}
	a.unpackBundle(context.Background())

	bin, err := ytdlp.Resolve("", dir)
	if err != nil || bin.Source != ytdlp.SourceManaged {
		t.Fatalf("yt-dlp: %+v, %v", bin, err)
	}
	if v := a.versionOf(context.Background(), bin.Path); v != strings.TrimSpace(string(want)) {
		t.Errorf("yt-dlp %q, want %q", v, want)
	}
	if ffErr != nil {
		return // built without ffmpeg
	}
	ff := ytdlp.FindFFmpeg(context.Background(), "", bin.Path, dir)
	if !strings.EqualFold(filepath.Dir(ff.Path), ytdlp.FFmpegDir(dir)) || ff.Version == "" {
		t.Fatalf("ffmpeg: %+v", ff)
	}
	t.Logf("unpacked yt-dlp %s and ffmpeg %s", want, ff.Version)
}
