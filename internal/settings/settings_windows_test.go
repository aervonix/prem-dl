package settings

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFileLockedAtStartupIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := Default()
	s.DownloadDir = `D:\Videos`
	saved := writeSettings(t, dir, s)
	// Hold it open with no sharing, the way antivirus or a sync tool briefly does.
	name, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	syscall.CloseHandle(h)
	if err != nil {
		t.Fatal(err)
	}
	// The background update check writes a timestamp a few seconds after launch.
	if _, err := st.Update(func(v *Settings) { v.Theme = "dark" }); err == nil {
		t.Error("saving over a file we couldn't read should be refused")
	}
	if got, _ := os.ReadFile(path); string(got) != string(saved) {
		t.Fatalf("settings.json was overwritten:\n%s", got)
	}
}

func TestSaveToFolderMustBeAFullPath(t *testing.T) {
	// yt-dlp resolves a relative --paths against the app's working directory.
	for dir, ok := range map[string]bool{"": true, `  D:\Videos `: true, "videos": false, `\videos`: false, "   ": true} {
		o := Default().Defaults
		o.OutputDir = dir
		if err := ValidateOptions(&o); (err == nil) != ok {
			t.Errorf("%q: err = %v", dir, err)
		}
	}
	o := Default().Defaults
	o.OutputDir = `  D:\Videos `
	_ = ValidateOptions(&o)
	if o.OutputDir != `D:\Videos` {
		t.Errorf("not trimmed: %q", o.OutputDir)
	}
}
