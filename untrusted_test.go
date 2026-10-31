package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestToastTextCantEndItsCDATA(t *testing.T) {
	title := `Funny cat ]]></text><image placement="hero" src="file:///\\host\s\x.png"/><text><![CDATA[` + "\x00\x1b"
	got := toastText(title)
	if strings.Contains(got, "]]>") || strings.ContainsAny(got, "\x00\x1b") {
		t.Errorf("toastText(%q) = %q", title, got)
	}
	if plain := "Rick Astley – Never Gonna Give You Up\nline 2"; toastText(plain) != plain {
		t.Errorf("changed a plain title: %q", toastText(plain))
	}
	if got := toastText("a]]]>b"); strings.Contains(got, "]]>") {
		t.Errorf("toastText = %q", got)
	}
}

func TestOpenableRefusesStrayNetworkPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		if !openable("/home/u/Videos/a.mp4", "/home/u/Videos") || openable("rel/a.mp4") {
			t.Error("local paths")
		}
		if openable("//evil/share/x.mp4", "/home/u/Videos") {
			t.Error("opened a network path outside the download folder")
		}
		return
	}
	if !openable(`C:\Users\u\Videos\a.mp4`, `D:\dl`) {
		t.Error("a local file elsewhere (a template with a folder) is fine")
	}
	if openable(`\\evil\share\x.mp4`, `C:\dl`) || openable(`\\?\UNC\evil\s\x.mp4`, `C:\dl`) {
		t.Error("opened a network path outside the download folder")
	}
	if !openable(`\\nas\media\a.mp4`, "", `\\nas\media`) {
		t.Error("a NAS download folder")
	}
	if openable(`\\nas\media\other\a.mp4`, `\\nas\media\dl`) {
		t.Error("escaped the folder")
	}
}

func TestRemoveStaleTemp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	old := filepath.Join(os.TempDir(), "ytgui-cookies-1.txt")
	fresh := filepath.Join(os.TempDir(), "ytgui-info-2.json")
	other := filepath.Join(os.TempDir(), "unrelated.txt")
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Now().Add(-24 * time.Hour)
	_ = os.Chtimes(old, day, day)
	_ = os.Chtimes(other, day, day)
	removeStaleTemp()
	if _, err := os.Stat(old); err == nil {
		t.Error("an old cookie copy stayed")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed", p)
		}
	}
}
