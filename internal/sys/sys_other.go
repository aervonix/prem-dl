//go:build !windows

package sys

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// Quiet puts the child in its own process group so KillTree can signal all of it.
func Quiet(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGrace is how long KillTree waits after SIGTERM before it sends SIGKILL.
var killGrace = 5 * time.Second

// KillTree ends the process group started by Quiet. SIGTERM comes first because yt-dlp is a
// PyInstaller launcher that unpacks itself into /tmp (85 MB, and /tmp is often in RAM) and only
// deletes that copy when it is allowed to exit; SIGKILL left it behind. Whatever still runs
// after killGrace is killed.
func KillTree(pid int) {
	if syscall.Kill(-pid, syscall.SIGTERM) != nil {
		return // the group is gone already
	}
	time.AfterFunc(killGrace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
}

// EndTreeOnCancel makes cancelling a command from exec.CommandContext end its whole process
// group with KillTree. exec's default kills only the launcher, and the yt-dlp under it kept
// running and holding the output pipes, so Wait didn't return until it finished on its own.
func EndTreeOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		KillTree(cmd.Process.Pid)
		return nil
	}
	cmd.WaitDelay = killGrace + 5*time.Second
}

// Contain is a no-op here: the process group from Quiet is enough for KillTree.
func Contain(cmd *exec.Cmd) {}

func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// Open opens a file or folder with its default application.
func Open(path string) error {
	return exec.Command(opener(), path).Start()
}

// Reveal opens the containing folder.
func Reveal(path string) error {
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		path = filepath.Dir(path)
	}
	return exec.Command(opener(), path).Start()
}
