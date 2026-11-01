package tiktok

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

// testdata/profile holds what yt-dlp read listing @nasa (yt-dlp -J --flat-playlist
// --playlist-end=30 --write-pages): the profile page and its two pages of posts, trimmed to
// what is read, and its answer.

func TestProfileHandle(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.tiktok.com/@nasa":              "nasa",
		"https://tiktok.com/@tyke.ae/?_t=8abc&_r=1": "tyke.ae",
		"https://m.tiktok.com/@a_b-c":               "a_b-c",
		"https://www.tiktok.com/@nasa/video/1":      "",
		"https://www.tiktok.com/@nasa/photo/1":      "",
		"https://www.tiktok.com/tag/edit":           "",
		"https://www.tiktok.com@evil.test/@nasa":    "",
		"https://evil.test/@nasa":                   "",
		"https://www.youtube.com/@nasa":             "",
		"ftp://www.tiktok.com/@nasa":                "",
		"https://www.tiktok.com/@":                  "",
	} {
		if got := ProfileHandle(link); got != want {
			t.Errorf("%s: %q, want %q", link, got, want)
		}
	}
}

// fakeProfile serves the @nasa capture: the page, then item_list by cursor, recording what was
// asked. page answers a cursor; nil uses the capture's order.
type fakeProfile struct {
	mu      sync.Mutex
	cursors []int64
	devices []string
	page    func(cursor int64) string
}

