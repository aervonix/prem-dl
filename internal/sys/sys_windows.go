//go:build windows

package sys

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

// Quiet stops a child console process from flashing a window.
func Quiet(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// KillTree terminates a process and everything it spawned (yt-dlp starts ffmpeg).
func KillTree(pid int) {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	Quiet(kill)
	_ = kill.Run()
}

// Open opens a file or folder with its default application.
func Open(path string) error {
	cmd := exec.Command("explorer.exe", path)
	Quiet(cmd)
	// explorer.exe returns exit code 1 even when it succeeds, so only start it.
	return cmd.Start()
}

// Reveal opens Explorer with the file selected.
func Reveal(path string) error {
	cmd := exec.Command("explorer.exe")
	// Go's argument quoting would mangle "/select,"; build the command line by hand.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    false,
		CmdLine:       `explorer.exe /select,"` + path + `"`,
		CreationFlags: 0,
	}
	return cmd.Start()
}
