//go:build windows

package sys

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Per-user registry entries. Nothing here needs administrator rights. The key paths are variables
// so tests can point them at a scratch key.
var (
	classesKey = `Software\Classes`
	runKey     = `Software\Microsoft\Windows\CurrentVersion\Run`
	// Where the per-user installer registers its uninstaller (wails_tools.nsh).
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
)

// InstalledDir returns exe's folder if the installer registered as name put it there, else "":
// the portable exe, a dev build, or a copy next to an older install elsewhere. The installer's
// uninstall entry points at the uninstall.exe it wrote next to exe.
func InstalledDir(name, exe string) string {
	u := strings.Trim(getString(uninstallKey+`\`+name, "UninstallString"), `"`)
	if u == "" || !strings.EqualFold(filepath.Base(u), "uninstall.exe") {
		return ""
	}
	dir := filepath.Dir(exe)
	if !strings.EqualFold(filepath.Clean(filepath.Dir(u)), filepath.Clean(dir)) {
		return ""
	}
	if _, err := os.Stat(u); err != nil {
		return ""
	}
	return dir
}

// RegisterURLScheme makes links like scheme://… start exe with the link as its only argument.
// Registering again, after the exe has moved, just points the scheme at the new path.
func RegisterURLScheme(scheme, name, exe string) error {
	root := classesKey + `\` + scheme
	if err := setString(root, "", "URL:"+name); err != nil {
		return err
	}
	if err := setString(root, "URL Protocol", ""); err != nil {
		return err
	}
	if err := setString(root+`\DefaultIcon`, "", `"`+exe+`",0`); err != nil {
		return err
	}
	return setString(root+`\shell\open\command`, "", `"`+exe+`" "%1"`)
}

// URLSchemeCommand is the command line a scheme is registered to run, or "".
func URLSchemeCommand(scheme string) string {
	return getString(classesKey+`\`+scheme+`\shell\open\command`, "")
}

// SetRunAtSignIn adds the entry that runs command when the user signs in to Windows, or removes it
// when command is empty.
func SetRunAtSignIn(name, command string) error {
	if command != "" {
		return setString(runKey, name, command)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// RunAtSignIn is the command the sign-in entry runs, or "" when there is none.
func RunAtSignIn(name string) string { return getString(runKey, name) }

// SetToastName sets the name Windows shows on notifications from the app registered as id.
// Without it Windows shows id itself (Wails uses the exe's file name). Wails registers the rest of
// the entry and keeps a display name that is already there.
func SetToastName(id, name string) error {
	return setString(classesKey+`\AppUserModelId\`+id, "DisplayName", name)
}

func setString(path, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func getString(path, name string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}
