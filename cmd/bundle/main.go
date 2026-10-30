// Command bundle fetches the tools a bundled build for this platform carries into bundle/: the
// newest yt-dlp (yt-dlp.exe on Windows) and its version and, unless -ffmpeg=false, yt-dlp's ffmpeg
// build as ffmpeg.zip (Linux's tar.xz repacked). Both are checked against their published SHA-256
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

	// A yt-dlp fetched for another platform into the same folder would be bundled too.
	for _, name := range []string{"yt-dlp", "yt-dlp.exe"} {
		if name == ytdlp.Name() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	fmt.Println("yt-dlp:")
	version, err := ytdlp.Install(ctx, client, channel, filepath.Join(dir, ytdlp.Name()), progress)
	fmt.Println()
	if err != nil {
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

func progress(done, total int64) {
	if total > 0 {
		fmt.Printf("\r  %5.1f / %5.1f MB", float64(done)/1e6, float64(total)/1e6)
	} else {
		fmt.Printf("\r  %5.1f MB", float64(done)/1e6)
	}
}
