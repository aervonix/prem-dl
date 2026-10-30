package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

func TestBundleInPlace(t *testing.T) {
	old := bundleFS
	defer func() { bundleFS = old }()
	bundleFS = fstest.MapFS{
		"yt-dlp.zip":     {Data: []byte("yt")},
		"yt-dlp.version": {Data: []byte("2026.10.21\n")},
		"ffmpeg.zip":     {Data: []byte("zip")},
	}
	stamp, err := bundleStamp()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stampPath := filepath.Join(dir, "bin", bundleStampName)
	write := func(p, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if bundleInPlace(dir, stampPath, stamp) {
		t.Error("an empty folder counts as unpacked")
	}
	write(ytdlp.ManagedPath(dir), "yt")
	write(filepath.Join(ytdlp.FFmpegDir(dir), ytdlp.FFmpegName()), "ff")
	write(filepath.Join(ytdlp.FFmpegDir(dir), ytdlp.FFprobeName()), "fp")
	write(stampPath, stamp+"\n")
	if !bundleInPlace(dir, stampPath, stamp) {
		t.Error("the same bundle is unpacked again")
	}

	// Another release's bundle replaces it, whether it's newer or older.
	bundleFS.(fstest.MapFS)["yt-dlp.zip"] = &fstest.MapFile{Data: []byte("other")}
	other, _ := bundleStamp()
	if other == stamp || bundleInPlace(dir, stampPath, other) {
		t.Error("a different yt-dlp keeps the old one")
	}
	// A program removed since (antivirus, a clean-up) is put back.
	os.Remove(filepath.Join(ytdlp.FFmpegDir(dir), ytdlp.FFprobeName()))
	if bundleInPlace(dir, stampPath, stamp) {
		t.Error("a missing ffprobe isn't noticed")
	}
}

// A build with the tools built in runs only its own copy, even with another yt-dlp.exe at hand.
func TestBuiltInRunsOnlyItsOwn(t *testing.T) {
	old := bundleFS
	defer func() { bundleFS = old }()
	bundleFS = fstest.MapFS{"yt-dlp.version": {Data: []byte("2026.10.21\n")}}

	wd := t.TempDir()
	t.Chdir(wd)
	if err := os.WriteFile(filepath.Join(wd, ytdlp.Name()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: t.TempDir()}
	if _, err := a.findYtdlp(); !errors.Is(err, ytdlp.ErrNotFound) {
		t.Fatalf("found %v before unpacking", err)
	}
	managed := ytdlp.ManagedPath(a.dataDir)
	os.MkdirAll(filepath.Dir(managed), 0o755)
	if err := os.WriteFile(managed, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if b, err := a.findYtdlp(); err != nil || b.Path != managed {
		t.Fatalf("%+v, %v; want the app's own copy", b, err)
	}

	bundleFS = nil // a plain build takes the one in the working folder
	if b, err := a.findYtdlp(); err != nil || b.Source != ytdlp.SourceCwd {
		t.Fatalf("plain build: %+v, %v", b, err)
	}
}

// Opt-in: unpacks the real yt-dlp in bundle/ (go run ./cmd/bundle) the way a bundled build does.
//
//	$env:YTGUI_INTEGRATION = "1"; go test . -run Integration -v
func TestIntegrationUnpackBundle(t *testing.T) {
	if os.Getenv("YTGUI_INTEGRATION") != "1" {
		t.Skip("set YTGUI_INTEGRATION=1 to run")
	}
	archive, err := os.ReadFile(filepath.Join("bundle", "yt-dlp.zip"))
	if err != nil {
		t.Skip("run go run ./cmd/bundle first:", err)
	}
	raw, err := os.ReadFile(filepath.Join("bundle", "yt-dlp.version"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(raw))
	// Away from the repository, whose own yt-dlp.exe would be found in the working directory.
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")
	old := bundleFS
	bundleFS = fstest.MapFS{"yt-dlp.zip": {Data: archive}, "yt-dlp.version": {Data: raw}}
	defer func() { bundleFS = old }()

	dir := t.TempDir()
	// What releases up to 0.1.2 unpacked: the single-file yt-dlp, straight into bin.
	legacy := filepath.Join(dir, "bin", ytdlp.Name())
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := settings.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: dir, store: store, versions: map[versionKey]string{}}
	if err := a.unpackBundle(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	got, err := a.findYtdlp()
	if err != nil {
		t.Fatalf("after unpacking: %+v, %v", got, err)
	}
	if v := a.versionOf(context.Background(), got.Path); v != version {
		t.Errorf("unpacked version %q, want %q", v, version)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the old single-file yt-dlp was left behind")
	}
	// A second launch finds it in place and leaves it alone.
	st, _ := os.Stat(got.Path)
	if err := a.unpackBundle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(got.Path); !st2.ModTime().Equal(st.ModTime()) {
		t.Error("unpacked again over the same version")
	}
	// A repair puts it back after it went missing.
	os.Remove(got.Path)
	if err := a.unpackBundle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if v := a.versionOf(context.Background(), got.Path); v != version {
		t.Errorf("repaired version %q, want %q", v, version)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "ffmpeg")); err == nil {
		t.Error("ffmpeg was unpacked without being bundled")
	}
}
