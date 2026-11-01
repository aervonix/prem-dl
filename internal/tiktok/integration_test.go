package tiktok

import (
	"context"
	"os"
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
