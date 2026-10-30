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
