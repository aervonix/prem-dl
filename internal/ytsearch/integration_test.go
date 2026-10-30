package ytsearch

import (
	"context"
	"os"
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

// thumbBase is a thumbnail link without its signed query, and without the A/B variant
// (hq720_custom_1.jpg and hq720_custom_2.jpg are one picture slot).
func thumbBase(u string) string {
	u, _, _ = strings.Cut(u, "?")
	return customThumbRe.ReplaceAllString(u, "")
}

var customThumbRe = regexp.MustCompile(`_custom_\d+`)
