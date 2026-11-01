package tiktok

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// Opt-in, against TikTok and the real yt-dlp: each post read here and by yt-dlp -J, compared
// as the UI sees them (ytdlp.ParseProbe), and how long each takes. Posts get deleted; swap in
// others of the same kind when one goes.
//
//	YTGUI_INTEGRATION=1 YTGUI_FORCE_IPV4=1 YTGUI_YTDLP=$PWD/yt-dlp go test ./internal/tiktok -run Integration -v
func TestIntegrationMatchesYtdlp(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	st := settings.Default()
	st.ForceIPv4 = os.Getenv("YTGUI_FORCE_IPV4") == "1"
	env := ytdlp.Env{Bin: bin, Settings: st}
	opts := Options{IPv4: st.ForceIPv4}
	for _, c := range []struct{ name, link string }{
		{"renditions", "https://www.tiktok.com/@xvbray/video/7702605479670828318"},
		{"player file only", "https://www.tiktok.com/@stxtic_daniel/video/7701966030897286406"},
		{"player file beside renditions", "https://www.tiktok.com/@slvtredm/video/7686135998387883272"},
		{"fast import", "https://www.tiktok.com/@haze.effects/video/7699519368077626646"},
		{"photo post", "https://www.tiktok.com/@nasa/video/7701866987609197837"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			start := time.Now()
			post, err := Read(ctx, opts, c.link)
			if err != nil {
				t.Fatal(err)
			}
			data, err := post.Info()
			if err != nil {
				t.Fatal(err)
			}
			ours, err := ytdlp.ParseProbe(data)
			if err != nil {
				t.Fatal(err)
			}
			native := time.Since(start)
			start = time.Now()
			theirs, err := ytdlp.Probe(ctx, env, c.link, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("native %v, yt-dlp %v", native.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
			compareVideos(t, ours.Video, theirs.Video)
		})
	}
}

// A profile listed here and by yt-dlp's tiktok:user: the same posts in the same order, with
// what the grid shows of each. A photo post's link is its /photo/ one here (yt-dlp: /video/).
//
//	YTGUI_INTEGRATION=1 YTGUI_FORCE_IPV4=1 YTGUI_YTDLP=$PWD/yt-dlp go test ./internal/tiktok -run IntegrationProfile -v
func TestIntegrationProfileMatchesYtdlp(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	st := settings.Default()
	st.ForceIPv4 = os.Getenv("YTGUI_FORCE_IPV4") == "1"
	env := ytdlp.Env{Bin: bin, Settings: st}
	for _, link := range []string{"https://www.tiktok.com/@nasa", "https://www.tiktok.com/@tyke.ae"} {
		t.Run(link, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			const limit = 30
			start := time.Now()
			var first time.Duration
			ours, err := ListProfile(ctx, Options{IPv4: st.ForceIPv4}, link, limit, func(*ytdlp.ProbeResult) {
				if first == 0 {
					first = time.Since(start)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			native := time.Since(start)
			start = time.Now()
			theirs, err := ytdlp.List(ctx, env, link, limit)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("native: first page %v, %d posts %v; yt-dlp %v", first.Round(time.Millisecond), limit, native.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
			a, b := ours.Playlist, theirs.Playlist
			if a.ID != b.ID || a.Title != b.Title || !a.Channel || a.Profile == nil || a.Profile.Handle == "" {
				t.Errorf("listing: ours %+v\nyt-dlp's %+v", a, b)
			}
			t.Logf("profile: %+v", *a.Profile)
			if len(a.Entries) != len(b.Entries) {
				t.Fatalf("%d entries, yt-dlp has %d", len(a.Entries), len(b.Entries))
			}
			for i, e := range a.Entries {
				y := b.Entries[i]
				yURL := y.URL
				if strings.Contains(e.URL, "/photo/") {
					yURL = strings.Replace(yURL, "/video/", "/photo/", 1)
				}
				if e.ID != y.ID || e.URL != yURL || e.Title != y.Title || e.Duration != y.Duration ||
					e.Timestamp != y.Timestamp || e.Index != y.Index || e.Thumbnail == "" {
					t.Errorf("entry %d:\n ours   %+v\n yt-dlp %+v", i, e, y)
				}
			}
		})
	}
}
