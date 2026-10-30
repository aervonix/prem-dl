package ytweb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

// fakeYouTube serves a fixture's pages as the search API does: the first for a query, the next
// for each page's continuation token. It records what it was asked.
type fakeYouTube struct {
	t      *testing.T
	pages  [][]byte
	tokens map[string]int // continuation token -> page index it asks for
	mu     sync.Mutex
	asked  []map[string]any
	header []http.Header
	status int    // answer with this instead, when set
	body   string // answer with this instead, when set
}

func newFake(t *testing.T, name string) (*fakeYouTube, *Searcher) {
	t.Helper()
	f := &fakeYouTube{t: t, tokens: map[string]int{}}
	_, now := expected(t, name)
	for i := 1; ; i++ {
		path := filepath.Join("testdata", "search", name, "page"+strconv.Itoa(i)+".json.gz")
		if !exists(path) {
			break
		}
		data := readGz(t, path)
		p, err := parseSearch(data, extractor{now: now, dates: true})
		if err != nil {
			t.Fatal(err)
		}
		f.pages = append(f.pages, data)
		f.tokens[p.next] = i // page i's token asks for page i+1 (index i)
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s := NewAt(srv.URL + "/youtubei/v1/search?prettyPrint=false")
	s.now = func() time.Time { return now }
	return f, s
}

func (f *fakeYouTube) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		f.t.Errorf("request body: %v", err)
	}
	f.mu.Lock()
	f.asked = append(f.asked, body)
	f.header = append(f.header, r.Header.Clone())
	status, override := f.status, f.body
	f.mu.Unlock()
	switch {
	case status != 0:
		w.WriteHeader(status)
	case override != "":
		_, _ = io.WriteString(w, override)
	case body["continuation"] != nil:
		i, ok := f.tokens[body["continuation"].(string)]
		if !ok || i >= len(f.pages) {
			// Past the fixture: the last page has no more results.
			_, _ = io.WriteString(w, `{"onResponseReceivedCommands":[{"appendContinuationItemsAction":{"continuationItems":[]}}]}`)
			return
		}
		_, _ = w.Write(f.pages[i])
	default:
		// An anonymous first page carries a visitor ID (yt-dlp sent its own, so the fixture lacks it).
		var first map[string]any
		_ = json.Unmarshal(f.pages[0], &first)
		first["responseContext"] = map[string]any{"visitorData": "fake-visitor"}
		_ = json.NewEncoder(w).Encode(first)
	}
}

func (f *fakeYouTube) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

func (f *fakeYouTube) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.requests() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d requests, want %d", f.requests(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ids(res *ytdlp.ProbeResult) []string {
	var out []string
	for _, e := range res.Playlist.Entries {
		out = append(out, e.ID)
	}
	return out
}

func TestFirstPageComesBackOnItsOwnAndTheNextIsReadAhead(t *testing.T) {
	f, s := newFake(t, "videos")
	ctx := context.Background()
	res, more, err := s.Search(ctx, Options{}, "lofi beats", ytdlp.SearchFilter{}, 1, 24)
	if err != nil {
		t.Fatal(err)
	}
	n := len(res.Playlist.Entries)
	if n == 0 || n >= 24 || !more {
		t.Fatalf("first call: %d results, more=%v; want page 1's alone, with more", n, more)
	}
	if res.Playlist.Entries[0].Index != 1 || res.Playlist.Title != "lofi beats" {
		t.Errorf("numbering or title: %+v", res.Playlist)
	}
	// The next page is read while the first is on screen...
	f.waitFor(t, 2)
	// ...so "Show more" needs no request of its own, and starts the one after.
	res2, more, err := s.Search(ctx, Options{}, "lofi beats", ytdlp.SearchFilter{}, n+1, 24)
	if err != nil || !more || len(res2.Playlist.Entries) == 0 {
		t.Fatalf("show more: %v, more=%v, %d results", err, more, len(res2.Playlist.Entries))
	}
	if res2.Playlist.Entries[0].Index != n+1 {
		t.Errorf("show more starts at %d", res2.Playlist.Entries[0].Index)
	}
	f.waitFor(t, 3)
	time.Sleep(50 * time.Millisecond)
	if got := f.requests(); got != 3 {
		t.Errorf("%d requests for two pages shown and one read ahead", got)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked[0]["query"] != "lofi beats" || f.asked[0]["params"] != nil || f.asked[0]["continuation"] != nil {
		t.Errorf("first request: %v", f.asked[0])
	}
	if f.asked[1]["continuation"] == nil || f.asked[1]["query"] != nil {
		t.Errorf("second request continues the first: %v", f.asked[1])
	}
	client := f.asked[0]["context"].(map[string]any)["client"].(map[string]any)
	if client["clientName"] != "WEB" || client["hl"] != "en" || client["clientVersion"] != clientVersion {
		t.Errorf("client context: %v", client)
	}
	h := f.header[1]
	if h.Get("X-Goog-Visitor-Id") != "fake-visitor" || h.Get("Cookie") != cookies || h.Get("X-YouTube-Client-Version") != clientVersion {
		t.Errorf("headers: %v", h)
	}
}

func TestSparseResultsAreReadUntilThereAreEnough(t *testing.T) {
	// A playlist search's pages hold ~5 playlists among courses, which are left out.
	f, s := newFake(t, "playlists")
	res, more, err := s.Search(context.Background(), Options{}, "python tutorial", ytdlp.SearchFilter{Type: "playlist"}, 1, 24)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res.Playlist.Entries); n < 18 || !more {
		t.Errorf("%d results after %d pages, more=%v; want 18 or more", n, f.requests(), more)
	}
}

