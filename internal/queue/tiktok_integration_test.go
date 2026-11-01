package queue

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/tiktok"
	"yt-dlp-gui/internal/ytdlp"
)

// TikTok posts download from internal/tiktok's analysis (--load-info-json), as the app does
// (savedInfo): a video with the default options, and a photo post as its pictures and sound,
// or only its sound with the audio preset. Posts get deleted; swap in others of the same kind.
func TestIntegrationTikTokDownloads(t *testing.T) {
	s, envFn := integrationEnv(t)
	infos := ytdlp.NewInfoCache()
	info := func(link string) (ytdlp.Saved, func()) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		post, err := tiktok.Read(ctx, tiktok.Options{IPv4: s.ForceIPv4}, link)
		if err != nil {
			t.Errorf("reading %s: %v", link, err)
			return ytdlp.Saved{}, func() {}
		}
		data, err := post.Info()
		if err != nil {
			t.Error(err)
			return ytdlp.Saved{}, func() {}
		}
		res, err := ytdlp.FromInfo(data)
		if err != nil {
			t.Error(err)
			return ytdlp.Saved{}, func() {}
		}
		infos.Put(link, res)
		return infos.Take(link)
	}
	m := New(Deps{Settings: func() settings.Settings { return s }, Env: envFn, Info: info, StatePath: filepath.Join(s.DownloadDir, "q.json")})
	m.Start()
	defer m.Shutdown()

	run := func(t *testing.T, link string, opts settings.DownloadOptions) Job {
		t.Helper()
		opts.OutputDir = t.TempDir()
		jobs, _, err := m.Add([]NewJob{{URL: link}}, opts)
		if err != nil {
			t.Fatal(err)
		}
		id := jobs[0].ID
		var top float64
		deadline := time.Now().Add(integTimout)
		for time.Now().Before(deadline) {
			j, _ := m.Get(id)
			if j.Status == StatusDownloading && j.Percent >= 0 {
				if j.Percent < top-0.5 {
					t.Errorf("progress went back: %.1f -> %.1f (%s)", top, j.Percent, j.Stage)
				}
				top = max(top, j.Percent)
			}
			if j.Status.finished() {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		j, _ := m.Get(id)
		log := strings.Join(m.Log(id), "\n  ")
		if j.Status != StatusCompleted {
			t.Fatalf("status %s, error %+v\nlog:\n  %s", j.Status, j.Error, log)
		}
		if strings.Contains(log, "Downloading webpage") {
			t.Errorf("yt-dlp read the page itself:\n  %s", log)
		}
		return j
	}
	files := func(dir string) []string {
		var out []string
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(dir, p)
				out = append(out, rel)
			}
			return nil
		})
		sort.Strings(out)
		return out
	}

	t.Run("video", func(t *testing.T) {
		j := run(t, "https://www.tiktok.com/@xvbray/video/7702605479670828318", s.Defaults)
		st, err := os.Stat(j.OutputFile)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s, %d bytes", filepath.Base(j.OutputFile), st.Size())
	})

	const photo = "https://www.tiktok.com/@nasa/photo/7701866987609197837"
	t.Run("photo post", func(t *testing.T) {
		j := run(t, photo, s.Defaults)
		got := files(j.Options.OutputDir)
		t.Logf("files: %v", got)
		var pictures, sound int
		for _, f := range got {
			switch base := filepath.Base(f); {
			case strings.HasSuffix(base, ".jpg"):
				pictures++
				data, _ := os.ReadFile(filepath.Join(j.Options.OutputDir, f))
				if !bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
					t.Errorf("%s isn't a JPEG", f)
				}
			case strings.HasPrefix(base, "Sound."):
				sound++
			}
			if filepath.Dir(f) == "." {
				t.Errorf("%s isn't in the post's folder", f)
			}
		}
		if pictures != 5 || sound != 1 {
			t.Errorf("%d pictures and %d sounds, want 5 and 1", pictures, sound)
		}
	})

	t.Run("photo post as audio", func(t *testing.T) {
		opts := s.Defaults
		opts.Preset, opts.AudioFormat = "audio", "mp3"
		j := run(t, photo, opts)
		got := files(j.Options.OutputDir)
		t.Logf("files: %v", got)
		if len(got) != 1 || !strings.HasSuffix(got[0], ".mp3") {
			t.Errorf("want one .mp3, got %v", got)
		}
	})
}
