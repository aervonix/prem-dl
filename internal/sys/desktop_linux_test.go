//go:build linux

package sys

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchDesktop points the XDG folders at a temporary home, so nothing touches the real one.
func scratchDesktop(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath(xdgMime); err != nil {
		t.Skip("xdg-mime isn't installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(home, "system"))
	t.Setenv("XDG_CURRENT_DESKTOP", "") // xdg-mime's generic path: mimeapps.list in XDG_CONFIG_HOME
	return home
}

func TestExecQuoting(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/Prem-dl/Prem-dl":        "/opt/Prem-dl/Prem-dl",
		"/home/u/My Apps/Prem-dl":     `"/home/u/My Apps/Prem-dl"`,
		`/home/u/50% off/$HOME/a"b\c`: `"/home/u/50%% off/\\$HOME/a\\"b\\\\c"`,
		"--background":                "--background",
		"/home/u/apps/`id`/Prem-dl":   "\"/home/u/apps/\\\\`id\\\\`/Prem-dl\"",
	} {
		if got := execArg(in); got != want {
			t.Errorf("execArg(%q) = %s, want %s", in, got, want)
		}
	}
	if got := CommandLine("/home/u/My Apps/Prem-dl", "--background"); got != `"/home/u/My Apps/Prem-dl" --background` {
		t.Errorf("CommandLine = %s", got)
	}
}

func TestURLSchemeEntry(t *testing.T) {
	home := scratchDesktop(t)
	exe := "/home/u/My Apps/Prem-dl"
	if c := URLSchemeCommand("prem-dl"); c != "" {
		t.Fatalf("registered before registering: %s", c)
	}
	if err := RegisterURLScheme("prem-dl", "Prem-dl", exe); err != nil {
		t.Fatal(err)
	}
	if c := URLSchemeCommand("prem-dl"); c != SchemeCommand(exe) {
		t.Fatalf("command = %q, want %q", c, SchemeCommand(exe))
	}
	keys, err := desktopEntry(filepath.Join(home, "data", "applications", "prem-dl.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if keys["Name"] != "Prem-dl" || keys["Icon"] != "prem-dl" || keys["MimeType"] != "x-scheme-handler/prem-dl;" || keys["StartupWMClass"] != "prem-dl" {
		t.Fatalf("entry = %v", keys)
	}
	mime, _ := os.ReadFile(filepath.Join(home, "config", "mimeapps.list"))
	if !strings.Contains(string(mime), "x-scheme-handler/prem-dl=prem-dl.desktop") {
		t.Fatalf("not the default for prem-dl: links:\n%s", mime)
	}

	// Moved: registering again points at the new place.
	if err := RegisterURLScheme("prem-dl", "Prem-dl", "/opt/Prem-dl"); err != nil {
		t.Fatal(err)
	}
	if c := URLSchemeCommand("prem-dl"); c != "/opt/Prem-dl %u" {
		t.Fatalf("after moving: %s", c)
	}

	// Another program took over the link type: it counts as not registered.
	err = writeDesktopEntry(filepath.Join(home, "data", "applications", "other.desktop"), [][2]string{
		{"Type", "Application"}, {"Name", "Other"}, {"Exec", "/usr/bin/other %u"}, {"MimeType", "x-scheme-handler/prem-dl;"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = runTool("update-desktop-database", filepath.Join(home, "data", "applications")) // as its package would
	if out, err := exec.Command(xdgMime, "default", "other.desktop", "x-scheme-handler/prem-dl").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if c := URLSchemeCommand("prem-dl"); c != "" {
		mime, _ := os.ReadFile(filepath.Join(home, "config", "mimeapps.list"))
		q, _ := exec.Command(xdgMime, "query", "default", "x-scheme-handler/prem-dl").CombinedOutput()
		t.Fatalf("another program's link type counts as ours: %s\n%s\nquery: %s", c, mime, q)
	}
}

// A package's entry (in XDG_DATA_DIRS) counts when the user has none, so an installed app
// doesn't write a second one.
func TestURLSchemeFromPackage(t *testing.T) {
	home := scratchDesktop(t)
	err := writeDesktopEntry(filepath.Join(home, "system", "applications", "prem-dl.desktop"), [][2]string{
		{"Type", "Application"}, {"Name", "Prem-dl"}, {"Exec", "/usr/bin/prem-dl %u"}, {"MimeType", "x-scheme-handler/prem-dl;"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(xdgMime, "default", "prem-dl.desktop", "x-scheme-handler/prem-dl").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if c := URLSchemeCommand("prem-dl"); c != SchemeCommand("/usr/bin/prem-dl") {
		t.Fatalf("command = %q", c)
	}
}

func TestRunAtSignIn(t *testing.T) {
	home := scratchDesktop(t)
	cmd := CommandLine("/home/u/My Apps/Prem-dl", "--background")
	if err := SetRunAtSignIn("Prem-dl", cmd); err != nil {
		t.Fatal(err)
	}
	if got := RunAtSignIn("Prem-dl"); got != cmd {
		t.Fatalf("RunAtSignIn = %q, want %q", got, cmd)
	}
	keys, _ := desktopEntry(filepath.Join(home, "config", "autostart", "prem-dl.desktop"))
	if keys["X-GNOME-Autostart-enabled"] != "true" || keys["Type"] != "Application" {
		t.Fatalf("entry = %v", keys)
	}
	if err := SetRunAtSignIn("Prem-dl", ""); err != nil {
		t.Fatal(err)
	}
	if got := RunAtSignIn("Prem-dl"); got != "" {
		t.Fatalf("still starts at sign-in: %s", got)
	}
	if err := SetRunAtSignIn("Prem-dl", ""); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

func TestInstallIcon(t *testing.T) {
	home := scratchDesktop(t)
	src := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := range src.Pix {
		src.Pix[i] = 0xff
	}
	var b bytes.Buffer
	_ = png.Encode(&b, src)
	if err := InstallIcon("prem-dl", b.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, size := range iconSizesOnDisk {
		path := filepath.Join(home, "data", "icons", "hicolor", fmt.Sprintf("%dx%d", size, size), "apps", "prem-dl.png")
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil || img.Bounds().Dx() != size {
			t.Fatalf("%s: %v %v", path, img.Bounds(), err)
		}
		if _, _, _, a := img.At(size/2, size/2).RGBA(); a != 0xffff {
			t.Fatalf("%s isn't opaque", path)
		}
	}
	if err := InstallIcon("prem-dl", []byte("not a png")); err == nil {
		t.Fatal("a broken icon was accepted")
	}
}

func TestAppPathPrefersTheAppImage(t *testing.T) {
	img := filepath.Join(t.TempDir(), "Prem-dl.AppImage")
	if err := os.WriteFile(img, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPIMAGE", img)
	if p, _ := AppPath(); p != img {
		t.Fatalf("AppPath = %s", p)
	}
	t.Setenv("APPIMAGE", filepath.Join(t.TempDir(), "gone.AppImage"))
	if p, _ := AppPath(); p == "" || strings.HasSuffix(p, "gone.AppImage") {
		t.Fatalf("AppPath = %s", p)
	}
}
