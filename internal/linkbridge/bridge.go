// Package linkbridge receives links from the companion Chrome extension while the app runs: its
// toolbar button and right-click menu post the page or link here, to open on the Download page,
// to download at once with a preset, or to follow.
//
// It listens on a fixed loopback port so the extension can find it, and accepts requests only
// from the extension's origin, which web pages can't fake. When no port is free, or the app isn't
// running, the extension falls back to the prem-dl: link type instead.
package linkbridge

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
)

// Ports are tried in order. chromehelper/background.js tries the same list.
var Ports = []int{38719, 41853, 46379}

const maxBody = 16 << 10

// Server handles the extension's requests.
type Server struct {
	// Origin is the only Origin header accepted: chrome-extension://<id>.
	Origin string
	// Handle is given each link. Its error is sent back for the extension to show.
	Handle func(Request) error
	// Presets names the presets the extension's menu offers; every reply carries them.
	Presets func() []string
}

// What a Request asks for.
const (
	ActionOpen     = ""         // put it on the Download page to review
	ActionDownload = "download" // queue it now, with Preset's options
	ActionFollow   = "follow"   // open the follow dialog for it
)

// Request is a link from the extension.
type Request struct {
	URL    string `json:"url"`
	Action string `json:"action"`
	// Preset names the preset a download uses; "" for the default options (or the preset of
	// the link's site).
	Preset string `json:"preset"`
}

// Listen takes the first free port in Ports.
func Listen() (net.Listener, error) {
	var errs []error
	for _, p := range Ports {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			return ln, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

// reply always names the app, so the extension can tell it reached Prem-dl and not some other
// program that happens to use the port.
type reply struct {
	App     string   `json:"app"`
	Error   string   `json:"error,omitempty"`
	Presets []string `json:"presets"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The Host check stops DNS rebinding (a page whose name resolves to 127.0.0.1); the Origin
	// check stops everything else a browser could send.
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.Origin == "" || r.Header.Get("Origin") != s.Origin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", s.Origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	switch {
	case r.Method == http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/hello":
		// The extension asks when it starts, to build its menu.
		s.send(w, http.StatusOK, "")
	case r.Method == http.MethodPost && r.URL.Path == "/open":
		var req Request
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil || req.URL == "" {
			s.send(w, http.StatusBadRequest, "No link was sent.")
			return
		}
		switch req.Action {
		case ActionOpen, ActionDownload, ActionFollow:
		default:
			s.send(w, http.StatusBadRequest, "This Prem-dl doesn't know that action. Is it up to date?")
			return
		}
		if err := s.Handle(req); err != nil {
			s.send(w, http.StatusBadRequest, err.Error())
			return
		}
		s.send(w, http.StatusOK, "")
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) send(w http.ResponseWriter, status int, msg string) {
	presets := []string{}
	if s.Presets != nil {
		presets = append(presets, s.Presets()...)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(reply{App: "prem-dl", Error: msg, Presets: presets})
}
