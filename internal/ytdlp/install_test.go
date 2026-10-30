package ytdlp

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildZip makes a zip in memory from name -> contents; names ending in "/" are folders.
func buildZip(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func zipReader(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	r := buildZip(t, files)
	z, err := zip.NewReader(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func TestExtractYtdlpRenamesTheLauncher(t *testing.T) {
	// Layout of the real yt-dlp_linux.zip (yt-dlp_win.zip has no folder entries).
	dest := filepath.Join(t.TempDir(), "ytdlp")
	err := extractYtdlp(zipReader(t, map[string]string{
		"yt-dlp_linux": "launcher", "_internal/": "", "_internal/base_library.zip": "lib",
		"_internal/Cryptodome/Cipher/_ARC4.abi3.so": "so",
	}), dest)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		Name(): "launcher", "_internal/base_library.zip": "lib", "_internal/Cryptodome/Cipher/_ARC4.abi3.so": "so",
	} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(path)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", path, got, err)
		}
	}
}

func TestExtractYtdlpRefusesOddArchives(t *testing.T) {
	cases := map[string]map[string]string{
		"escapes dest":       {"yt-dlp.exe": "x", "_internal/../../evil.dll": "x"},
		"absolute path":      {"yt-dlp.exe": "x", "/evil.dll": "x"},
		"two programs":       {"yt-dlp.exe": "x", "other.exe": "x"},
		"no program":         {"_internal/base_library.zip": "x"},
		"parent of the root": {"../yt-dlp.exe": "x"},
	}
	for name, files := range cases {
		root := t.TempDir()
		if err := extractYtdlp(zipReader(t, files), filepath.Join(root, "a", "ytdlp")); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := os.Stat(filepath.Join(root, "evil.dll")); err == nil {
			t.Errorf("%s: wrote outside dest", name)
		}
	}
}

func TestInstallSwapsInAWorkingCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake launcher is a shell script")
	}
	launcher := func(version string) map[string]string {
		return map[string]string{"yt-dlp_linux": "#!/bin/sh\necho " + version + "\n", "_internal/lib": version}
	}
	managed := t.TempDir()
	install := func(files map[string]string) (string, error) {
		r := buildZip(t, files)
		return Install(context.Background(), r, r.Size(), managed)
	}
	if v, err := install(launcher("2026.10.21")); err != nil || v != "2026.10.21" {
		t.Fatalf("first install: %q, %v", v, err)
	}
	if v, err := install(launcher("2026.10.26")); err != nil || v != "2026.10.26" {
		t.Fatalf("second install: %q, %v", v, err)
	}
	// One that doesn't run leaves the working copy in place.
	if _, err := install(map[string]string{"yt-dlp_linux": "#!/bin/sh\nexit 3\n"}); err == nil {
		t.Fatal("a broken yt-dlp was installed")
	}
	if v, err := Version(context.Background(), ManagedPath(managed)); err != nil || v != "2026.10.26" {
		t.Errorf("after a failed install: %q, %v", v, err)
	}
	if lib, _ := os.ReadFile(filepath.Join(YtdlpDir(managed), "_internal", "lib")); strings.TrimSpace(string(lib)) != "2026.10.26" {
		t.Errorf("_internal holds %q", lib)
	}
	entries, _ := os.ReadDir(filepath.Dir(YtdlpDir(managed)))
	if len(entries) != 1 {
		t.Errorf("stray folders left: %v", entries)
	}
}
