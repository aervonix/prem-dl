package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
)

// A playlist downloaded with "Write a playlist file" gets "<Playlist>.m3u8" next to its videos,
// in playlist order, so a player (foobar, VLC, a car stereo) plays the folder as the playlist.
// Prem-dl queues each video as its own download, so yt-dlp can't write it: the file is rebuilt
// as each one finishes. Entries already in it are kept (the queue may have been cleared since),
// with the playlist position in a "#PREMDL-INDEX" line players skip.

type playlistFiles struct {
	mu sync.Mutex // one rewrite at a time
}

type m3uEntry struct {
	index   int
	seconds int
	title   string
	path    string // relative to the playlist file, with forward slashes
}

// finished takes the jobs that just changed and rewrites the playlist file of each completed one
// that asked for it.
func (p *playlistFiles) finished(changed []queue.Job) {
	for _, j := range changed {
		if j.Status != queue.StatusCompleted || !j.Options.PlaylistFile || j.PlaylistTitle == "" || j.OutputFile == "" || j.PlaylistIndex <= 0 {
			continue
		}
		dir := filepath.Dir(j.OutputFile)
		name := playlistFileName(j.PlaylistTitle)
		p.mu.Lock()
		_ = addToPlaylistFile(filepath.Join(dir, name), m3uEntry{
			index: j.PlaylistIndex, seconds: int(j.Duration), title: j.Title, path: filepath.Base(j.OutputFile),
		})
		p.mu.Unlock()
	}
}

// playlistFileName is the playlist's title made safe as a file name, ".m3u8" added.
func playlistFileName(title string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	clean = strings.Trim(clean, ". ")
	if r := []rune(clean); len(r) > 120 {
		clean = string(r[:120])
	}
	if clean == "" {
		clean = "Playlist"
	}
	return clean + ".m3u8"
}

// addToPlaylistFile puts e into the playlist file at path, in index order, keeping what's there
// whose file still exists.
func addToPlaylistFile(path string, e m3uEntry) error {
	entries := readPlaylistFile(path)
	dir := filepath.Dir(path)
	entries = slices.DeleteFunc(entries, func(o m3uEntry) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(o.path)))
		return o.path == e.path || o.index == e.index || err != nil
	})
	entries = append(entries, e)
	slices.SortStableFunc(entries, func(a, b m3uEntry) int { return a.index - b.index })
	var buf bytes.Buffer
	buf.WriteString("#EXTM3U\n")
	for _, x := range entries {
		fmt.Fprintf(&buf, "#PREMDL-INDEX:%d\n#EXTINF:%d,%s\n%s\n", x.index, x.seconds, strings.ReplaceAll(x.title, "\n", " "), x.path)
	}
	return settings.WriteFileAtomic(path, buf.Bytes(), 0o644)
}

func readPlaylistFile(path string) []m3uEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []m3uEntry
	var cur m3uEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "#PREMDL-INDEX:"):
			cur.index, _ = strconv.Atoi(strings.TrimPrefix(line, "#PREMDL-INDEX:"))
		case strings.HasPrefix(line, "#EXTINF:"):
			secs, title, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			cur.seconds, _ = strconv.Atoi(secs)
			cur.title = title
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			cur.path = line
			if cur.index > 0 {
				out = append(out, cur)
			}
			cur = m3uEntry{}
		}
	}
	return out
}
