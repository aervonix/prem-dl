// Package jsc solves YouTube's JavaScript challenges for yt-dlp with a solver Prem-dl keeps
// running. yt-dlp solves them (the n and sig parameters of every stream link) by starting a
// JavaScript runtime that parses YouTube's 3 MB player script from scratch, 0.66 s on every
// analysis and every download that reads the page. Kept running, the player is parsed once per
// player version (YouTube changes it about weekly; 0.5 s) and each analysis after that takes
// 0.04 s or less.
//
// The solver is yt-dlp's own: the lib and core scripts of the bundled yt-dlp (yt-dlp-ejs), so
// it changes with yt-dlp. yt-dlp reaches it through a plugin (plugin/premdl_jsc.py, a
// JsChallengeProvider, yt-dlp's public API for this), which asks Server; whenever that fails,
// yt-dlp solves the challenges itself as before.
package jsc

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"yt-dlp-gui/internal/sys"
)

//go:embed worker.js
var workerJS string

// ScriptsDir is where an unpacked ("onedir") yt-dlp keeps its challenge solver scripts, given
// the path of its launcher; "" when it has none (a single-file or pip yt-dlp).
func ScriptsDir(ytdlpBin string) string {
	real, err := filepath.EvalSymlinks(ytdlpBin)
	if err != nil {
		return ""
	}
	dir := filepath.Join(filepath.Dir(real), "_internal", "yt_dlp_ejs", "yt", "solver")
	for _, f := range []string{"lib.min.js", "core.min.js"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.IsDir() {
			return ""
		}
	}
	return dir
}

// Request is one of yt-dlp's batches: challenges of one player script.
type Request struct {
	PlayerURL string `json:"player_url"`
	// Player is the player script, sent only once the solver has asked for it (NeedPlayer).
	Player   string          `json:"player,omitempty"`
	Requests json.RawMessage `json:"requests"` // [{"type": "n"|"sig", "challenges": [...]}]
}

// Solver runs the challenge solver in a JavaScript runtime, started on first use and again if
// it stops. One request is solved at a time; each takes milliseconds once its player is known.
type Solver struct {
	node    string // the runtime: node (the worker uses node's stdin)
	scripts string // ScriptsDir

	mu   sync.Mutex
	proc *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	done chan struct{} // closed when proc exits
}

// NewSolver returns a solver running scripts (ScriptsDir) in node.
func NewSolver(node, scripts string) *Solver {
	return &Solver{node: node, scripts: scripts}
}

// timeout bounds one answer from the worker; parsing a new player takes about 0.5 s.
var timeout = 20 * time.Second

// Solve answers a request with the solver's JSON: a result in yt-dlp's solver format,
// need_player, or an error.
func (s *Solver) Solve(ctx context.Context, req Request) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(ctx); err != nil {
		return nil, err
	}
	msg, err := json.Marshal(struct {
		Op string `json:"op"`
		Request
	}{"solve", req})
	if err != nil {
		return nil, err
	}
	out, err := s.askLocked(ctx, msg)
	if err != nil {
		s.stopLocked() // a worker that stalls or breaks is started afresh next time
	}
	return out, err
}

// startLocked starts the worker if it isn't running and gives it the solver scripts.
func (s *Solver) startLocked(ctx context.Context) error {
	if s.proc != nil {
		select {
		case <-s.done:
			s.stopLocked()
		default:
			return nil
		}
	}
	lib, err := os.ReadFile(filepath.Join(s.scripts, "lib.min.js"))
	if err != nil {
		return err
	}
	core, err := os.ReadFile(filepath.Join(s.scripts, "core.min.js"))
	if err != nil {
		return err
	}
	args, err := permissionArgs(ctx, s.node)
	if err != nil {
		return err
	}
	// Like yt-dlp's own runs: no file system, network or child processes for the player's code.
	cmd := exec.Command(s.node, append(args, "-e", workerJS)...)
	sys.Quiet(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", filepath.Base(s.node), err)
	}
	s.proc, s.in, s.out, s.done = cmd, in, bufio.NewReaderSize(stdout, 1<<16), make(chan struct{})
	go func(done chan struct{}) {
		_ = cmd.Wait()
		close(done)
	}(s.done)

	init, _ := json.Marshal(map[string]string{"op": "init", "lib": string(lib), "core": string(core)})
	out, err := s.askLocked(ctx, init)
	if err == nil {
		var r struct{ Type, Error string }
		if json.Unmarshal(out, &r) != nil || r.Type != "ready" {
			err = fmt.Errorf("the solver didn't start: %s", r.Error)
		}
	}
	if err != nil {
		s.stopLocked()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
	}
	return err
}

// askLocked sends one message and reads the answer.
func (s *Solver) askLocked(ctx context.Context, msg []byte) (json.RawMessage, error) {
	type answer struct {
		line []byte
		err  error
	}
	got := make(chan answer, 1)
	go func(in io.Writer, out *bufio.Reader) {
		if _, err := in.Write(append(msg, '\n')); err != nil {
			got <- answer{err: err}
			return
		}
		line, err := out.ReadBytes('\n')
		got <- answer{line, err}
	}(s.in, s.out)
	select {
	case a := <-got:
		if a.err != nil {
			return nil, fmt.Errorf("the solver stopped: %w", a.err)
		}
		return json.RawMessage(a.line), nil
	case <-s.done:
		return nil, errors.New("the solver stopped")
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, errors.New("the solver took too long")
	}
}

func (s *Solver) stopLocked() {
	if s.proc == nil {
		return
	}
	_ = s.in.Close()
	_ = s.proc.Process.Kill()
	<-s.done
	s.proc, s.in, s.out, s.done = nil, nil, nil, nil
}

// Close stops the worker.
func (s *Solver) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// permissionArgs is node's sandbox flag, which became stable in 23.5 (yt-dlp passes the same).
func permissionArgs(ctx context.Context, node string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--version")
	sys.Quiet(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s --version: %w", node, err)
	}
	var v [3]int
	for i, p := range strings.SplitN(strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), ".", 3) {
		v[i], _ = strconv.Atoi(p)
	}
	if v[0] > 23 || (v[0] == 23 && v[1] >= 5) {
		return []string{"--permission"}, nil
	}
	return []string{"--experimental-permission", "--no-warnings=ExperimentalWarning"}, nil
}

// limitedWriter keeps the first n bytes written to it.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
