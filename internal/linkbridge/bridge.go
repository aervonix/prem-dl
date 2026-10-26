// Package linkbridge receives links from the companion Chrome extension while the app runs: its
// toolbar button and "Download with Prem-dl" menu item post the page or link here, and the app
// opens it on the Download page.
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
	// Open is given each link. Its error is sent back for the extension to show.
	Open func(link string) error
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

type request struct {
	URL string `json:"url"`
}

// reply always names the app, so the extension can tell it reached Prem-dl and not some other
// program that happens to use the port.
type reply struct {
	App   string `json:"app"`
	Error string `json:"error,omitempty"`
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
	case r.Method == http.MethodPost && r.URL.Path == "/open":
		var req request
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil || req.URL == "" {
			send(w, http.StatusBadRequest, "No link was sent.")
			return
		}
		if err := s.Open(req.URL); err != nil {
			send(w, http.StatusBadRequest, err.Error())
			return
		}
		send(w, http.StatusOK, "")
	default:
		http.NotFound(w, r)
	}
}

func send(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(reply{App: "prem-dl", Error: msg})
}
