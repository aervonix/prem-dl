package jsc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// The yt-dlp plugin, and the environment variables it finds the server by. The token travels in
// the environment, not on the command line, where other users of the computer could read it.
const (
	EnvURL   = "PREMDL_JSC_URL"
	EnvToken = "PREMDL_JSC_TOKEN"
)

//go:embed plugin/premdl_jsc.py
var plugin []byte

// InstallPlugin writes the plugin under dir, as the package yt-dlp's --plugin-dirs=dir finds
// (dir/premdl/yt_dlp_plugins/extractor/premdl_jsc.py), unless it is already there.
func InstallPlugin(dir string) error {
	path := filepath.Join(dir, "premdl", "yt_dlp_plugins", "extractor", "premdl_jsc.py")
	if old, err := os.ReadFile(path); err == nil && string(old) == string(plugin) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, plugin, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Server is the loopback endpoint the plugin asks: POST /solve with a Request, and the token as
// a bearer token.
type Server struct {
	solver *Solver
	token  string
	url    string
	srv    *http.Server
	solved atomic.Int64 // requests answered, for tests and bug reports
}

// Serve starts a server for solver on a free loopback port.
func Serve(solver *Solver) (*Server, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{solver: solver, token: hex.EncodeToString(b), url: "http://" + ln.Addr().String() + "/solve"}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// Env is what a yt-dlp run needs in its environment to use the server.
func (s *Server) Env() []string {
	return []string{EnvURL + "=" + s.url, EnvToken + "=" + s.token}
}

// maxRequest bounds a request: the player script is about 3 MB.
const maxRequest = 32 << 20

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if r.Method != http.MethodPost || r.URL.Path != "/solve" || len(auth) <= len(prefix) ||
		subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(s.token)) != 1 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var req Request
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequest)).Decode(&req); err != nil || req.PlayerURL == "" || len(req.Requests) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out, err := s.solver.Solve(r.Context(), req)
	if err != nil {
		// The plugin declines, and yt-dlp solves the challenges itself.
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	s.solved.Add(1)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// Solved is how many requests the server has answered.
func (s *Server) Solved() int64 { return s.solved.Load() }

// Close stops the server and the solver.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.srv.Shutdown(ctx) != nil {
		_ = s.srv.Close() // requests still running after 2 s are cut off
	}
	s.solver.Close()
}
