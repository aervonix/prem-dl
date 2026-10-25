//go:build windows

package sys

import (
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
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

var (
	jobOnce sync.Once
	job     windows.Handle // never closed: Windows closes it when the app exits, however it exits
)

// Contain ties a started process (and whatever it spawns later) to the app's lifetime, so
// yt-dlp and ffmpeg can't keep running after a crash or an End task. It is best effort.
func Contain(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			_ = windows.CloseHandle(h)
			return
		}
		job = h
	})
	if job == 0 {
		return
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(p)
	_ = windows.AssignProcessToJobObject(job, p)
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
