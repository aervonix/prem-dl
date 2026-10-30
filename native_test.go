package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
	"yt-dlp-gui/internal/ytweb"
)

// Opt-in: when reading YouTube without yt-dlp fails (YouTube changed its pages, say), yt-dlp
// reads it instead and the user sees results all the same.
//
//	YTGUI_INTEGRATION=1 YTGUI_FORCE_IPV4=1 YTGUI_YTDLP=$PWD/yt-dlp go test . -run FallsBackToYtdlp -v
func TestIntegrationSearchFallsBackToYtdlp(t *testing.T) {
	a := fallbackApp(t)
	res := a.Search("lofi beats", ytdlp.SearchFilter{}, 1, 10)
	if res.Error != nil || res.Result == nil || len(res.Result.Playlist.Entries) != 10 || !res.More {
		t.Fatalf("%+v", res)
	}
	t.Logf("yt-dlp found %q first", res.Result.Playlist.Entries[0].Title)
}

func TestIntegrationListingFallsBackToYtdlp(t *testing.T) {
	a := fallbackApp(t)
	res := a.Probe("https://www.youtube.com/@kurzgesagt/playlists", false)
	if res.Error != nil || res.Result == nil || res.Result.Kind != "playlist" || len(res.Result.Playlist.Entries) == 0 {
		t.Fatalf("%+v", res)
	}
	t.Logf("yt-dlp listed %d playlists", len(res.Result.Playlist.Entries))
}

// fallbackApp is an App whose own YouTube reading always fails, with the real yt-dlp.
func fallbackApp(t *testing.T) *App {
	t.Helper()
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	// buildEnv finds yt-dlp in the working folder.
	wd := t.TempDir()
	if err := os.Symlink(bin, filepath.Join(wd, ytdlp.Name())); err != nil {
		t.Skip("can't link yt-dlp here (Windows needs developer mode):", err)
	}
	t.Chdir(wd)

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"contents":{"somethingNew":{}}}`))
	}))
	t.Cleanup(broken.Close)

	dir := t.TempDir()
	store, err := settings.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := store.Get()
	s.ForceIPv4 = os.Getenv("YTGUI_FORCE_IPV4") == "1"
	if _, err := store.Set(s); err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: dir, store: store, versions: map[versionKey]string{}, toolsFirst: make(chan struct{}),
		infos: ytdlp.NewInfoCache(), searcher: ytweb.NewAt(broken.URL), lister: ytweb.NewListerAt(broken.URL)}
	a.root, a.stopAll = context.WithCancel(context.Background())
	t.Cleanup(a.stopAll)
	return a
}

// Opt-in: a channel analysed on the Download page comes back after its first page, and the
// rest, read in parts, is the whole channel.
func TestIntegrationProbeListsAChannelInParts(t *testing.T) {
	if os.Getenv("YTGUI_INTEGRATION") != "1" {
		t.Skip("set YTGUI_INTEGRATION=1 to run")
	}
	store, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := store.Get()
	s.ForceIPv4 = os.Getenv("YTGUI_FORCE_IPV4") == "1"
	if _, err := store.Set(s); err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, infos: ytdlp.NewInfoCache(), lister: ytweb.NewLister()}
	a.root, a.stopAll = context.WithCancel(context.Background())
	defer a.stopAll()

	for _, link := range []string{"https://www.youtube.com/@MrBeast", "https://www.youtube.com/playlist?list=UUsXVk37bltHxD1rDPwtNM8Q"} {
		start := time.Now()
		res := a.Probe(link, false)
		first := time.Since(start)
		if res.Error != nil || res.Listing == 0 {
			t.Fatalf("%s: %+v", link, res)
		}
		entries := res.Result.Playlist.Entries
		for {
			part := a.MoreEntries(res.Listing)
			entries = append(entries, part.Entries...)
			if part.Done {
				if part.Error != "" {
					t.Fatal(part.Error)
				}
				break
			}
		}
		total := time.Since(start)
		whole, err := a.lister.List(context.Background(), ytweb.Options{IPv4: s.ForceIPv4}, link, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: first %d entries after %d ms, all %d after %d ms (read whole: %d)", link,
			len(res.Result.Playlist.Entries), first.Milliseconds(), len(entries), total.Milliseconds(), len(whole.Playlist.Entries))
		if len(entries) != len(whole.Playlist.Entries) {
			t.Errorf("%d entries in parts, %d read whole", len(entries), len(whole.Playlist.Entries))
		}
		for i, e := range entries {
			if e.Index != i+1 {
				t.Fatalf("entry %d numbered %d", i+1, e.Index)
			}
		}
	}
}

// Opt-in: analysing a video starts Prem-dl's challenge solver and yt-dlp uses it; the second
// analysis has the player parsed already.
func TestIntegrationProbeUsesTheChallengeSolver(t *testing.T) {
	a := fallbackApp(t) // its own search and listing fail; analysing a video is yt-dlp's
	for i := 1; i <= 2; i++ {
		start := time.Now()
		res := a.Probe("https://www.youtube.com/watch?v=dQw4w9WgXcQ", true)
		if res.Error != nil || res.Result == nil || res.Result.Video == nil || len(res.Result.Video.Formats) == 0 {
			t.Fatalf("analysis %d: %+v", i, res)
		}
		a.jscMu.Lock()
		srv := a.jscSrv
		a.jscMu.Unlock()
		if srv == nil || srv.Solved() < int64(i) {
			t.Fatalf("analysis %d didn't go through the solver (server %v)", i, srv != nil)
		}
		t.Logf("analysis %d: %d ms, %d requests solved so far", i, time.Since(start).Milliseconds(), srv.Solved())
	}
}