func TestFilterGoesInParams(t *testing.T) {
	f, s := newFake(t, "channels")
	if _, _, err := s.Search(context.Background(), Options{}, "kurzgesagt", ytdlp.SearchFilter{Type: "channel"}, 1, 24); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked[0]["params"] != "EgIQAg==" {
		t.Errorf("params = %v", f.asked[0]["params"])
	}
}

func TestSearchingAgainStartsOver(t *testing.T) {
	f, s := newFake(t, "videos")
	ctx := context.Background()
	first, _, _ := s.Search(ctx, Options{}, "lofi beats", ytdlp.SearchFilter{}, 1, 24)
	f.waitFor(t, 2)
	// The same search from the top asks YouTube again: its results may have changed.
	again, _, err := s.Search(ctx, Options{}, "lofi beats", ytdlp.SearchFilter{}, 1, 24)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	restarted := f.asked[2]["query"] == "lofi beats"
	f.mu.Unlock()
	if !restarted || len(ids(again)) != len(ids(first)) {
		t.Errorf("searching again didn't start from the first page")
	}
}

func TestEndOfResults(t *testing.T) {
	f, s := newFake(t, "videos")
	ctx := context.Background()
	total, more := 0, true
	for calls := 0; more && calls < 10; calls++ {
		res, m, err := s.Search(ctx, Options{}, "lofi beats", ytdlp.SearchFilter{}, total+1, 24)
		if err != nil {
			t.Fatal(err)
		}
		total += len(res.Playlist.Entries)
		more = m
	}
	if more {
		t.Fatal("never reached the end")
	}
	want := 0
	_, now := expected(t, "videos")
	for _, data := range f.pages {
		p, _ := parseSearch(data, extractor{now: now, dates: true})
		want += len(p.entries)
	}
	if total != want {
		t.Errorf("%d results over all pages, want %d", total, want)
	}
}

func TestFailuresAreErrors(t *testing.T) {
	for name, setup := range map[string]func(*fakeYouTube){
		"forbidden":    func(f *fakeYouTube) { f.status = http.StatusForbidden },
		"not JSON":     func(f *fakeYouTube) { f.body = "<html>consent</html>" },
		"other layout": func(f *fakeYouTube) { f.body = `{"contents":{"somethingNew":{}}}` },
	} {
		f, s := newFake(t, "videos")
		setup(f)
		if _, _, err := s.Search(context.Background(), Options{}, "lofi beats", ytdlp.SearchFilter{}, 1, 24); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestEmptySearch(t *testing.T) {
	f, s := newFake(t, "videos")
	// Captured shape of a search with no results: the list is there, with nothing in it.
	f.body = `{"contents":{"twoColumnSearchResultsRenderer":{"primaryContents":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"backgroundPromoRenderer":{}}]}}]}}}}}`
	res, more, err := s.Search(context.Background(), Options{}, "zzqqxx", ytdlp.SearchFilter{}, 1, 24)
	if err != nil || more || len(res.Playlist.Entries) != 0 {
		t.Errorf("%v, more=%v, %+v", err, more, res)
	}
}

func TestCancelledWhileWaitingForARead(t *testing.T) {
	_, s := newFake(t, "videos")
	sess := s.session(Options{}, "lofi beats", "", true)
	sess.lock <- struct{}{} // a read in progress
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.Search(ctx, Options{}, "lofi beats", ytdlp.SearchFilter{}, 21, 24); err != context.Canceled {
		t.Errorf("err = %v", err)
	}
}
