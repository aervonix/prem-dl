//go:build !windows

package sys

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

// Quiet puts the child in its own process group so KillTree can signal all of it.
func Quiet(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillTree terminates the process group started by Quiet.
func KillTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
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
