package ytweb

import (
	"context"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// Opt-in, against YouTube and the real yt-dlp: our results against yt-dlp's for the same
// searches, and how long each takes.
//
//	YTGUI_INTEGRATION=1 YTGUI_FORCE_IPV4=1 YTGUI_YTDLP=$PWD/yt-dlp go test ./internal/ytsearch -run Integration -v
func TestIntegrationMatchesYtdlp(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	ipv4 := os.Getenv("YTGUI_FORCE_IPV4") == "1"
	st := settings.Default()
	st.ForceIPv4 = ipv4
	env := ytdlp.Env{Bin: bin, Settings: st, JSRuntimes: ytdlp.DetectJSRuntimes(context.Background())}
	s := New()
	for _, c := range []struct {
		query string
		f     ytdlp.SearchFilter
	}{
		{"lofi beats", ytdlp.SearchFilter{}},
		{"kurzgesagt", ytdlp.SearchFilter{Type: "channel"}},
		{"python tutorial", ytdlp.SearchFilter{Type: "playlist"}},
		{"news", ytdlp.SearchFilter{Uploaded: "week", Type: "video"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		start := time.Now()
		mine, more, err := s.Search(ctx, Options{IPv4: ipv4}, c.query, c.f, 1, 20)
		ours := time.Since(start)
		if err != nil || !more || len(mine.Playlist.Entries) < 15 {
			cancel()
			t.Fatalf("%q: %v, more=%v, %d results", c.query, err, more, len(mine.Playlist.Entries))
		}
		start = time.Now()
		theirs, err := ytdlp.Search(ctx, env, c.query, c.f, 1, 20)
		cancel()
		if err != nil {
			t.Fatalf("%q with yt-dlp: %v", c.query, err)
		}
		t.Logf("%-16q ours %4d ms (%d results), yt-dlp %4d ms (%d)", c.query, ours.Milliseconds(),
			len(mine.Playlist.Entries), time.Since(start).Milliseconds(), len(theirs.Playlist.Entries))

		// Two requests don't get the same list: the same search twice shared 4 of 20 results
		// ("minecraft"), from yt-dlp as from here. What both have must read the same, apart
		// from what YouTube varies per request: titles and thumbnails under A/B test, and the
		// thumbnail links' signatures.
		byID := map[string]ytdlp.PlaylistEntry{}
		for _, e := range theirs.Playlist.Entries {
			byID[e.ID] = e
		}
		shared := 0
		for _, g := range mine.Playlist.Entries {
			w, ok := byID[g.ID]
			if !ok {
				continue
			}
			shared++
			g.Index, w.Index = 0, 0
			g.ViewCount, w.ViewCount = 0, 0 // live counts move between the two requests
			g.Timestamp, w.Timestamp = 0, 0 // "streamed 2 hours ago" ticks over too
			g.Title, w.Title = "", ""
			g.Thumbnail, w.Thumbnail = thumbBase(g.Thumbnail), thumbBase(w.Thumbnail)
			if g != w {
				t.Errorf("%q, %s:\n ours   %+v\n yt-dlp %+v", c.query, g.ID, g, w)
			}
		}
		t.Logf("%-16q %d results in both", c.query, shared)
		if shared == 0 {
			t.Errorf("%q: nothing in common with yt-dlp's results", c.query)
		}
	}
}

// thumbBase is a thumbnail link without its signed query, and without the variant YouTube
// rotates between requests (hq720_custom_1.jpg and _custom_2 are one picture slot, as are a
// Short's oar1.jpg and oar2.jpg, and /eb/vi/ID/oar.jpg and /vi/ID/oar.jpg).
func thumbBase(u string) string {
	u, _, _ = strings.Cut(u, "?")
	u = strings.Replace(u, "/eb/vi/", "/vi/", 1)
	return variantThumbRe.ReplaceAllString(u, "$1")
}

var variantThumbRe = regexp.MustCompile(`(_custom_|/oar)\d+`)

// Opt-in: channel tabs, channels and playlists listed here and by yt-dlp, as the app lists them.
func TestIntegrationListsMatchYtdlp(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	ipv4 := os.Getenv("YTGUI_FORCE_IPV4") == "1"
	st := settings.Default()
	st.ForceIPv4 = ipv4
	env := ytdlp.Env{Bin: bin, Settings: st, JSRuntimes: ytdlp.DetectJSRuntimes(context.Background())}
	l := NewLister()
	for _, c := range []struct {
		link  string
		limit int
	}{
		{"https://www.youtube.com/@kurzgesagt/videos", 60},
		{"https://www.youtube.com/@kurzgesagt/shorts", 60},
		{"https://www.youtube.com/@LofiGirl/streams", 30},
		{"https://www.youtube.com/@kurzgesagt/playlists", 0},
		{"https://www.youtube.com/@LofiGirl", 30},
		{"https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q", 30},
		{"https://www.youtube.com/playlist?list=UUsXVk37bltHxD1rDPwtNM8Q", 150},
		{"https://www.youtube.com/playlist?list=PLFs4vir_WsTyXrrpFstD64Qj95vpy-yo1", 0},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		start := time.Now()
		mine, err := l.List(ctx, Options{IPv4: ipv4}, c.link, c.limit, nil)
		ours := time.Since(start)
		if err != nil {
			cancel()
			t.Errorf("%s: %v", c.link, err)
			continue
		}
		start = time.Now()
		theirs, err := ytdlp.List(ctx, env, c.link, c.limit)
		cancel()
		if err != nil {
			t.Fatalf("%s with yt-dlp: %v", c.link, err)
		}
		t.Logf("%-62s ours %4d ms, yt-dlp %4d ms, %d entries", c.link, ours.Milliseconds(), time.Since(start).Milliseconds(), len(mine.Playlist.Entries))
		g, w := *mine.Playlist, *theirs.Playlist
		if len(g.Entries) != len(w.Entries) {
			t.Errorf("%s: %d entries, yt-dlp %d", c.link, len(g.Entries), len(w.Entries))
		}
		for i := range min(len(g.Entries), len(w.Entries)) {
			ge, we := g.Entries[i], w.Entries[i]
			ge.ViewCount, we.ViewCount = 0, 0 // counts move between the two requests
			ge.Thumbnail, we.Thumbnail = thumbBase(ge.Thumbnail), thumbBase(we.Thumbnail)
			// Read a moment apart, "2 hours ago" can round either side of an hour (a day for
			// days and longer).
			if d := ge.Timestamp - we.Timestamp; d >= -86400 && d <= 86400 && (ge.Timestamp == 0) == (we.Timestamp == 0) {
				ge.Timestamp, we.Timestamp = 0, 0
			}
			if ge != we {
				t.Errorf("%s, entry %d:\n ours   %+v\n yt-dlp %+v", c.link, i+1, ge, we)
			}
		}
		g.Entries, w.Entries = nil, nil
		if mine.Kind != theirs.Kind || !reflect.DeepEqual(g, w) {
			t.Errorf("%s:\n ours   %+v\n yt-dlp %+v", c.link, g, w)
		}
	}
}
