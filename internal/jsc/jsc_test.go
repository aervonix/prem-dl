package jsc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fake solver scripts with the real ones' contract (lib.min.js declares lib, core.min.js jsc,
// which preprocesses a player into code that sets _result.n and _result.sig). Each parse counts
// itself into the functions it makes, so a cached player shows in the answers. A player "exit"
// stops the runtime and "loop" never returns.
const (
	fakeLib  = `var lib = {meriyah: {}, astring: {}};`
	fakeCore = `var parses = 0;
var jsc = function (input) {
  let pre = input.preprocessed_player;
  if (input.type === 'player') {
    if (input.player === 'exit') process.exit(3);
    if (input.player === 'loop') for (;;) {}
    parses++;
    pre = '_result.n = (x) => x.split("").reverse().join("") + "#' + parses + '"; _result.sig = (x) => x + "!";';
  }
  const r = {n: null, sig: null};
  Function('_result', pre)(r);
  const o = {type: 'result', responses: input.requests.map((q) => ({type: 'result', data: Object.fromEntries(q.challenges.map((c) => [c, r[q.type](c)]))}))};
  if (input.type === 'player' && input.output_preprocessed) o.preprocessed_player = pre;
  return o;
};`
)

func fakeSolver(t *testing.T) *Solver {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed")
	}
	dir := t.TempDir()
	for name, code := range map[string]string{"lib.min.js": fakeLib, "core.min.js": fakeCore} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewSolver(node, dir)
	t.Cleanup(s.Close)
	return s
}

func solve(t *testing.T, s *Solver, player string) map[string]any {
	t.Helper()
	out, err := s.Solve(context.Background(), Request{PlayerURL: "https://www.youtube.com/s/player/p1/base.js", Player: player,
		Requests: json.RawMessage(`[{"type":"n","challenges":["abc"]},{"type":"sig","challenges":["xy"]}]`)})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	return m
}

func TestSolverParsesEachPlayerOnce(t *testing.T) {
	s := fakeSolver(t)
	if got := solve(t, s, ""); got["type"] != "need_player" {
		t.Fatalf("unknown player: %v", got)
	}
	want := `[{"data":{"abc":"cba#1"},"type":"result"},{"data":{"xy":"xy!"},"type":"result"}]`
	for i, player := range []string{"player code", "", "player code"} {
		got := solve(t, s, player)
		resp, _ := json.Marshal(got["responses"])
		if got["type"] != "result" || string(resp) != want {
			t.Errorf("solve %d: %v", i+1, got)
		}
	}
}

func TestSolverStartsAgainAfterTrouble(t *testing.T) {
	s := fakeSolver(t)
	old := timeout
	timeout = time.Second
	defer func() { timeout = old }()
	for _, player := range []string{"exit", "loop"} {
		if _, err := s.Solve(context.Background(), Request{PlayerURL: "https://p/" + player, Player: player,
			Requests: json.RawMessage(`[{"type":"n","challenges":["a"]}]`)}); err == nil {
			t.Errorf("player %q: no error", player)
		}
		// A new worker answers, and has forgotten the players the old one knew.
		if got := solve(t, s, ""); got["type"] != "need_player" {
			t.Errorf("after %q: %v", player, got)
		}
		if got := solve(t, s, "player code"); got["type"] != "result" {
			t.Errorf("after %q: %v", player, got)
		}
	}
}

func TestServerNeedsItsToken(t *testing.T) {
	srv, err := Serve(fakeSolver(t))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	env := map[string]string{}
	for _, kv := range srv.Env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	post := func(token, body string) int {
		req, _ := http.NewRequest(http.MethodPost, env[EnvURL], bytes.NewBufferString(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	body := `{"player_url":"https://p/1","player":"code","requests":[{"type":"n","challenges":["a"]}]}`
	for name, c := range map[string]struct {
		token, body string
		want        int
	}{
		"no token":    {"", body, http.StatusNotFound},
		"wrong token": {strings.Repeat("0", 64), body, http.StatusNotFound},
		"bad request": {env[EnvToken], `{"player_url":""}`, http.StatusBadRequest},
		"solved":      {env[EnvToken], body, http.StatusOK},
	} {
		if got := post(c.token, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}

func TestInstallPlugin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "premdl", "yt_dlp_plugins", "extractor", "premdl_jsc.py")
	for range 2 {
		if err := InstallPlugin(dir); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, plugin) {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

func TestScriptsDir(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ScriptsDir(bin); got != "" {
		t.Errorf("a single-file yt-dlp has scripts at %q", got)
	}
	solver := filepath.Join(dir, "_internal", "yt_dlp_ejs", "yt", "solver")
	if err := os.MkdirAll(solver, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"lib.min.js", "core.min.js"} {
		if err := os.WriteFile(filepath.Join(solver, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Through a symlink, as a dev build's ./yt-dlp points into yt-dlp.d.
	link := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	if got := ScriptsDir(link); got != solver {
		t.Errorf("ScriptsDir = %q, want %q", got, solver)
	}
}

func TestWorkerEnvKeepsNoSecrets(t *testing.T) {
	got := workerEnv([]string{"PATH=/bin", "GITHUB_TOKEN=x", "NODE_OPTIONS=--require=/tmp/x.js", "SystemRoot=C:\\Windows", "HTTPS_PROXY=http://u:p@h"})
	if strings.Join(got, ";") != "PATH=/bin;SystemRoot=C:\\Windows" {
		t.Errorf("workerEnv = %v", got)
	}
}
