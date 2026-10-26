package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

func TestBundledYtdlpWanted(t *testing.T) {
	none := settings.Settings{}
	custom := settings.Settings{YtdlpPath: `C:\tools\yt-dlp.exe`}
	at := func(src string) ytdlp.Binary { return ytdlp.Binary{Path: `C:\x\yt-dlp.exe`, Source: src} }
	cases := []struct {
		name    string
		s       settings.Settings
		found   ytdlp.Binary
		err     error
		managed string
		want    bool
	}{
		{"none anywhere", none, ytdlp.Binary{}, ytdlp.ErrNotFound, "", true},
		{"configured path, even missing", custom, ytdlp.Binary{}, ytdlp.ErrNotFound, "", false},
		{"next to the app", none, at(ytdlp.SourceApp), nil, "", false},
		{"working directory", none, at(ytdlp.SourceCwd), nil, "", false},
		{"on PATH", none, at(ytdlp.SourcePath), nil, "", true},
		{"own copy older", none, at(ytdlp.SourceManaged), nil, "2026.09.01", true},
		{"own copy the same", none, at(ytdlp.SourceManaged), nil, "2026.10.21", false},
		{"own copy updated since", none, at(ytdlp.SourceManaged), nil, "2026.11.02", false},
		{"own copy doesn't run", none, at(ytdlp.SourceManaged), nil, "", true},
		{"some other failure", none, ytdlp.Binary{}, errors.New("boom"), "", false},
	}
	for _, c := range cases {
		if got := bundledYtdlpWanted(c.s, c.found, c.err, c.managed, "2026.10.21"); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// Opt-in: unpacks a real yt-dlp the way a bundled build does.
//
//	$env:YTGUI_INTEGRATION = "1"; $env:YTGUI_YTDLP = "C:\path\to\yt-dlp.exe"; go test . -run Integration -v
func TestIntegrationUnpackBundle(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to yt-dlp> to run")
	}
	exe, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	version, err := ytdlp.Version(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	// Away from the repository, whose own yt-dlp.exe would be found in the working directory.
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")
	old := bundleFS
	bundleFS = fstest.MapFS{"yt-dlp.exe": {Data: exe}, "yt-dlp.version": {Data: []byte(version + "\n")}}
	defer func() { bundleFS = old }()

	dir := t.TempDir()
	store, err := settings.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: dir, store: store, versions: map[versionKey]string{}}
	a.unpackBundle(context.Background())

	got, err := ytdlp.Resolve("", dir)
	if err != nil || got.Source != ytdlp.SourceManaged {
		t.Fatalf("after unpacking: %+v, %v", got, err)
	}
	if v := a.versionOf(context.Background(), got.Path); v != version {
		t.Errorf("unpacked version %q, want %q", v, version)
	}
	// A second launch finds it in place and leaves it alone.
	st, _ := os.Stat(got.Path)
	a.unpackBundle(context.Background())
	if st2, _ := os.Stat(got.Path); !st2.ModTime().Equal(st.ModTime()) {
		t.Error("unpacked again over the same version")
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "ffmpeg")); err == nil {
		t.Error("ffmpeg was unpacked without being bundled")
	}
}
