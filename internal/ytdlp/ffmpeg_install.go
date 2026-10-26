package ytdlp

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// yt-dlp's own ffmpeg builds carry the patches yt-dlp relies on. "latest" is a rolling release,
// rebuilt daily under the same name, so the checksum file and the zip are fetched together and
// fetched again if they don't match (a rebuild landed in between).
const ffmpegRelease = "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/"

// The shared build (ffmpeg.exe and ffprobe.exe over common DLLs) is 87 MB to download against
// 191 MB for the static one, which carries the same code twice.
const ffmpegAsset = "ffmpeg-master-latest-win64-gpl-shared.zip"

// FFmpegDir is where the app installs ffmpeg. It gets a folder of its own because of the DLLs.
func FFmpegDir(managedDir string) string {
	return filepath.Join(managedDir, "bin", "ffmpeg")
}

// CanInstallFFmpeg reports whether InstallFFmpeg has a build for this computer.
func CanInstallFFmpeg() bool {
	return runtime.GOOS == "windows" && runtime.GOARCH == "amd64"
}

// InstallFFmpeg downloads ffmpeg into FFmpegDir, verifying the archive's SHA-256 and running the
// result once before swapping it in. It returns the installed version.
func InstallFFmpeg(ctx context.Context, client *http.Client, managedDir string, progress ProgressFunc) (string, error) {
	if !CanInstallFFmpeg() {
		return "", errors.New("ffmpeg can only be installed automatically on 64-bit Windows")
	}
	dir := FFmpegDir(managedDir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	archive := dir + ".zip.part"
	defer os.Remove(archive)
	if err := DownloadFFmpeg(ctx, client, archive, progress); err != nil {
		return "", err
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		return "", fmt.Errorf("unpacking ffmpeg: %w", err)
	}
	defer z.Close()
	return installFFmpegZip(ctx, &z.Reader, managedDir)
}

// DownloadFFmpeg fetches yt-dlp's ffmpeg build to path, verified against its published SHA-256.
func DownloadFFmpeg(ctx context.Context, client *http.Client, path string, progress ProgressFunc) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = fetchFFmpeg(ctx, client, path, progress); !errors.Is(err, errHashMismatch) {
			break
		}
	}
	return err
}

// InstallFFmpegFrom installs ffmpeg from an archive DownloadFFmpeg fetched earlier (the copy built
// into the app), the same way InstallFFmpeg does.
func InstallFFmpegFrom(ctx context.Context, archive io.ReaderAt, size int64, managedDir string) (string, error) {
	z, err := zip.NewReader(archive, size)
	if err != nil {
		return "", fmt.Errorf("unpacking ffmpeg: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(FFmpegDir(managedDir)), 0o755); err != nil {
		return "", err
	}
	return installFFmpegZip(ctx, z, managedDir)
}

// installFFmpegZip unpacks the archive next to FFmpegDir, runs it once and swaps it in.
func installFFmpegZip(ctx context.Context, archive *zip.Reader, managedDir string) (string, error) {
	dir := FFmpegDir(managedDir)
	staged := dir + ".new"
	_ = os.RemoveAll(staged)
	if err := extractFFmpeg(archive, staged); err != nil {
		os.RemoveAll(staged)
		return "", fmt.Errorf("unpacking ffmpeg: %w", err)
	}
	out, err := run(ctx, 30*time.Second, filepath.Join(staged, "ffmpeg.exe"), "-version")
	if err != nil {
		os.RemoveAll(staged)
		return "", fmt.Errorf("the downloaded ffmpeg didn't run: %w", err)
	}
	version := "unknown"
	if m := ffmpegVersionRe.FindStringSubmatch(out); m != nil {
		version = m[1]
	}

	// Swap: the previous copy is moved aside first and restored if the new one can't move in.
	old := dir + ".old"
	_ = os.RemoveAll(old)
	hadOld := false
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			os.RemoveAll(staged)
			return "", fmt.Errorf("replacing ffmpeg (is it in use?): %w", err)
		}
		hadOld = true
	}
	if err := os.Rename(staged, dir); err != nil {
		if hadOld {
			_ = os.Rename(old, dir)
		}
		os.RemoveAll(staged)
		return "", fmt.Errorf("installing ffmpeg: %w", err)
	}
	_ = os.RemoveAll(old)
	return version, nil
}

var errHashMismatch = errors.New("the downloaded ffmpeg failed its SHA-256 check and was discarded")

func fetchFFmpeg(ctx context.Context, client *http.Client, archive string, progress ProgressFunc) error {
	sums, err := fetchAll(ctx, client, ffmpegRelease+"checksums.sha256", 1<<20)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	want, ok := parseChecksum(sums, ffmpegAsset)
	if !ok {
		return fmt.Errorf("no checksum published for %s; refusing to install an unverified download", ffmpegAsset)
	}
	hash, err := downloadTo(ctx, client, ffmpegRelease+ffmpegAsset, archive, progress)
	if err != nil {
		return err
	}
	if hash != want {
		return errHashMismatch
	}
	return nil
}

// extractFFmpeg copies what ffmpeg needs out of the archive's bin folder (the two programs and
// their DLLs, not the ffplay player) plus the licence, flat into dest. Names are reduced to their
// last element, so no entry can write outside dest.
func extractFFmpeg(z *zip.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	found := map[string]bool{}
	for _, f := range z.File {
		if !wantFromFFmpegZip(f.Name) {
			continue
		}
		name := filepath.Base(filepath.FromSlash(f.Name))
		if err := extractFile(f, filepath.Join(dest, name)); err != nil {
			return err
		}
		found[strings.ToLower(name)] = true
	}
	for _, need := range []string{"ffmpeg.exe", "ffprobe.exe"} {
		if !found[need] {
			return fmt.Errorf("%s is missing from the download", need)
		}
	}
	return nil
}

// wantFromFFmpegZip picks <top>/bin/ffmpeg.exe, ffprobe.exe and *.dll, and <top>/LICENSE.txt.
func wantFromFFmpegZip(name string) bool {
	parts := strings.Split(name, "/")
	switch {
	case len(parts) == 2:
		return strings.EqualFold(parts[1], "LICENSE.txt")
	case len(parts) == 3 && parts[1] == "bin":
		base := strings.ToLower(parts[2])
		return base == "ffmpeg.exe" || base == "ffprobe.exe" || strings.HasSuffix(base, ".dll")
	}
	return false
}

func extractFile(f *zip.File, path string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(w, r)
	if err := w.Close(); copyErr == nil {
		copyErr = err
	}
	return copyErr
}
