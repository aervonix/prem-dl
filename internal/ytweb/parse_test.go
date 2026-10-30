package ytweb

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

// Each folder in testdata holds the pages of one real search (yt-dlp's --write-pages, with
// only what a parser reads kept) and what yt-dlp 2026.10.26 printed for them with
// -J --flat-playlist --playlist-items=1:45 --extractor-args=youtubetab:approximate_date.
var fixtures = []string{"videos", "channels", "playlists", "live", "week"}

func readGz(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// expected is what yt-dlp made of a fixture, and when ("3 years ago" depends on it).
func expected(t *testing.T, name string) (entries []json.RawMessage, now time.Time) {
	t.Helper()
	var exp struct {
		Epoch   int64             `json:"epoch"`
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(readGz(t, filepath.Join("testdata", "search", name, "expected.json.gz")), &exp); err != nil {
		t.Fatal(err)
	}
	return exp.Entries, time.Unix(exp.Epoch, 0)
}

// pages parses a fixture's pages in order, checking each continues from the one before.
func pages(t *testing.T, name string, now time.Time) []entry {
	t.Helper()
	var all []entry
	for i := 1; ; i++ {
		path := filepath.Join("testdata", "search", name, "page"+strconv.Itoa(i)+".json.gz")
		if !exists(path) {
			break
		}
		p, err := parseSearch(readGz(t, path), extractor{now: now, dates: true})
		if err != nil {
			t.Fatalf("%s page %d: %v", name, i, err)
		}
		if p.next == "" {
			t.Errorf("%s page %d has no continuation", name, i)
		}
		all = append(all, p.entries...)
	}
	return all
}

// The app sees results as PlaylistEntry; ours must come out exactly as yt-dlp's do.
func TestParsesLikeYtdlp(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			want, now := expected(t, name)
			got := pages(t, name, now)
			if len(got) < len(want) {
				t.Fatalf("%d results, yt-dlp found %d", len(got), len(want))
			}
			mine, err := result("q", ytdlp.SearchFilter{}, got[:len(want)], 1)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(map[string]any{"_type": "playlist", "id": "q", "title": "q", "entries": want})
			theirs, err := ytdlp.ParseProbe(data)
			if err != nil {
				t.Fatal(err)
			}
			for i := range want {
				g, w := mine.Playlist.Entries[i], theirs.Playlist.Entries[i]
				if !reflect.DeepEqual(g, w) {
					t.Errorf("result %d:\n got %+v\nwant %+v", i+1, g, w)
				}
			}
		})
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
