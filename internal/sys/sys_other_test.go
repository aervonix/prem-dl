//go:build !windows

package sys

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func start(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	Quiet(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the shell set its traps
}

// waitWithin waits for cmd and fails if that takes longer than d.
func waitWithin(t *testing.T, cmd *exec.Cmd, d time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatalf("still running after %v", d)
		return nil
	}
}

// yt-dlp's launcher cleans up its unpacked copy on SIGTERM, so that comes first.
func TestKillTreeLetsTheGroupExitCleanly(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleaned-up")
	cmd := exec.Command("sh", "-c", `trap 'touch "$1"; exit 0' TERM; while :; do sleep 0.1; done`, "sh", marker)
	start(t, cmd)
	KillTree(cmd.Process.Pid)
	_ = waitWithin(t, cmd, 3*time.Second)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the process wasn't asked to exit before being killed")
	}
}

func TestKillTreeKillsWhatIgnoresTerm(t *testing.T) {
	old := killGrace
	killGrace = 300 * time.Millisecond
	defer func() { killGrace = old }()
	cmd := exec.Command("sh", "-c", `trap '' TERM; sleep 30`)
	start(t, cmd)
	KillTree(cmd.Process.Pid)
	err := waitWithin(t, cmd, 3*time.Second)
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("ended with %v", err)
	}
}

// Cancelling ends the child the shell started too, which holds the output pipe; exec's default
// killed only the shell, and Wait sat on the pipe until the child finished.
func TestEndTreeOnCancelEndsTheChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", `sleep 30 & wait`)
	EndTreeOnCancel(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	start(t, cmd)
	cancel()
	if err := waitWithin(t, cmd, 3*time.Second); err == nil {
		t.Fatal("a cancelled command reported success")
	}
}
