package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
	"yt-dlp-gui/internal/ytweb"
)

// fakeChannel serves internal/ytweb's recorded Videos tab of a channel (60 videos over a page
// and a continuation). release, when set, holds the continuation until it is closed.
func fakeChannel(t *testing.T, release chan struct{}) *httptest.Server {
	t.Helper()
	dir := filepath.Join("internal", "ytweb", "testdata", "browse", "tab_videos")
	read := func(name string) []byte {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		zr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var page struct {
		Ytcfg map[string]string `json:"ytcfg"`
		Data  json.RawMessage   `json:"data"`
	}
	if err := json.Unmarshal(read("page-videos.json.gz"), &page); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(page.Ytcfg)
	cont := read("api-2.json.gz")
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `<script>ytcfg.set(%s);</script><script>var ytInitialData = %s;</script>`, cfg, page.Data)
			return
		}
		// Read first: until then the server doesn't notice the client hanging up.
		_, _ = io.ReadAll(r.Body)
		if release != nil {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		mu.Lock()
		next := cont
		cont = []byte(`{"onResponseReceivedActions":[{"appendContinuationItemsAction":{"continuationItems":[]}}]}`)
		mu.Unlock()
		_, _ = w.Write(next)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func listingApp(t *testing.T, base string) *App {
	t.Helper()
	store, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, infos: ytdlp.NewInfoCache(), lister: ytweb.NewListerAt(base)}
	a.root, a.stopAll = context.WithCancel(context.Background())
	t.Cleanup(a.stopAll)
	return a
}

func TestProbeShowsAChannelBeforeItIsRead(t *testing.T) {
	release := make(chan struct{})
	a := listingApp(t, fakeChannel(t, release).URL)

	res := a.Probe("https://www.youtube.com/@kurzgesagt/videos", false)
	if res.Error != nil || res.Result == nil || res.Listing == 0 {
		t.Fatalf("%+v", res)
	}
	first := res.Result.Playlist.Entries
	if len(first) != 30 || first[0].Index != 1 || res.Result.Playlist.Title != "Kurzgesagt – In a Nutshell - Videos" {
		t.Fatalf("first part: %d entries, %+v", len(first), res.Result.Playlist)
	}
	// The continuation is still held back: the page has its first 30 already.
	close(release)
	var rest []ytdlp.PlaylistEntry
	for {
		part := a.MoreEntries(res.Listing)
		rest = append(rest, part.Entries...)
		if part.Done {
			if part.Error != "" {
				t.Fatal(part.Error)
			}
			break
		}
	}
	if len(rest) != 30 || rest[0].Index != 31 || rest[29].Index != 60 {
		t.Errorf("rest: %d entries from %d", len(rest), rest[0].Index)
	}
}

func TestAnotherAnalysisStopsTheListing(t *testing.T) {
	release := make(chan struct{}) // never closed: the continuation hangs
	a := listingApp(t, fakeChannel(t, release).URL)
	res := a.Probe("https://www.youtube.com/@kurzgesagt/videos", false)
	if res.Listing == 0 {
		t.Fatalf("%+v", res)
	}
	old := a.listing
	a.CancelProbe()
	<-waitDone(old)
	if part := a.MoreEntries(res.Listing); !part.Done || part.Error != "" || len(part.Entries) != 0 {
		t.Errorf("after cancelling: %+v", part)
	}
	// A listing that isn't the current one is over as far as the page is concerned.
	if part := a.MoreEntries(res.Listing + 1); !part.Done {
		t.Errorf("unknown listing: %+v", part)
	}
}

// waitDone is closed once l is done.
func waitDone(l *listing) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		for {
			l.mu.Lock()
			done, wake := l.done, l.wake
			l.mu.Unlock()
			if done {
				close(ch)
				return
			}
			<-wake
		}
	}()
	return ch
}

func TestListingThatCantStartIsLeftToYtdlp(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>consent</html>")
	}))
	t.Cleanup(broken.Close)
	a := listingApp(t, broken.URL)
	ctx, cancel := context.WithCancel(a.root)
	defer cancel()
	if _, _, ok := a.startListing(ctx, cancel, "https://www.youtube.com/@kurzgesagt/videos"); ok {
		t.Fatal("started")
	}
	if ctx.Err() != nil {
		t.Error("the context yt-dlp would use was cancelled")
	}
	if _, _, ok := a.startListing(ctx, cancel, "https://www.youtube.com/watch?v=dQw4w9WgXcQ"); ok {
		t.Error("a video was listed")
	}
}
