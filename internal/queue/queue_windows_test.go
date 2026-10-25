package queue

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

func TestQueueFileLockedAtStartupIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "queue.json")
	const saved = `{"version":1,"jobs":[{"id":"keep","seq":1,"url":"https://fake.test/ok","status":"completed"}]}`
	if err := os.WriteFile(state, []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold it open with no sharing, the way antivirus or a sync tool briefly does.
	name, _ := syscall.UTF16PtrFromString(state)
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_YTDLP", "1")
	s := settings.Default()
	s.DownloadDir = dir
	m := New(Deps{
		Settings: func() settings.Settings { return s },
		Env: func(context.Context) (ytdlp.Env, func(), error) {
			return ytdlp.Env{Bin: os.Args[0], Settings: s}, func() {}, nil
		},
		StatePath: state,
	})
	syscall.CloseHandle(h) // the lock is gone; the queue we started with is empty
	m.Start()
	if _, _, err := m.Add([]NewJob{{URL: "https://fake.test/fail403"}}, s.Defaults); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if got, _ := os.ReadFile(state); string(got) != saved {
		t.Fatalf("the saved queue was overwritten:\n%s", got)
	}
}
