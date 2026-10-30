package ytweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

// Each folder in testdata/browse holds what yt-dlp 2026.10.26 read for one listing
// (--write-pages: the embedded data of each web page, its client settings, the API's answers in
// order, trimmed to what a parser reads) and what it printed for them with the app's arguments
// (--dump-single-json --flat-playlist --extractor-args=youtubetab:approximate_date
// --playlist-end=N).
var browseCases = []struct {
	name, link string
	limit      int
}{
	{"tab_videos", "https://www.youtube.com/@kurzgesagt/videos", 60},
	{"tab_shorts", "https://www.youtube.com/@kurzgesagt/shorts", 60},
	{"tab_streams", "https://www.youtube.com/@LofiGirl/streams", 40},
	{"tab_playlists", "https://www.youtube.com/@kurzgesagt/playlists", 40},
	{"home", "https://www.youtube.com/@LofiGirl", 40},
	{"playlist", "https://www.youtube.com/playlist?list=UUsXVk37bltHxD1rDPwtNM8Q", 150},
}

// fakeChannels serves a fixture: channel pages as HTML laid out like YouTube's, the playlist
// reload (api-1) and continuations (the other api-N files, in order).
type fakeChannels struct {
	t      *testing.T
	dir    string
	mu     sync.Mutex
	api    []string // continuation answers not yet given
	served []string // everything answered, for checking each token came from one of them
	gets   []string
	posts  []map[string]any
}

