package jsc

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in, against YouTube with the real yt-dlp (an unpacked build, which carries the solver
// scripts) and node: yt-dlp, given the plugin, solves its challenges here and never starts its
// own solver.
//
//	YTGUI_INTEGRATION=1 YTGUI_FORCE_IPV4=1 YTGUI_YTDLP=$PWD/yt-dlp go test ./internal/jsc -run Integration -v
func TestIntegrationYtdlpUsesTheSolver(t *testing.T) {
	bin := os.Getenv("YTGUI_YTDLP")
	if os.Getenv("YTGUI_INTEGRATION") != "1" || bin == "" {
		t.Skip("set YTGUI_INTEGRATION=1 and YTGUI_YTDLP=<path to an unpacked yt-dlp> to run")
	}
	scripts := ScriptsDir(bin)
	node, err := exec.LookPath("node")
	if scripts == "" || err != nil {
		t.Skipf("needs an unpacked yt-dlp (scripts %q) and node (%v)", scripts, err)
	}
	srv, err := Serve(NewSolver(node, scripts))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	plugins := t.TempDir()
	if err := InstallPlugin(plugins); err != nil {
		t.Fatal(err)
	}
	analyse := func(useSolver bool) (time.Duration, string) {
		args := []string{"-v", "--ignore-config", "--js-runtimes=node:" + node, "-J", "--flat-playlist"}
		if os.Getenv("YTGUI_FORCE_IPV4") == "1" {
			args = append(args, "--force-ipv4")
		}
		cmd := exec.Command(bin, append(args, "--", "https://www.youtube.com/watch?v=dQw4w9WgXcQ")...)
		cmd.Env = os.Environ()
		if useSolver {
			cmd.Args = append(cmd.Args[:1], append([]string{"--plugin-dirs=" + plugins}, cmd.Args[1:]...)...)
			cmd.Env = append(cmd.Env, srv.Env()...)
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		start := time.Now()
		out, err := cmd.Output()
		took := time.Since(start)
		if err != nil || !strings.Contains(string(out), `"formats"`) {
			t.Fatalf("yt-dlp: %v\n%s", err, stderr.String())
		}
		return took, stderr.String()
	}
	builtin, _ := analyse(false)
	for i := 1; i <= 2; i++ {
		took, log := analyse(true)
		if !strings.Contains(log, "premdl-1.0.0 (external)") {
			t.Fatalf("the plugin wasn't loaded:\n%s", log)
		}
		if strings.Contains(log, "Solving JS challenges using node") || strings.Contains(log, "[jsc:node]") {
			t.Errorf("run %d: yt-dlp solved the challenges itself:\n%s", i, log)
		}
		t.Logf("run %d with Prem-dl's solver: %d ms (yt-dlp's own: %d ms)", i, took.Milliseconds(), builtin.Milliseconds())
	}
}
