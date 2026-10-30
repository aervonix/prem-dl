// Package cookiebridge receives YouTube cookies from the companion Chrome extension.
//
// yt-dlp can't read Chrome's cookie store on Windows (it is encrypted for Chrome alone), so a
// small extension reads the cookies through Chrome's API after the user approves it, and posts
// them here. A Session listens on 127.0.0.1 for exactly one delivery, guarded by a random token
// and the extension's origin.
package cookiebridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ExtensionID is fixed by the public key in chromehelper/manifest.json.
const ExtensionID = "hmphkemhpljgdcibdgcicgmbbhmnnpgp"

const maxBody = 1 << 20

var (
	// ErrNotOpened means the helper page never reported in: usually the extension isn't installed
	// in the Chrome profile that opened.
	ErrNotOpened = errors.New("chrome didn't open the cookie helper")
	// ErrDeclined means the user said no, in the page or in Chrome's permission prompt.
	ErrDeclined = errors.New("cookie access was declined")
	// ErrTimedOut means the helper opened but nothing was approved in time.
	ErrTimedOut = errors.New("no answer from Chrome in time")
)

// Cookie is one cookie as chrome.cookies.getAll reports it.
type Cookie struct {
	Domain         string  `json:"domain"`
	HostOnly       bool    `json:"hostOnly"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	HTTPOnly       bool    `json:"httpOnly"`
	Session        bool    `json:"session"`
	ExpirationDate float64 `json:"expirationDate"`
	Name           string  `json:"name"`
	Value          string  `json:"value"`
}

type delivery struct {
	Token     string   `json:"token"`
	Cancelled bool     `json:"cancelled"`
	Cookies   []Cookie `json:"cookies"`
}

// Session is one request for cookies.
type Session struct {
	ln     net.Listener
	token  string
	hello  chan struct{}
	result chan delivery
}

// Listen starts a session on a random loopback port.
func Listen() (*Session, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		ln.Close()
		return nil, err
	}
	return &Session{ln: ln, token: hex.EncodeToString(b), hello: make(chan struct{}, 1), result: make(chan delivery, 1)}, nil
}

// HelperURL is the extension page to open in Chrome for this session.
func (s *Session) HelperURL() string {
	port := s.ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("chrome-extension://%s/connect.html#%d.%s", ExtensionID, port, s.token)
}

// Wait serves the session until cookies arrive, the user declines, or time runs out. The page
// has openWithin to report in, then answerWithin for the user to approve.
func (s *Session) Wait(ctx context.Context, openWithin, answerWithin time.Duration) ([]Cookie, error) {
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(s.ln)
	// Shutdown, not Close: the delivery handler hands over the result before it writes its
	// reply, and Close would cut that reply off, so the page reported a failure that wasn't.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if srv.Shutdown(ctx) != nil {
			srv.Close()
		}
	}()

	opened := time.NewTimer(openWithin)
	defer opened.Stop()
	var answer <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-opened.C:
			return nil, ErrNotOpened
		case <-s.hello:
			if answer == nil {
				opened.Stop()
				t := time.NewTimer(answerWithin)
				defer t.Stop()
				answer = t.C
			}
		case <-answer:
			return nil, ErrTimedOut
		case d := <-s.result:
			if d.Cancelled {
				return nil, ErrDeclined
			}
			return d.Cookies, nil
		}
	}
}

func (s *Session) validToken(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

// ServeHTTP accepts only the helper page: its exact origin, the session token, one delivery.
func (s *Session) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const origin = "chrome-extension://" + ExtensionID
	if r.Header.Get("Origin") != origin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	switch {
	case r.Method == http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/hello":
		if !s.validToken(r.URL.Query().Get("token")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		select {
		case s.hello <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/cookies":
		var d delivery
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&d); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !s.validToken(d.Token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		select {
		case s.result <- d:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "already received", http.StatusConflict)
		}
	default:
		http.NotFound(w, r)
	}
}

// Netscape renders the youtube.com cookies in the cookies.txt format yt-dlp reads. Cookies for
// other sites are dropped, as are ones that can't be written on a tab-separated line.
func Netscape(cookies []Cookie) (data []byte, count int) {
	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n# Exported from Chrome by Prem-dl\n\n")
	for _, c := range cookies {
		host := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
			continue
		}
		if c.Name == "" || strings.ContainsAny(c.Name+c.Value+c.Path, "\t\r\n") {
			continue
		}
		domain, sub := c.Domain, "FALSE"
		if !c.HostOnly {
			domain, sub = "."+host, "TRUE"
		}
		if c.HTTPOnly {
			domain = "#HttpOnly_" + domain
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		expiry := int64(0)
		if !c.Session {
			expiry = int64(c.ExpirationDate)
		}
		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", domain, sub, path, secure, strconv.FormatInt(expiry, 10), c.Name, c.Value)
		count++
	}
	return []byte(b.String()), count
}