func newFakeChannels(t *testing.T, name string) (*fakeChannels, *Lister) {
	f := &fakeChannels{t: t, dir: filepath.Join("testdata", "browse", name)}
	files, _ := filepath.Glob(filepath.Join(f.dir, "api-*.json.gz"))
	sort.Strings(files)
	for _, p := range files {
		if !strings.HasSuffix(p, "api-1.json.gz") {
			f.api = append(f.api, p)
		}
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	// "3 years ago" is dated from when yt-dlp read the pages.
	var exp struct {
		Epoch int64 `json:"epoch"`
	}
	if err := json.Unmarshal(readGz(t, filepath.Join(f.dir, "expected.json.gz")), &exp); err != nil || exp.Epoch == 0 {
		t.Fatalf("expected.json's epoch: %v", err)
	}
	l := NewListerAt(srv.URL)
	l.now = func() time.Time { return time.Unix(exp.Epoch, 0) }
	return f, l
}

func (f *fakeChannels) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodGet {
		f.gets = append(f.gets, r.URL.Path)
		tab := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		path := filepath.Join(f.dir, "page-"+tab+".json.gz")
		if !exists(path) {
			http.NotFound(w, r)
			return
		}
		var page struct {
			Ytcfg map[string]string `json:"ytcfg"`
			Data  json.RawMessage   `json:"data"`
		}
		if err := json.Unmarshal(readGz(f.t, path), &page); err != nil {
			f.t.Error(err)
		}
		cfg, _ := json.Marshal(page.Ytcfg)
		// As on YouTube: a script that names ytInitialData before the one that sets it.
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><script nonce="n">(function(a){ytPageType(a.ytInitialData,a.ytInitialPlayerResponse)})</script>`+
			`<script nonce="n">ytcfg.set(%s);</script></head><body><script nonce="n">var ytInitialData = %s;</script></body></html>`, cfg, page.Data)
		f.served = append(f.served, string(page.Data))
		return
	}
	data, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		f.t.Errorf("request body: %v", err)
	}
	f.posts = append(f.posts, body)
	var file string
	switch {
	case body["browseId"] != nil:
		file = filepath.Join(f.dir, "api-1.json.gz")
	case body["continuation"] != nil:
		tok, _ := body["continuation"].(string)
		found := false
		for _, s := range f.served {
			found = found || strings.Contains(s, tok)
		}
		if !found {
			f.t.Errorf("continuation %.20s… isn't in any page served", tok)
		}
		if len(f.api) == 0 {
			_, _ = io.WriteString(w, `{"onResponseReceivedActions":[{"appendContinuationItemsAction":{"continuationItems":[]}}]}`)
			return
		}
		file, f.api = f.api[0], f.api[1:]
	}
	raw := readGz(f.t, file)
	f.served = append(f.served, string(raw))
	_, _ = w.Write(raw)
}

// expectedListing is yt-dlp's answer for a fixture, read as the app reads it.
func expectedListing(t *testing.T, name string) *ytdlp.ProbeResult {
	t.Helper()
	res, err := ytdlp.ParseProbe(readGz(t, filepath.Join("testdata", "browse", name, "expected.json.gz")))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestListsLikeYtdlp(t *testing.T) {
	for _, c := range browseCases {
		t.Run(c.name, func(t *testing.T) {
			f, l := newFakeChannels(t, c.name)
			got, err := l.List(context.Background(), Options{}, c.link, c.limit)
			if err != nil {
				t.Fatal(err)
			}
			want := expectedListing(t, c.name)
			g, w := *got.Playlist, *want.Playlist
			if len(g.Entries) != len(w.Entries) {
				t.Errorf("%d entries, yt-dlp %d", len(g.Entries), len(w.Entries))
			}
			for i := range min(len(g.Entries), len(w.Entries)) {
				if !reflect.DeepEqual(g.Entries[i], w.Entries[i]) {
					t.Errorf("entry %d:\n got %+v\nwant %+v", i+1, g.Entries[i], w.Entries[i])
				}
			}
			g.Entries, w.Entries = nil, nil
			if got.Kind != want.Kind || !reflect.DeepEqual(g, w) {
				t.Errorf("listing:\n got %s %+v\nwant %s %+v", got.Kind, g, want.Kind, w)
			}
			if len(f.api) > 0 {
				t.Errorf("%d continuation pages left unread", len(f.api))
			}
		})
	}
}

func TestHandles(t *testing.T) {
	for link, want := range map[string]bool{
		"https://www.youtube.com/@kurzgesagt":                                     true,
		"youtube.com/@kurzgesagt/videos":                                          true,
		"https://m.youtube.com/@kurzgesagt/shorts":                                true,
		"https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q":                true,
		"https://www.youtube.com/c/kurzgesagt/streams":                            true,
		"https://www.youtube.com/user/kurzgesagt/playlists":                       true,
		"https://www.youtube.com/playlist?list=PLFs4vir_WsTyXrrpFstD6":            true,
		"https://www.youtube.com/playlist?list=UUsXVk37bltHxD1rDPwtNM8Q&si=x":     true,
		"https://www.youtube.com/@kurzgesagt/featured":                            false, // shelves, not a list
		"https://www.youtube.com/@kurzgesagt/posts":                               false,
		"https://www.youtube.com/@kurzgesagt/live":                                false, // a live stream, if one is on
		"https://www.youtube.com/@kurzgesagt/Videos":                              false,
		"https://www.youtube.com/@kurzgesagt/videos?view=0":                       false,
		"https://www.youtube.com/@kurzgesagt/videos/extra":                        false,
		"https://www.youtube.com/playlist?list=RDdQw4w9WgXcQ":                     false, // a mix
		"https://www.youtube.com/playlist?list=WL":                                false, // Watch later
		"https://www.youtube.com/playlist?list=LL":                                false,
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLFs4vir_WsTyXrrpFstD6": false,
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":                             false,
		"https://vimeo.com/channels/staffpicks":                                   false,
		"https://www.youtube.com/@kurzgesagt/videos#top":                          false,
	} {
		if got := Handles(link); got != want {
			t.Errorf("Handles(%q) = %v", link, got)
		}
	}
}

// A page yt-dlp would stop at, or list some other way, is left to it.
func TestLeavesOddPagesToYtdlp(t *testing.T) {
	page := func(data map[string]any) string {
		raw, _ := json.Marshal(data)
		return `<script>var ytInitialData = ` + string(raw) + `;</script>`
	}
	tabs := func(selected string) map[string]any {
		var ts []any
		for _, id := range []string{"videos", "shorts"} {
			ts = append(ts, map[string]any{"tabRenderer": map[string]any{
				"title": id, "selected": id == selected,
				"endpoint": map[string]any{"commandMetadata": map[string]any{"webCommandMetadata": map[string]any{"url": "/@x/" + id}}},
				"content":  map[string]any{"richGridRenderer": map[string]any{"contents": []any{}}},
			}})
		}
		return map[string]any{"twoColumnBrowseResultsRenderer": map[string]any{"tabs": ts}}
	}
	for name, html := range map[string]string{
		"error alert": page(map[string]any{"contents": tabs("videos"),
			"alerts": []any{map[string]any{"alertRenderer": map[string]any{"type": "ERROR", "text": map[string]any{"simpleText": "This channel does not exist."}}}}}),
		"regional redirect": page(map[string]any{"contents": tabs("videos"),
			"onResponseReceivedActions": []any{map[string]any{"navigateAction": map[string]any{"endpoint": map[string]any{"commandMetadata": map[string]any{"webCommandMetadata": map[string]any{"url": "/channel/UCother"}}}}}}}),
		"tab missing": page(map[string]any{"contents": tabs("videos")}), // asked for streams
		"no data":     "<html>consent</html>",
		"no contents": page(map[string]any{"header": map[string]any{}}),
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, html) }))
		_, err := NewListerAt(srv.URL).List(context.Background(), Options{}, "https://www.youtube.com/@x/streams", 10)
		srv.Close()
		if err == nil {
			t.Errorf("%s: listed", name)
		}
	}
}

func TestHomeReadsOtherTabsAlongside(t *testing.T) {
	f, l := newFakeChannels(t, "home")
	if _, err := l.List(context.Background(), Options{}, "https://www.youtube.com/@LofiGirl", 40); err != nil {
		t.Fatal(err)
	}
	sort.Strings(f.gets)
	if want := []string{"/@LofiGirl/shorts", "/@LofiGirl/streams", "/@LofiGirl/videos"}; !reflect.DeepEqual(f.gets, want) {
		t.Errorf("pages read: %v", f.gets)
	}
	for _, p := range f.posts {
		client := p["context"].(map[string]any)["client"].(map[string]any)
		if client["clientVersion"] == clientVersion {
			t.Errorf("a continuation used yt-dlp's client version, not the page's")
		}
	}
}

func TestUnreadablePlaylistIsLeftToYtdlp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Captured shape of a playlist that doesn't exist.
		_, _ = io.WriteString(w, `{"alerts":[{"alertRenderer":{"type":"ERROR","text":{"runs":[{"text":"The playlist does not exist."}]}}}],"contents":{}}`)
	}))
	defer srv.Close()
	if _, err := NewListerAt(srv.URL).List(context.Background(), Options{}, "https://www.youtube.com/playlist?list=PLnothing000", 0); err == nil {
		t.Error("listed")
	}
}
