package sys

import (
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestContainPutsTheProcessInTheJob(t *testing.T) {
	cmd := exec.Command("ping", "-n", "30", "127.0.0.1")
	Quiet(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	Contain(cmd)
	if job == 0 {
		t.Fatal("no job object was created")
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(p)
	var in int32
	r, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob").Call(uintptr(p), uintptr(job), uintptr(unsafe.Pointer(&in)))
	if r == 0 || in == 0 {
		t.Fatalf("process not in the job (err %v)", err)
	}
}

func TestWindowPlacementOnScreen(t *testing.T) {
	// The primary monitor always contains (0,0); nothing is 30 000 px to the left.
	if !(WindowPlacement{Left: 50, Top: 50, Right: 900, Bottom: 650}).OnScreen() {
		t.Error("a window on the primary monitor should count as on screen")
	}
	if (WindowPlacement{Left: -30000, Top: 50, Right: -29000, Bottom: 650}).OnScreen() {
		t.Error("a window on a monitor that's gone should not be restored there")
	}
	if (WindowPlacement{Left: 50, Top: 50, Right: 60, Bottom: 60}).OnScreen() {
		t.Error("a window too small to use should not be restored")
	}
}
