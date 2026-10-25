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
