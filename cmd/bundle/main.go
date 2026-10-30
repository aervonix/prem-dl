// Command bundle fetches the tools a bundled build for this platform carries into bundle/: the
// newest yt-dlp's unpacked build as yt-dlp.zip and its version and, unless -ffmpeg=false, yt-dlp's
// ffmpeg build as ffmpeg.zip (Linux's tar.xz repacked). Both are checked against their published SHA-256
// sums, the same way the app installs them. Then:
//
//	wails build -tags bundled
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

func main() {
	dir := flag.String("dir", "bundle", "folder to fill")
	channel := flag.String("channel", "stable", "yt-dlp release channel: stable, nightly or master")
	withFFmpeg := flag.Bool("ffmpeg", true, "also bundle ffmpeg (about 90 MB on Windows, 140 MB on Linux)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *dir, *channel, *withFFmpeg); err != nil {
		fmt.Fprintln(os.Stderr, "bundle:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dir, channel string, withFFmpeg bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := ytdlp.HTTPClient("", 15*time.Minute)

	// Earlier versions of this command fetched the single-file yt-dlp.
	for _, name := range []string{"yt-dlp", "yt-dlp.exe"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	fmt.Println("yt-dlp:")
	ytZip := filepath.Join(dir, "yt-dlp.zip")
	err := ytdlp.Download(ctx, client, channel, ytZip+".part", progress)
	fmt.Println()
	if err != nil {
		return err
	}
	defer os.Remove(ytZip + ".part")
	// Unpacked once, to check it runs here and to learn its version.
	version, err := tryYtdlp(ctx, ytZip+".part")
	if err != nil {
		return err
	}
	if err := os.Rename(ytZip+".part", ytZip); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp.version"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("  version", version)

	zipPath := filepath.Join(dir, "ffmpeg.zip")
	if !withFFmpeg {
		// An archive left from an earlier run would be bundled too.
		if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	fmt.Println("ffmpeg:")
	part := zipPath + ".part"
	defer os.Remove(part)
	err = ytdlp.DownloadFFmpeg(ctx, client, part, progress)
	fmt.Println()
	if err != nil {
		return err
	}
	return os.Rename(part, zipPath)
}

func tryYtdlp(ctx context.Context, archive string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "ytgui-bundle-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	return ytdlp.Install(ctx, f, st.Size(), tmp)
}

func progress(done, total int64) {
	if total > 0 {
		fmt.Printf("\r  %5.1f / %5.1f MB", float64(done)/1e6, float64(total)/1e6)
	} else {
		fmt.Printf("\r  %5.1f MB", float64(done)/1e6)
	}
}
