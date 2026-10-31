package main

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/ytdlp"
)

// bundleFS holds yt-dlp's unpacked build (yt-dlp.zip, and yt-dlp.version) and optionally the
// ffmpeg archive (ffmpeg.zip) built into this exe, or is nil for a build without them. A program can't be run
// from inside another, so they are unpacked into the app's folder. A build that carries
// them runs nothing else (see findYtdlp), so the tools change only with a new Prem-dl.
var bundleFS fs.FS

// bundleHas reports whether this build carries the named file.
func bundleHas(name string) bool {
	if bundleFS == nil {
		return false
	}
	_, err := fs.Stat(bundleFS, name)
	return err == nil
}

// bundleStampName records which bundle the app's folder holds, so each launch doesn't unpack
// ffmpeg's 200 MB again.
const bundleStampName = "bundle.stamp"

// unpackBundle puts the built-in tools where the app runs them from. It runs once, at the start
// of the first tool detection, before anything resolves yt-dlp, and again on a repair (force).
// Whatever the folder held before is replaced, newer or older: the bundle is the version this
// release was tested with.
func (a *App) unpackBundle(ctx context.Context, force bool) error {
	if bundleFS == nil {
		return nil
	}
	dir := a.managedDir()
	stampPath := filepath.Join(dir, "bin", bundleStampName)
	// The stamp's second line names the exe that wrote it. The same exe carries the same bundle,
	// so its checksum (185 MB read: 0.1 s here, seconds on a hard disk) isn't worked out again.
	exe := exeID()
	if recorded, by := readStamp(stampPath); !force && exe != "" && by == exe && bundleInPlace(dir, stampPath, recorded) {
		return nil
	}
	stamp, err := bundleStamp()
	if err != nil {
		return err
	}
	if !force && bundleInPlace(dir, stampPath, stamp) {
		return os.WriteFile(stampPath, []byte(stamp+"\n"+exe+"\n"), 0o644)
	}
	// A failed unpack must not leave the old stamp claiming the folder is complete.
	_ = os.Remove(stampPath)
	if err := installBundled(ctx, "yt-dlp.zip", dir, ytdlp.Install); err != nil {
		return fmt.Errorf("unpacking yt-dlp: %w", err)
	}
	// Releases up to 0.1.2 ran the single-file yt-dlp from bin.
	for _, suffix := range []string{"", ".old", ".new"} {
		_ = os.Remove(filepath.Join(dir, "bin", ytdlp.Name()+suffix))
	}
	if bundleHas("ffmpeg.zip") {
		if err := installBundled(ctx, "ffmpeg.zip", dir, ytdlp.InstallFFmpegFrom); err != nil {
			return fmt.Errorf("unpacking ffmpeg: %w", err)
		}
	}
	return os.WriteFile(stampPath, []byte(stamp+"\n"+exe+"\n"), 0o644)
}

// readStamp returns the bundle a stamp file records and the exe that wrote it ("" for stamps
// written before it was recorded).
func readStamp(path string) (stamp, exe string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	stamp, exe, _ = strings.Cut(strings.TrimSpace(string(raw)), "\n")
	return strings.TrimSpace(stamp), strings.TrimSpace(exe)
}

// exeID identifies the running exe by path, size and time, or "" when it can't be read.
func exeID() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s %d %d", path, fi.Size(), fi.ModTime().UnixNano())
}

// bundleInPlace reports whether the app's folder already holds this bundle: the stamp matches
// and the programs are still there (antivirus or a clean-up may have removed them since).
func bundleInPlace(dir, stampPath, stamp string) bool {
	if recorded, _ := readStamp(stampPath); recorded == "" || recorded != stamp {
		return false
	}
	need := []string{ytdlp.ManagedPath(dir)}
	if bundleHas("ffmpeg.zip") {
		need = append(need, filepath.Join(ytdlp.FFmpegDir(dir), ytdlp.FFmpegName()), filepath.Join(ytdlp.FFmpegDir(dir), ytdlp.FFprobeName()))
	}
	for _, p := range need {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return false
		}
	}
	return true
}

// bundleStamp identifies the built-in tools: yt-dlp's version plus a checksum of each file.
// CRC-32 runs at memory speed, and this only tells one bundle from another.
func bundleStamp() (string, error) {
	raw, err := fs.ReadFile(bundleFS, "yt-dlp.version")
	if err != nil {
		return "", err
	}
	parts := []string{strings.TrimSpace(string(raw))}
	for _, name := range []string{"yt-dlp.zip", "ffmpeg.zip"} {
		f, err := bundleFS.Open(name)
		if errors.Is(err, fs.ErrNotExist) && name == "ffmpeg.zip" {
			continue
		}
		if err != nil {
			return "", err
		}
		h := crc32.NewIEEE()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s:%08x", name, h.Sum32()))
	}
	return strings.Join(parts, " "), nil
}

// installBundled unpacks the built-in archive name with install. Files built into the exe can be
// read at any offset, which zip needs; anything else is read into memory first.
func installBundled(ctx context.Context, name, dir string, install func(context.Context, io.ReaderAt, int64, string) (string, error)) error {
	f, err := bundleFS.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
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
	_, err = install(ctx, ra, st.Size(), dir)
	return err
}

// warn logs a problem the user can't act on right now. The Wails logger needs the runtime
// context, which isn't there before startup.
func (a *App) warn(msg string) {
	if a.ready() {
		wr.LogWarning(a.rt(), msg)
	}
}
