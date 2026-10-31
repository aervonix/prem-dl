package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
)

func TestPlaylistFileInPlaylistOrder(t *testing.T) {
	dir := t.TempDir()
	done := func(index int, title string) queue.Job {
		file := filepath.Join(dir, title+".mp3")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return queue.Job{Status: queue.StatusCompleted, Title: title, Duration: 61, OutputFile: file,
			PlaylistTitle: "Mix: A/B", PlaylistIndex: index, Options: settings.DownloadOptions{PlaylistFile: true}}
	}
	var p playlistFiles
	p.finished([]queue.Job{done(3, "Three")})
	p.finished([]queue.Job{done(1, "One"), {Status: queue.StatusFailed, PlaylistTitle: "Mix: A/B", PlaylistIndex: 2}})
	// The queue was cleared since: what the file had stays.
	p.finished([]queue.Job{done(2, "Two")})
	data, err := os.ReadFile(filepath.Join(dir, "Mix_ A_B.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "#EXTM3U\n") || strings.Index(got, "One.mp3") > strings.Index(got, "Two.mp3") || strings.Index(got, "Two.mp3") > strings.Index(got, "Three.mp3") {
		t.Fatalf("out of order:\n%s", got)
	}
	if !strings.Contains(got, "#EXTINF:61,Two\nTwo.mp3\n") {
		t.Errorf("entry:\n%s", got)
	}
	// A file deleted since drops out; one not asked for writes nothing.
	os.Remove(filepath.Join(dir, "One.mp3"))
	p.finished([]queue.Job{done(4, "Four")})
	data, _ = os.ReadFile(filepath.Join(dir, "Mix_ A_B.m3u8"))
	if strings.Contains(string(data), "One.mp3") || !strings.Contains(string(data), "Four.mp3") {
		t.Errorf("after a delete:\n%s", data)
	}
	j := done(5, "Five")
	j.Options.PlaylistFile = false
	j.PlaylistTitle = "Other"
	p.finished([]queue.Job{j})
	if _, err := os.Stat(filepath.Join(dir, "Other.m3u8")); err == nil {
		t.Error("wrote a playlist file nobody asked for")
	}
}
