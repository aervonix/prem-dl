package settings

import (
	"path/filepath"
	"testing"
)

func TestXDGDownloadDir(t *testing.T) {
	home := filepath.FromSlash("/home/u")
	for _, c := range []struct{ file, want string }{
		{"# written by xdg-user-dirs-update\nXDG_DESKTOP_DIR=\"$HOME/Desktop\"\nXDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n", filepath.Join(home, "Downloads")},
		{`XDG_DOWNLOAD_DIR="$HOME/Téléchargements"`, filepath.Join(home, "Téléchargements")},
		{`XDG_DOWNLOAD_DIR="/mnt/data/dl"`, "/mnt/data/dl"},
		{`XDG_DOWNLOAD_DIR="$HOME/"`, ""}, // the home folder: the Downloads folder was removed
		{`XDG_DOWNLOAD_DIR=$HOME/Downloads`, ""},
		{`XDG_DOWNLOAD_DIR="relative/dl"`, ""},
		{`XDG_MUSIC_DIR="$HOME/Music"`, ""},
		{"", ""},
	} {
		if got := xdgDownloadDir(c.file, home); got != c.want {
			t.Errorf("%q: %q, want %q", c.file, got, c.want)
		}
	}
}

func TestDownloadsFolderIsAbsolute(t *testing.T) {
	if dir := DownloadsFolder(); dir == "" || !filepath.IsAbs(dir) {
		t.Errorf("DownloadsFolder() = %q", dir)
	}
}
