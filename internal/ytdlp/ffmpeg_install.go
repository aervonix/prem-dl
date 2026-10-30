package ytdlp

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
)

// yt-dlp's own ffmpeg builds carry the patches yt-dlp relies on. "latest" is a rolling release,
// rebuilt daily under the same name, so the checksum file and the zip are fetched together and
// fetched again if they don't match (a rebuild landed in between).
const ffmpegRelease = "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/"

// ffmpegAsset is the build for this computer, or "" when there is none. On Windows the shared
// build (ffmpeg.exe and ffprobe.exe over common DLLs) is 87 MB to download against 191 MB for the
// static one, which carries the same code twice. Linux builds are only static, and come as tar.xz.
func ffmpegAsset() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "ffmpeg-master-latest-win64-gpl-shared.zip"
	case "linux/amd64":
		return "ffmpeg-master-latest-linux64-gpl.tar.xz"
	case "linux/arm64":
		return "ffmpeg-master-latest-linuxarm64-gpl.tar.xz"
	}
	return ""
}

// exeName is a program's file name on goos.
func exeName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

// FFmpegName and FFprobeName are the programs' file names on this platform.
func FFmpegName() string  { return exeName("ffmpeg", runtime.GOOS) }
func FFprobeName() string { return exeName("ffprobe", runtime.GOOS) }

// FFmpegDir is where the app installs ffmpeg. It gets a folder of its own because of the DLLs
// on Windows.
func FFmpegDir(managedDir string) string {
	return filepath.Join(managedDir, "bin", "ffmpeg")
}

// CanInstallFFmpeg reports whether InstallFFmpeg has a build for this computer.
func CanInstallFFmpeg() bool {
	return ffmpegAsset() != ""
}

// InstallFFmpeg downloads ffmpeg into FFmpegDir, verifying the archive's SHA-256 and running the
// result once before swapping it in. It returns the installed version.
func InstallFFmpeg(ctx context.Context, client *http.Client, managedDir string, progress ProgressFunc) (string, error) {
	if !CanInstallFFmpeg() {
		return "", errNoFFmpegBuild
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

var errNoFFmpegBuild = errors.New("there is no ffmpeg build to install on this computer")

// DownloadFFmpeg fetches yt-dlp's ffmpeg build, verified against its published SHA-256, and
// leaves it at path as a zip. A tar.xz (Linux) is repacked into a zip of the same layout holding
// only what the app keeps, so both install the same way.
func DownloadFFmpeg(ctx context.Context, client *http.Client, path string, progress ProgressFunc) error {
	asset := ffmpegAsset()
	if asset == "" {
		return errNoFFmpegBuild
	}
	download := path
	if !strings.HasSuffix(asset, ".zip") {
		download = path + ".download"
		defer os.Remove(download)
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = fetchFFmpeg(ctx, client, asset, download, progress); !errors.Is(err, errHashMismatch) {
			break
		}
	}
	if err != nil || download == path {
		return err
	}
	return repackFFmpeg(download, path, runtime.GOOS)
}

// repackFFmpeg copies the entries the app keeps from a tar.xz build into a zip at dest.
func repackFFmpeg(tarXZ, dest, goos string) error {
	in, err := os.Open(tarXZ)
	if err != nil {
		return err
	}
	defer in.Close()
	xr, err := xz.NewReader(bufio.NewReader(in))
	if err != nil {
		return fmt.Errorf("reading the ffmpeg archive: %w", err)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	found := map[string]bool{}
	tr := tar.NewReader(xr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Close()
			return fmt.Errorf("reading the ffmpeg archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !wantFFmpegFile(hdr.Name, goos) {
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: hdr.Name, Method: zip.Deflate, Modified: hdr.ModTime})
		if err == nil {
			_, err = io.Copy(w, tr)
		}
		if err != nil {
			out.Close()
			return err
		}
		found[path.Base(hdr.Name)] = true
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	for _, need := range []string{exeName("ffmpeg", goos), exeName("ffprobe", goos)} {
		if !found[need] {
			return fmt.Errorf("%s is missing from the download", need)
		}
	}
	return nil
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
	if err := extractFFmpeg(archive, staged, runtime.GOOS); err != nil {
		os.RemoveAll(staged)
		return "", fmt.Errorf("unpacking ffmpeg: %w", err)
	}
	out, err := run(ctx, 30*time.Second, filepath.Join(staged, FFmpegName()), "-version")
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

func fetchFFmpeg(ctx context.Context, client *http.Client, asset, archive string, progress ProgressFunc) error {
	sums, err := fetchAll(ctx, client, ffmpegRelease+"checksums.sha256", 1<<20)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	want, ok := parseChecksum(sums, asset)
	if !ok {
		return fmt.Errorf("no checksum published for %s; refusing to install an unverified download", asset)
	}
	hash, err := downloadTo(ctx, client, ffmpegRelease+asset, archive, progress)
	if err != nil {
		return err
	}
	if hash != want {
		return errHashMismatch
	}
	return nil
}

// extractFFmpeg copies what ffmpeg needs out of the archive's bin folder (the two programs and,
// on Windows, their DLLs, not the ffplay player) plus the licence, flat into dest. Names are
// reduced to their last element, so no entry can write outside dest. goos is runtime.GOOS
// outside tests.
func extractFFmpeg(z *zip.Reader, dest, goos string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	found := map[string]bool{}
	for _, f := range z.File {
		if !wantFFmpegFile(f.Name, goos) {
			continue
		}
		name := filepath.Base(filepath.FromSlash(f.Name))
		if err := extractFile(f, filepath.Join(dest, name)); err != nil {
			return err
		}
		found[strings.ToLower(name)] = true
	}
	for _, need := range []string{exeName("ffmpeg", goos), exeName("ffprobe", goos)} {
		if !found[need] {
			return fmt.Errorf("%s is missing from the download", need)
		}
	}
	return nil
}

// wantFFmpegFile picks <top>/bin/ffmpeg and ffprobe (with .exe and *.dll on Windows), and
// <top>/LICENSE.txt.
func wantFFmpegFile(name, goos string) bool {
	parts := strings.Split(name, "/")
	switch {
	case len(parts) == 2:
		return strings.EqualFold(parts[1], "LICENSE.txt")
	case len(parts) == 3 && parts[1] == "bin":
		base := parts[2]
		if goos == "windows" {
			base = strings.ToLower(base)
			if strings.HasSuffix(base, ".dll") {
				return true
			}
		}
		return base == exeName("ffmpeg", goos) || base == exeName("ffprobe", goos)
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