func (f *fakeProfile) server(t *testing.T) *httptest.Server {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("testdata", "profile", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html := read("nasa.html")
	p1, p2 := read("nasa.page1.json"), read("nasa.page2.json")
	var page1 map[string]any
	json.Unmarshal([]byte(p1), &page1)
	items := page1["itemList"].([]any)
	last1, _ := intOrNone(items[len(items)-1].(map[string]any)["createTime"])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@nasa":
			http.SetCookie(w, &http.Cookie{Name: "ttwid", Value: "x", Path: "/"})
			w.Write([]byte(html))
		case "/api/creator/item_list/":
			q := r.URL.Query()
			if q.Get("count") != "15" || q.Get("secUid") == "" || q.Get("type") != "1" || r.Header.Get("Referer") == "" {
				t.Errorf("asked %s with Referer %q", r.URL.RawQuery, r.Header.Get("Referer"))
			}
			if c, err := r.Cookie("ttwid"); err != nil || c.Value != "x" {
				t.Error("the list was asked without the page's cookies")
			}
			cursor, _ := strconv.ParseInt(q.Get("cursor"), 10, 64)
			f.mu.Lock()
			f.cursors = append(f.cursors, cursor)
			f.devices = append(f.devices, q.Get("device_id"))
			f.mu.Unlock()
			if f.page != nil {
				w.Write([]byte(f.page(cursor)))
				return
			}
			switch {
			case cursor > last1*1000:
				w.Write([]byte(p1))
			case cursor == last1*1000:
				w.Write([]byte(p2))
			default:
				w.Write([]byte(`{"statusCode":0,"itemList":[],"hasMorePrevious":false}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func listFake(t *testing.T, srv *httptest.Server, limit int, progress Progress) (*ytdlp.ProbeResult, error) {
	site := profileSite{base: srv.URL, tr: srv.Client().Transport, now: func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) }}
	return site.list(context.Background(), "https://www.tiktok.com/@nasa", limit, progress)
}

func TestListsAProfileLikeYtdlp(t *testing.T) {
	var parts []*ytdlp.ProbeResult
	res, err := listFake(t, (&fakeProfile{}).server(t), 30, func(p *ytdlp.ProbeResult) { parts = append(parts, p) })
	if err != nil {
		t.Fatal(err)
	}
	theirsJSON, err := os.ReadFile(filepath.Join("testdata", "profile", "nasa.ytdlp.json"))
	if err != nil {
		t.Fatal(err)
	}
	theirs := probeOf(t, theirsJSON).Playlist
	ours := res.Playlist
	if ours.ID != theirs.ID || ours.Title != theirs.Title || ours.URL != "https://www.tiktok.com/@nasa" || !ours.Channel || !ours.Portrait {
		t.Errorf("listing %+v\nyt-dlp's %+v", ours, theirs)
	}
	if p := ours.Profile; p == nil || p.Handle != "nasa" || p.Name != "NASA" || !p.Verified || p.Followers == 0 || p.Avatar == "" {
		t.Errorf("profile %+v", p)
	}
	if len(ours.Entries) != 30 || len(theirs.Entries) != 30 {
		t.Fatalf("%d entries, yt-dlp's %d", len(ours.Entries), len(theirs.Entries))
	}
	photos := 0
	for i, e := range ours.Entries {
		y := theirs.Entries[i]
		yURL := y.URL
		if strings.Contains(e.URL, "/photo/") {
			photos++
			yURL = strings.Replace(yURL, "/video/", "/photo/", 1)
		}
		if e.ID != y.ID || e.URL != yURL || e.Title != y.Title || e.Duration != y.Duration || e.Timestamp != y.Timestamp ||
			e.ViewCount != y.ViewCount || e.Index != y.Index || e.Uploader != y.Uploader {
			t.Errorf("entry %d:\n ours   %+v\n yt-dlp %+v", i, e, y)
		}
		// The grid-sized cover, not the full frame yt-dlp lists. A photo post has none: its
		// cover is TikTok's (76-155 KB, measured), not its first picture (200-475 KB).
		if !strings.Contains(e.Thumbnail, "zoomcover") && !strings.Contains(e.URL, "/photo/") {
			t.Errorf("entry %d's thumbnail %s", i, e.Thumbnail)
		}
	}
	if photos != 2 {
		t.Errorf("%d photo posts, want 2", photos)
	}
	// Shown as read: the profile with the first 15, then the next 15.
	if len(parts) != 2 || len(parts[0].Playlist.Entries) != 15 || parts[0].Playlist.Profile == nil ||
		parts[1].Playlist.Entries[0].Index != 16 {
		t.Errorf("%d parts", len(parts))
	}
}

func TestListsAProfileToTheEnd(t *testing.T) {
	f := &fakeProfile{}
	res, err := listFake(t, f.server(t), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Playlist.Entries) != 30 || len(f.cursors) != 3 {
		t.Errorf("%d entries from %d requests", len(res.Playlist.Entries), len(f.cursors))
	}
}

func TestProfileThatRepeatsItsPageEnds(t *testing.T) {
	// A device ID TikTok dislikes gets the same page for ever (yt-dlp#14031): one more try
	// with another, then the listing ends.
	p1, _ := os.ReadFile(filepath.Join("testdata", "profile", "nasa.page1.json"))
	f := &fakeProfile{page: func(int64) string { return string(p1) }}
	res, err := listFake(t, f.server(t), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Playlist.Entries) != 15 || len(f.cursors) != 3 || f.devices[1] == f.devices[2] {
		t.Errorf("%d entries from %d requests, devices %v", len(res.Playlist.Entries), len(f.cursors), f.devices)
	}
}

func TestProfileQuietWeeksStepBack(t *testing.T) {
	// An empty page that says there is more: the cursor goes back a week, as yt-dlp's does.
	f := &fakeProfile{}
	f.page = func(cursor int64) string {
		if len(f.cursors) < 3 {
			return `{"statusCode":0,"itemList":[],"hasMorePrevious":true}`
		}
		return `{"statusCode":0,"itemList":[],"hasMorePrevious":false}`
	}
	if _, err := listFake(t, f.server(t), 0, nil); err != nil {
		t.Fatal(err)
	}
	week := int64(7 * 24 * time.Hour / time.Millisecond)
	if len(f.cursors) != 3 || f.cursors[0]-f.cursors[1] != week || f.cursors[1]-f.cursors[2] != week {
		t.Errorf("cursors %v", f.cursors)
	}
}

func TestProfilesLeftToYtdlp(t *testing.T) {
	page := func(detail string) string {
		return `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{"__DEFAULT_SCOPE__":{"webapp.user-detail":` + detail + `}}</script>`
	}
	for name, body := range map[string]string{
		"missing":     page(`{"statusCode":10221,"statusMsg":"user doesn't exist"}`),
		"private":     page(`{"statusCode":0,"userInfo":{"user":{"uniqueId":"nasa","secUid":"S","privateAccount":true}}}`),
		"another":     page(`{"statusCode":0,"userInfo":{"user":{"uniqueId":"someone","secUid":"S"}}}`),
		"no secUid":   page(`{"statusCode":0,"userInfo":{"user":{"uniqueId":"nasa"}}}`),
		"a challenge": `<html>Please wait...</html>`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/@nasa" {
				w.Write([]byte(body))
				return
			}
			t.Errorf("%s: asked %s", name, r.URL)
		}))
		if _, err := listFake(t, srv, 0, nil); err == nil {
			t.Errorf("%s: listed", name)
		}
		srv.Close()
	}
	// A refused list (0 bytes) before anything was shown fails the whole listing too.
	f := &fakeProfile{page: func(int64) string { return "" }}
	if _, err := listFake(t, f.server(t), 0, nil); err == nil {
		t.Error("a refused list was taken as empty")
	}
}
