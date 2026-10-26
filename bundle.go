package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// bundleFS holds the yt-dlp (yt-dlp.exe, yt-dlp.version) and optionally the ffmpeg archive
// (ffmpeg.zip) built into this exe, or is nil for a build without them. Windows can't run a
// program from inside another, so they are unpacked into the app's folder, where Resolve and
// FindFFmpeg find them and updates replace them like any copy the app installed.
var bundleFS fs.FS

// unpackBundle puts the built-in tools where the app looks for them. It runs once, at the start
// of the first tool detection, before anything resolves yt-dlp.
func (a *App) unpackBundle(ctx context.Context) {
	if bundleFS == nil {
		return
	}
	s := a.store.Get()
	dir := a.managedDir()
	if raw, err := fs.ReadFile(bundleFS, "yt-dlp.version"); err == nil {
		managed := ytdlp.ManagedPath(dir)
		found, ferr := ytdlp.Resolve(s.YtdlpPath, dir)
		have := ""
		if ferr == nil && found.Source == ytdlp.SourceManaged {
			have = a.versionOf(ctx, managed) // cached, so detection doesn't ask again
		}
		if bundledYtdlpWanted(s, found, ferr, have, strings.TrimSpace(string(raw))) {
			if err := installBundled(ctx, managed); err != nil {
				a.warn("couldn't unpack the built-in yt-dlp: " + err.Error())
			}
		}
	}
	if f, err := bundleFS.Open("ffmpeg.zip"); err == nil {
		defer f.Close()
		ytPath := ""
		if b, err := ytdlp.Resolve(s.YtdlpPath, dir); err == nil {
			ytPath = b.Path
		}
		if ytdlp.FindFFmpeg(ctx, s.FfmpegPath, ytPath, dir).Path == "" && ytdlp.CanInstallFFmpeg() {
			if err := installBundledFFmpeg(ctx, f, dir); err != nil {
				a.warn("couldn't unpack the built-in ffmpeg: " + err.Error())
			}
		}
	}
}

// bundledYtdlpWanted decides whether the built-in yt-dlp goes into the app's folder: when there's
// no yt-dlp, when the one found is on PATH (the app's copy outranks it), or when the app's own
// copy is older or broken. One the user chose or placed next to the app is left in charge, and
// a copy updated since is never downgraded.
func bundledYtdlpWanted(s settings.Settings, found ytdlp.Binary, err error, managedVersion, bundled string) bool {
	switch {
	case s.YtdlpPath != "":
		return false // a configured path wins even when it's missing; the user is told about that
	case err != nil:
		return errors.Is(err, ytdlp.ErrNotFound)
	case found.Source == ytdlp.SourcePath:
		return true
	case found.Source == ytdlp.SourceManaged:
		return managedVersion == "" || ytdlp.CompareVersions(managedVersion, bundled) < 0
	}
	return false
}

func installBundled(ctx context.Context, dest string) error {
	f, err := bundleFS.Open("yt-dlp.exe")
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = ytdlp.InstallFrom(ctx, f, dest)
	return err
}

// installBundledFFmpeg unpacks the built-in archive. Files built into the exe can be read at any
// offset, which zip needs; anything else is read into memory first.
func installBundledFFmpeg(ctx context.Context, f fs.File, dir string) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	ra, ok := f.(io.ReaderAt)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		ra = strings.NewReader(string(data))
	}
	_, err = ytdlp.InstallFFmpegFrom(ctx, ra, st.Size(), dir)
	return err
}

// warn logs a problem the user can't act on right now. The Wails logger needs the runtime
// context, which isn't there before startup.
func (a *App) warn(msg string) {
	if a.ready() {
		wr.LogWarning(a.rt(), msg)
	}
}
