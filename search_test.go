package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
	"yt-dlp-gui/internal/ytsearch"
)

// Opt-in: when reading YouTube's results without yt-dlp fails (YouTube changed its pages, say),
// yt-dlp searches instead and the user sees results all the same.
//
//	YTGUI_INTEGRATION=1 YTGUI_FORCE_IPV4=1 YTGUI_YTDLP=$PWD/yt-dlp go test . -run IntegrationSearch -v
func TestIntegrationSearchFallsBackToYtdlp(t *testing.T) {
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
	defer broken.Close()

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
		searcher: ytsearch.NewAt(broken.URL)}
	a.root, a.stopAll = context.WithCancel(context.Background())
	defer a.stopAll()

	res := a.Search("lofi beats", ytdlp.SearchFilter{}, 1, 10)
	if res.Error != nil || res.Result == nil || len(res.Result.Playlist.Entries) != 10 || !res.More {
		t.Fatalf("%+v", res)
	}
	t.Logf("yt-dlp found %q first", res.Result.Playlist.Entries[0].Title)
}
