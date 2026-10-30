package ytdlp

import (
	"archive/tar"
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
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

func openZip(t *testing.T, path string) *zip.Reader {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { z.Close() })
	return &z.Reader
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
	if err := extractFFmpeg(openZip(t, archive), dest, "windows"); err != nil {
		t.Fatal(err)
	}
	got := dirNames(t, dest)
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
	if err := extractFFmpeg(openZip(t, archive), filepath.Join(t.TempDir(), "ff"), "windows"); err == nil || !strings.Contains(err.Error(), "ffprobe.exe") {
		t.Fatalf("err = %v", err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	return got
}

// writeTarXZ writes a Linux build's archive: each name holds "x <name>".
func writeTarXZ(t *testing.T, names ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ff.tar.xz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	xw, err := xz.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xw)
	for _, n := range names {
		if strings.HasSuffix(n, "/") {
			_ = tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeDir, Mode: 0o755})
			continue
		}
		data := "x " + n
		if err := tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(data))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestRepackLinuxBuildKeepsOnlyWhatItNeeds(t *testing.T) {
	// Layout of the real ffmpeg-master-latest-linux64-gpl.tar.xz, plus hostile names.
	top := "ffmpeg-master-latest-linux64-gpl/"
	archive := writeTarXZ(t,
		top, top+"bin/", top+"bin/ffmpeg", top+"bin/ffplay", top+"bin/ffprobe", top+"LICENSE.txt",
		top+"presets/libvpx-720p.ffpreset", top+"doc/ffmpeg.html", top+"bin/../../escape", "../outside",
		top+"bin/ffmpeg.exe",
	)
	repacked := filepath.Join(t.TempDir(), "ffmpeg.zip")
	if err := repackFFmpeg(archive, repacked, "linux"); err != nil {
		t.Fatal(err)
	}
	var inZip []string
	for _, f := range openZip(t, repacked).File {
		inZip = append(inZip, f.Name)
	}
	if got, want := strings.Join(inZip, " "), top+"bin/ffmpeg "+top+"bin/ffprobe "+top+"LICENSE.txt"; got != want {
		t.Fatalf("repacked %s, want %s", got, want)
	}

	// The repacked zip installs like Windows' one: flat, runnable, nothing else.
	dest := filepath.Join(t.TempDir(), "ffmpeg")
	if err := extractFFmpeg(openZip(t, repacked), dest, "linux"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(dirNames(t, dest), " "); got != "LICENSE.txt ffmpeg ffprobe" {
		t.Fatalf("extracted %s", got)
	}
	data, _ := os.ReadFile(filepath.Join(dest, "ffprobe"))
	if string(data) != "x "+top+"bin/ffprobe" {
		t.Fatalf("ffprobe holds %q", data)
	}
	if st, err := os.Stat(filepath.Join(dest, "ffmpeg")); err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("ffmpeg isn't executable: %v %v", st.Mode(), err)
	}
}

func TestRepackLinuxBuildNeedsBothPrograms(t *testing.T) {
	archive := writeTarXZ(t, "x/bin/ffmpeg", "x/bin/ffplay")
	if err := repackFFmpeg(archive, filepath.Join(t.TempDir(), "ff.zip"), "linux"); err == nil || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("err = %v", err)
	}
}
