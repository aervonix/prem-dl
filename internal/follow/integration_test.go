package follow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// Opt-in, against the real yt-dlp and YouTube:
//
//	$env:YTGUI_INTEGRATION = "1"; $env:YTGUI_YTDLP = "C:\path\to\yt-dlp.exe"; go test ./internal/follow -run Integration -v
func TestIntegrationFollowFindsNewUploads(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	s := settings.Default()
	s.ForceIPv4 = os.Getenv("YTGUI_FORCE_IPV4") == "1" // for a network whose IPv6 doesn't work
	env := ytdlp.Env{Bin: bin, Settings: s, JSRuntimes: ytdlp.DetectJSRuntimes(context.Background())}
	var queued []queue.NewJob
	m := New(Deps{
		List: func(ctx context.Context, link string, limit int) (*ytdlp.ProbeResult, error) {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			return ytdlp.List(ctx, env, link, limit)
		},
		Enqueue: func(items []queue.NewJob, _ settings.DownloadOptions) (int, error) {
			queued = append(queued, items...)
			return len(items), nil
		},
		Every: func() time.Duration { return 6 * time.Hour },
	})
	f, err := m.Follow(NewFeed{URL: "https://www.youtube.com/@RickAstleyYT/videos", Options: settings.Default().Defaults})
	if err != nil {
		t.Fatal(err)
	}
	m.check(f.ID)
	got := get(t, m, f.ID)
	if !got.Ready || got.Error != nil || len(queued) != 0 {
		t.Fatalf("baseline: %+v, queued %d", got, len(queued))
	}
	if got.Title != "Rick Astley" {
		t.Errorf("named %q, want the channel's name", got.Title)
	}

	// Pretend the two newest uploads appeared after the baseline.
	m.mu.Lock()
	r := m.findLocked(f.ID)
	top, _, err := m.listFeed(r.Feed, nil)
	if err != nil || len(top) < 3 {
		m.mu.Unlock()
		t.Fatalf("listing: %v, %d entries", err, len(top))
	}
	for _, it := range top[:2] {
		delete(r.seen, it.ID)
	}
	m.mu.Unlock()

	m.check(f.ID)
	if len(queued) != 2 {
		t.Fatalf("queued %d, want 2", len(queued))
	}
	// Oldest first: the second-newest upload is queued before the newest.
	if queued[0].Title != top[1].Title || queued[1].Title != top[0].Title {
		t.Errorf("queued %q then %q; listing starts %q, %q", queued[0].Title, queued[1].Title, top[0].Title, top[1].Title)
	}
	for _, j := range queued {
		if !strings.HasPrefix(j.URL, "https://www.youtube.com/watch?v=") || j.PlaylistTitle != "Rick Astley" {
			t.Errorf("job %+v", j)
		}
	}
	t.Logf("queued %q and %q", queued[0].Title, queued[1].Title)
}
