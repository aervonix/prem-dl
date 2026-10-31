package ytdlp

import (
	"os"
	"strings"
	"testing"
	"time"
)

func probed(t *testing.T, extractor string, mod func(*VideoInfo)) *ProbeResult {
	t.Helper()
	data := []byte(`{"id":"abc","title":"T","webpage_url":"https://www.youtube.com/watch?v=abc","extractor_key":"` + extractor + `","formats":[]}`)
	res, err := ParseProbe(data)
	if err != nil {
		t.Fatal(err)
	}
	res.info = data
	if mod != nil {
		mod(res.Video)
	}
	return res
}

func TestInfoCacheGivesTheAnalysisOnce(t *testing.T) {
	c := NewInfoCache()
	res := probed(t, "Youtube", nil)
	c.Put("youtu.be/abc", res)

	// Queued under the video's own URL, which is not the link that was pasted.
	path, done := c.Take("https://www.youtube.com/watch?v=abc")
	if path == "" {
		t.Fatal("no analysis for the video's URL")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(res.info) {
		t.Fatalf("file = %q, %v", got, err)
	}
	done()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("done left the file behind")
	}
	// Used up under every link it was stored under: a retry reads the page.
	for _, link := range []string{"https://www.youtube.com/watch?v=abc", "https://youtu.be/abc"} {
		if p, _ := c.Take(link); p != "" {
			t.Errorf("%s given out twice", link)
		}
	}
}

func TestInfoCacheKeepsOnlyReusableVideos(t *testing.T) {
	cases := map[string]*ProbeResult{
		"another site": probed(t, "Vimeo", nil),
		"live":         probed(t, "Youtube", func(v *VideoInfo) { v.IsLive = true }),
		"upcoming":     probed(t, "Youtube", func(v *VideoInfo) { v.Upcoming = true }),
		"playlist":     {Kind: "playlist", Playlist: &PlaylistInfo{}},
	}
	for name, res := range cases {
		c := NewInfoCache()
		c.Put("https://www.youtube.com/watch?v=abc", res)
		if p, _ := c.Take("https://www.youtube.com/watch?v=abc"); p != "" {
			t.Errorf("%s was kept", name)
		}
	}
}

func TestInfoCacheExpiresAndClears(t *testing.T) {
	c := NewInfoCache()
	at := time.Now()
	c.now = func() time.Time { return at }
	c.Put("https://www.youtube.com/watch?v=abc", probed(t, "Youtube", nil))
	at = at.Add(InfoTTL + time.Second)
	if p, _ := c.Take("https://www.youtube.com/watch?v=abc"); p != "" {
		t.Error("an analysis older than InfoTTL was used")
	}

	c.Put("https://www.youtube.com/watch?v=abc", probed(t, "Youtube", nil))
	c.Clear()
	if p, _ := c.Take("https://www.youtube.com/watch?v=abc"); p != "" {
		t.Error("Clear kept an analysis")
	}
}

func TestInfoCacheIsBounded(t *testing.T) {
	c := NewInfoCache()
	at := time.Now()
	c.now = func() time.Time { at = at.Add(time.Millisecond); return at }
	for i := range maxInfos + 5 {
		res := probed(t, "Youtube", nil)
		res.Video.URL = "https://www.youtube.com/watch?v=" + strings.Repeat("x", i+1)
		c.Put(res.Video.URL, res)
	}
	if len(c.items) > maxInfos {
		t.Errorf("%d entries kept", len(c.items))
	}
	if p, _ := c.Take("https://www.youtube.com/watch?v=x"); p != "" {
		t.Error("the oldest entry should have gone first")
	}
	p, done := c.Take("https://www.youtube.com/watch?v=" + strings.Repeat("x", maxInfos+5))
	defer done()
	if p == "" {
		t.Error("the newest entry is missing")
	}
}

func TestDownloadArgsFromInfoFile(t *testing.T) {
	e := testEnv()
	args := e.DownloadArgs(e.Settings.Defaults, Target{URL: "https://example.com/v", InfoFile: "/tmp/i.json"})
	if args[len(args)-1] != "--load-info-json=/tmp/i.json" {
		t.Errorf("last argument = %q", args[len(args)-1])
	}
	if has(args, "--") || has(args, "https://example.com/v") {
		t.Errorf("the URL is read from the file, not given: %v", args)
	}
}

func TestInfoCacheHas(t *testing.T) {
	c := NewInfoCache()
	at := time.Now()
	c.now = func() time.Time { return at }
	if c.Has("https://www.youtube.com/watch?v=abc") {
		t.Fatal("empty cache has it")
	}
	c.Put("youtu.be/abc", probed(t, "Youtube", nil))
	if !c.Has("https://www.youtube.com/watch?v=abc") || !c.Has("youtu.be/abc") {
		t.Error("kept analysis not found")
	}
	at = at.Add(InfoTTL + time.Second)
	if c.Has("https://www.youtube.com/watch?v=abc") {
		t.Error("an old analysis counts")
	}
}

func TestIsYouTubeVideo(t *testing.T) {
	for link, want := range map[string]bool{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":      true,
		"youtube.com/watch?v=dQw4w9WgXcQ&list=PLx&index=2": true,
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":        true,
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ":    true,
		"https://youtu.be/dQw4w9WgXcQ?si=x":                true,
		"https://www.youtube.com/shorts/abc123":            true,
		"https://www.youtube.com/live/abc123":              true,
		"https://www.youtube.com/watch?list=PLx":           false,
		"https://www.youtube.com/playlist?list=PLx":        false,
		"https://www.youtube.com/@kurzgesagt":              false,
		"https://www.youtube.com/@kurzgesagt/videos":       false,
		"https://vimeo.com/76979871":                       false,
		"https://notyoutube.com/watch?v=dQw4w9WgXcQ":       false,
	} {
		if got := IsYouTubeVideo(link); got != want {
			t.Errorf("IsYouTubeVideo(%q) = %v", link, got)
		}
	}
}

// An analysis still running when cookies change finishes under the old ones: it isn't kept.
func TestInfoCacheDropsAnalysisFromBeforeClear(t *testing.T) {
	c := NewInfoCache()
	gen := c.Gen()
	c.Clear()
	c.PutFrom(gen, "youtu.be/abc", probed(t, "Youtube", nil))
	if c.Has("youtu.be/abc") {
		t.Error("kept an analysis that began before Clear")
	}
	c.PutFrom(c.Gen(), "youtu.be/abc", probed(t, "Youtube", nil))
	if !c.Has("youtu.be/abc") {
		t.Error("dropped a fresh analysis")
	}
}

func TestYouTubeVideoID(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s": "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?si=abc":               "dQw4w9WgXcQ",
		"https://m.youtube.com/shorts/abc":                  "abc",
		"https://music.youtube.com/watch?v=xyz":             "xyz",
		"https://www.youtube.com/live/live1":                "live1",
		"https://www.youtube.com/@chan/videos":              "",
		"https://www.youtube.com/playlist?list=PL1":         "",
		"https://vimeo.com/123":                             "",
	} {
		if got := YouTubeVideoID(link); got != want {
			t.Errorf("%s: %q, want %q", link, got, want)
		}
	}
}
