package sys

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// scratchKeys points the registry helpers at a throwaway key for the test.
func scratchKeys(t *testing.T) string {
	t.Helper()
	root := `Software\Prem-dl-test-` + strconv.Itoa(os.Getpid())
	oldClasses, oldRun := classesKey, runKey
	classesKey, runKey = root+`\Classes`, root+`\Run`
	t.Cleanup(func() {
		classesKey, runKey = oldClasses, oldRun
		deleteTree(t, root)
	})
	return root
}

func deleteTree(t *testing.T, path string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	names, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, n := range names {
		deleteTree(t, path+`\`+n)
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
		t.Errorf("cleaning up %s: %v", path, err)
	}
}

func TestRegisterURLScheme(t *testing.T) {
	scratchKeys(t)
	exe := `C:\Apps\Prem dl\Prem-dl.exe`
	if err := RegisterURLScheme("prem-dl", "Prem-dl", exe); err != nil {
		t.Fatal(err)
	}
	if got := URLSchemeCommand("prem-dl"); got != `"C:\Apps\Prem dl\Prem-dl.exe" "%1"` {
		t.Fatalf("command = %q", got)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, classesKey+`\prem-dl`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if _, _, err := k.GetStringValue("URL Protocol"); err != nil {
		t.Error(`Windows only treats it as a link type with a "URL Protocol" value`)
	}
	// Moving the exe and registering again points the link type at the new place.
	if err := RegisterURLScheme("prem-dl", "Prem-dl", `D:\Prem-dl.exe`); err != nil {
		t.Fatal(err)
	}
	if got := URLSchemeCommand("prem-dl"); got != `"D:\Prem-dl.exe" "%1"` {
		t.Fatalf("after moving, command = %q", got)
	}
}

func TestRunAtSignIn(t *testing.T) {
	scratchKeys(t)
	if got := RunAtSignIn("Prem-dl"); got != "" {
		t.Fatalf("before: %q", got)
	}
	// Removing an entry that isn't there, under a key that doesn't exist, is fine.
	if err := SetRunAtSignIn("Prem-dl", ""); err != nil {
		t.Fatal(err)
	}
	cmd := `"C:\Apps\Prem-dl.exe" --background`
	if err := SetRunAtSignIn("Prem-dl", cmd); err != nil {
		t.Fatal(err)
	}
	if got := RunAtSignIn("Prem-dl"); got != cmd {
		t.Fatalf("after adding: %q", got)
	}
	if err := SetRunAtSignIn("Prem-dl", ""); err != nil {
		t.Fatal(err)
	}
	if got := RunAtSignIn("Prem-dl"); got != "" {
		t.Fatalf("after removing: %q", got)
	}
}

func TestTaskbarListCanBeCreated(t *testing.T) {
	// Checks the class and interface IDs and the HrInit slot; a wrong one fails here, where on
	// the shell thread it would only leave the taskbar button without progress.
	done := make(chan error)
	go func() {
		runtime.LockOSThread()
		procCoInitializeEx.Call(0, coinitApartmentThreaded)
		defer procCoUninitialize.Call()
		tb, err := newTaskbarList()
		if err == nil {
			tb.release()
		}
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestShellStartsAndStops(t *testing.T) {
	// No tray icon is shown (that waits for SetTray) and there's no main window to show progress
	// on; this checks the thread starts, takes requests and stops, and can start again.
	for i := 0; i < 2; i++ {
		s, err := StartShell(ShellOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := StartShell(ShellOptions{}); err == nil {
			t.Fatal("a second shell started while one runs")
		}
		s.SetProgress(ProgressNormal, 0.5)
		s.Refresh()
		s.Close()
		select {
		case <-s.done:
		default:
			t.Fatal("the shell thread didn't stop")
		}
	}
}

func TestInstalledDir(t *testing.T) {
	root := scratchKeys(t)
	old := uninstallKey
	uninstallKey = root + `\Uninstall`
	t.Cleanup(func() { uninstallKey = old })

	dir := t.TempDir()
	exe := filepath.Join(dir, "Prem-dl.exe")
	if got := InstalledDir("Prem-dl", exe); got != "" {
		t.Fatalf("no uninstall entry: got %q", got)
	}
	uninst := filepath.Join(dir, "uninstall.exe")
	if err := setString(uninstallKey+`\Prem-dl`, "UninstallString", `"`+uninst+`"`); err != nil {
		t.Fatal(err)
	}
	if got := InstalledDir("Prem-dl", exe); got != "" {
		t.Fatalf("uninstall.exe missing: got %q", got)
	}
	if err := os.WriteFile(uninst, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := InstalledDir("Prem-dl", exe); got != dir {
		t.Fatalf("installed copy: got %q, want %q", got, dir)
	}
	// Folder names compare like Windows does.
	if got := InstalledDir("Prem-dl", strings.ToUpper(exe)); got == "" {
		t.Error("a differently cased path is the same folder")
	}
	// A portable copy elsewhere isn't the installed one.
	if got := InstalledDir("Prem-dl", filepath.Join(t.TempDir(), "Prem-dl.exe")); got != "" {
		t.Errorf("portable copy: got %q", got)
	}
}
