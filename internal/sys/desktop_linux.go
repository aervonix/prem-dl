//go:build linux

package sys

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Desktop entries take the place of the Windows registry: one in applications/ for the
// prem-dl: link type (it also gives the window its name and icon in the dock), one in
// autostart/ for starting at sign-in. Both live in the user's own folders, so no root is needed.

// AppPath is the program to put in desktop entries: the AppImage file when running from one,
// since what runs is a copy mounted somewhere new on every start.
func AppPath() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" && isRegular(p) {
		return p, nil
	}
	return os.Executable()
}

func isRegular(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// xdgMime is the tool that sets which program opens a link type.
var xdgMime = "xdg-mime"

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func dataDirs() []string {
	var dirs []string
	for _, d := range strings.Split(os.Getenv("XDG_DATA_DIRS"), ":") {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		dirs = []string{"/usr/local/share", "/usr/share"}
	}
	return dirs
}

func configHome() string {
	d, _ := os.UserConfigDir()
	return d
}

// execArg quotes one argument for a desktop entry's Exec key: reserved characters are escaped
// inside double quotes, % is doubled, and the backslashes that leaves are escaped again because
// Exec is a string value.
func execArg(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_=./+,:@") == "" {
		return s
	}
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(s)
	q = strings.ReplaceAll(`"`+q+`"`, "%", "%%")
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(q)
}

// CommandLine is the command that starts exe with args, as a desktop entry's Exec holds it.
func CommandLine(exe string, args ...string) string {
	parts := []string{execArg(exe)}
	for _, a := range args {
		parts = append(parts, execArg(a))
	}
	return strings.Join(parts, " ")
}

// SchemeCommand is the command RegisterURLScheme points a link type at.
func SchemeCommand(exe string) string { return execArg(exe) + " %u" }

// desktopEntry reads the keys of a desktop entry's [Desktop Entry] group, values unescaped only
// as far as the file stores them.
func desktopEntry(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
	inGroup := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inGroup = line == "[Desktop Entry]"
			continue
		}
		if k, v, ok := strings.Cut(line, "="); inGroup && ok {
			keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return keys, nil
}

// writeDesktopEntry writes the [Desktop Entry] group, replacing the file in one step.
func writeDesktopEntry(path string, keys [][2]string) error {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	for _, kv := range keys {
		b.WriteString(kv[0] + "=" + kv[1] + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(b.String()), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

func runTool(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// RegisterURLScheme makes exe open scheme: links, with a desktop entry named after the scheme
// that also names the app (name) and its icon (InstallIcon) to the desktop.
func RegisterURLScheme(scheme, name, exe string) error {
	file := scheme + ".desktop"
	dir := filepath.Join(dataHome(), "applications")
	err := writeDesktopEntry(filepath.Join(dir, file), [][2]string{
		{"Type", "Application"},
		{"Name", name},
		{"Comment", "Download video and audio"},
		{"Exec", SchemeCommand(exe)},
		{"Icon", scheme},
		{"Terminal", "false"},
		{"Categories", "AudioVideo;Video;"},
		{"MimeType", "x-scheme-handler/" + scheme + ";"},
		{"StartupWMClass", scheme},
	})
	if err != nil {
		return err
	}
	_, _ = runTool("update-desktop-database", dir) // only a cache; xdg-mime below is what counts
	// xdg-mime writes mimeapps.list there, and quietly does nothing when the folder is missing.
	if err := os.MkdirAll(configHome(), 0o700); err != nil {
		return err
	}
	if out, err := runTool(xdgMime, "default", file, "x-scheme-handler/"+scheme); err != nil {
		return fmt.Errorf("xdg-mime: %v %s", err, out)
	}
	if def, _ := runTool(xdgMime, "query", "default", "x-scheme-handler/"+scheme); def != file {
		return fmt.Errorf("xdg-mime didn't make %s the default for %s: links (it is %q)", file, scheme, def)
	}
	return nil
}

// URLSchemeCommand is the command that opens scheme: links, if it is the desktop entry
// RegisterURLScheme writes (the user's own, or one a package installed) and it is the default
// for the link type. Otherwise it is "".
func URLSchemeCommand(scheme string) string {
	file := scheme + ".desktop"
	if def, err := runTool(xdgMime, "query", "default", "x-scheme-handler/"+scheme); err != nil || def != file {
		return ""
	}
	for _, dir := range append([]string{dataHome()}, dataDirs()...) {
		keys, err := desktopEntry(filepath.Join(dir, "applications", file))
		if err != nil {
			continue // the first one found is the one the desktop uses
		}
		if !strings.Contains(keys["MimeType"], "x-scheme-handler/"+scheme+";") {
			return ""
		}
		return keys["Exec"]
	}
	return ""
}

func autostartPath(name string) string {
	return filepath.Join(configHome(), "autostart", strings.ToLower(name)+".desktop")
}

// SetRunAtSignIn starts command (from CommandLine) when the user signs in, or stops doing so
// when command is "".
func SetRunAtSignIn(name, command string) error {
	path := autostartPath(name)
	if command == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeDesktopEntry(path, [][2]string{
		{"Type", "Application"},
		{"Name", name},
		{"Exec", command},
		{"Icon", strings.ToLower(name)},
		{"Terminal", "false"},
		{"X-GNOME-Autostart-enabled", "true"},
	})
}

// RunAtSignIn is the command started at sign-in, or "".
func RunAtSignIn(name string) string {
	keys, err := desktopEntry(autostartPath(name))
	if err != nil {
		return ""
	}
	return keys["Exec"]
}

// iconSizesOnDisk are the sizes InstallIcon writes into the hicolor theme.
var iconSizesOnDisk = []int{48, 128, 256}

// InstallIcon puts the app's icon (a PNG) into the user's icon theme as name, for the desktop
// entries, the dock and notifications. Files already in place are left alone.
func InstallIcon(name string, pngData []byte) error {
	img, _, err := image.Decode(bytes.NewReader(pngData))
	if err != nil {
		return err
	}
	for _, size := range iconSizesOnDisk {
		var b bytes.Buffer
		if err := png.Encode(&b, argbImage(scaleARGB(img, size), size)); err != nil {
			return err
		}
		path := filepath.Join(dataHome(), "icons", "hicolor", fmt.Sprintf("%dx%d", size, size), "apps", name+".png")
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b.Bytes()) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(path, b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// SystemIcon reports whether a package installed the icon name into a system icon theme.
func SystemIcon(name string) bool {
	for _, dir := range dataDirs() {
		for _, size := range iconSizesOnDisk {
			if isRegular(filepath.Join(dir, "icons", "hicolor", fmt.Sprintf("%dx%d", size, size), "apps", name+".png")) {
				return true
			}
		}
	}
	return false
}

// argbImage turns scaleARGB's pixels back into an image.
func argbImage(argb []byte, size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i+3 < len(argb); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = argb[i+1], argb[i+2], argb[i+3], argb[i]
	}
	return img
}

// SetToastName does nothing here: notifications name the app themselves.
func SetToastName(id, name string) error { return nil }

// InstalledDir and StartCommandLine belong to Prem-dl's own updates, which Linux doesn't have yet.
func InstalledDir(name, exe string) string { return "" }

func StartCommandLine(exe, cmdline string) error { return errors.New("not supported") }
