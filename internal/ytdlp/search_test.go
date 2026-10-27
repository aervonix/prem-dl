package ytdlp

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/settings"
)

func TestSearchURL(t *testing.T) {
	// The sp values are the ones YouTube's filter menu puts in its own links.
	cases := []struct {
		f      SearchFilter
		wantSP string
	}{
		{SearchFilter{}, ""},
		{SearchFilter{Type: "video"}, "EgIQAQ=="},
		{SearchFilter{Type: "playlist"}, "EgIQAw=="},
		{SearchFilter{Uploaded: "week", Type: "video"}, "EgQIAxAB"},
		{SearchFilter{Type: "video", Length: "long"}, "EgQQARgC"},
		{SearchFilter{Type: "nonsense"}, ""},
	}
	for _, c := range cases {
		u, err := url.Parse(SearchURL("  lofi hip hop ", c.f))
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("search_query") != "lofi hip hop" || q.Get("sp") != c.wantSP {
			t.Errorf("%+v: got %s", c.f, u)
		}
	}
	// Anything typed stays inside the query parameter.
	u := SearchURL("a&sp=x --exec rm", SearchFilter{})
	if !strings.HasPrefix(u, "https://www.youtube.com/results?search_query=a%26sp%3Dx+--exec+rm") {
		t.Errorf("not escaped: %s", u)
	}
}

// The shape of yt-dlp's answer for a results page, trimmed from a real one: a video, and a
// channel whose avatar link has no scheme.
func TestParseSearchResults(t *testing.T) {
	res, err := ParseProbe([]byte(`{"_type": "playlist", "id": "rick astley", "title": "rick astley",
	 "webpage_url": "https://www.youtube.com/results?search_query=rick+astley", "entries": [
	 {"_type": "url", "ie_key": "Youtube", "id": "dQw4w9WgXcQ", "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	  "title": "Rick Astley - Never Gonna Give You Up", "duration": 213, "view_count": 1800000000, "timestamp": 1256688000,
	  "channel": "Rick Astley", "thumbnails": [{"url": "https://i.ytimg.com/vi/dQw4w9WgXcQ/hq720.jpg", "width": 360, "height": 202}]},
	 {"_type": "url", "ie_key": "YoutubeTab", "id": "UC_6hX_CXZfLlqFJTD8hehv", "url": "https://www.youtube.com/channel/UC_6hX_CXZfLlqFJTD8hehv",
	  "title": "Rick Astley", "thumbnails": [{"url": "//yt3.ggpht.com/jhn2xLQ=s88", "width": 88, "height": 88}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	es := res.Playlist.Entries
	if len(es) != 2 {
		t.Fatalf("want 2 entries, got %+v", es)
	}
	if v := es[0]; v.IsPlaylist || v.Uploader != "Rick Astley" || v.ViewCount != 1800000000 || v.Timestamp != 1256688000 || v.Thumbnail == "" {
		t.Errorf("video: %+v", v)
	}
	if c := es[1]; !c.IsPlaylist || c.Thumbnail != "https://yt3.ggpht.com/jhn2xLQ=s88" {
		t.Errorf("channel: %+v", c)
	}
}

func TestSearchRejectsEmptyAndLong(t *testing.T) {
	for _, q := range []string{"", "   ", strings.Repeat("é", MaxQuery+1)} {
		_, err := Search(context.Background(), Env{Bin: "does-not-exist"}, q, SearchFilter{}, 1, 20)
		var ae *apperr.AppError
		if !errors.As(err, &ae) {
			t.Errorf("%q: want an AppError, got %v", q, err)
		}
	}
}

// Opt-in, against the real yt-dlp and YouTube:
//
//	$env:YTGUI_INTEGRATION = "1"; $env:YTGUI_YTDLP = "C:\path\to\yt-dlp.exe"; go test ./internal/ytdlp -run IntegrationSearch -v
func TestIntegrationSearch(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	env := Env{Bin: bin, Settings: settings.Default(), JSRuntimes: DetectJSRuntimes(ctx)}

	res, err := Search(ctx, env, "rick astley never gonna give you up", SearchFilter{Type: "video"}, 21, 10)
	if err != nil {
		t.Fatal(err)
	}
	es := res.Playlist.Entries
	if len(es) != 10 || es[0].Index != 21 || es[9].Index != 30 {
		t.Fatalf("want results 21-30, got %d starting at %d", len(es), es[0].Index)
	}
	for _, e := range es {
		if e.IsPlaylist || !strings.HasPrefix(e.URL, "https://www.youtube.com/watch?v=") || e.Title == "" || e.Thumbnail == "" {
			t.Errorf("not a video result: %+v", e)
		}
	}
	dated := 0
	for _, e := range es {
		if e.Timestamp > 0 {
			dated++
		}
	}
	if dated == 0 {
		t.Error("no result has an (approximate) upload date")
	}

	res, err = Search(ctx, env, "rick astley", SearchFilter{Type: "channel"}, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Playlist.Entries {
		if !e.IsPlaylist {
			t.Errorf("channel result not marked as a collection: %+v", e)
		}
	}
}
