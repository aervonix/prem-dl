package ytdlp

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeZip(t *testing.T, names ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ff.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("x " + n))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestExtractFFmpegTakesOnlyWhatItNeeds(t *testing.T) {
	// Layout of the real ffmpeg-master-latest-win64-gpl-shared.zip, plus hostile names.
	top := "ffmpeg-master-latest-win64-gpl-shared/"
	archive := writeZip(t,
		top+"LICENSE.txt", top+"bin/ffmpeg.exe", top+"bin/ffprobe.exe", top+"bin/ffplay.exe",
		top+"bin/avcodec-63.dll", top+"bin/swscale-10.dll", top+"doc/ffmpeg.html", top+"include/libavcodec/avcodec.h",
		top+"bin/../../escape.dll", "../outside.dll", top+"bin/sub/deep.dll",
	)
	dest := filepath.Join(t.TempDir(), "ffmpeg")
	if err := extractFFmpeg(archive, dest); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dest)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	want := "LICENSE.txt avcodec-63.dll ffmpeg.exe ffprobe.exe swscale-10.dll"
	if strings.Join(got, " ") != want {
		t.Fatalf("extracted %v, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "outside.dll")); err == nil {
		t.Fatal("an entry escaped the destination")
	}
}

func TestExtractFFmpegNeedsBothPrograms(t *testing.T) {
	archive := writeZip(t, "x/bin/ffmpeg.exe", "x/bin/avutil-61.dll")
	if err := extractFFmpeg(archive, filepath.Join(t.TempDir(), "ff")); err == nil || !strings.Contains(err.Error(), "ffprobe.exe") {
		t.Fatalf("err = %v", err)
	}
}
