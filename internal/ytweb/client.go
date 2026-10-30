package ytweb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// The web client as yt-dlp 2026.10.26 describes it (INNERTUBE_CLIENTS["web"]). YouTube takes
// versions months old; a channel page gives the current one, which its continuations use.
const (
	clientName    = "WEB"
	clientID      = "1"
	clientVersion = "2.20260708.00.00"
	userAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	// SOCS accepts YouTube's terms up front, as yt-dlp does, so EU visitors aren't sent to a
	// consent page; PREF asks for English and UTC, which "3 years ago" is parsed in.
	cookies = "SOCS=CAI; PREF=hl=en&tz=UTC"

	maxBody = 16 << 20
)

// Options are the network settings a request honours.
type Options struct {
	Proxy string // "" for the system's
	IPv4  bool   // connect over IPv4 only (Settings → Network)
}

// clients keeps an HTTP client per Options, so requests reuse their connection to YouTube.
type clients struct {
	mu sync.Mutex
	m  map[Options]*http.Client
}

func (c *clients) get(o Options) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if hc, ok := c.m[o]; ok {
		return hc
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.Proxy != "" {
		if u, err := url.Parse(o.Proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	if o.IPv4 {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp4", addr)
		}
	}
	hc := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	if c.m == nil {
		c.m = map[Options]*http.Client{}
	}
	c.m[o] = hc
	return hc
}

// innertube is the request context of the web client: version "" for yt-dlp's.
func innertube(version string) map[string]any {
	if version == "" {
		version = clientVersion
	}
	return map[string]any{"client": map[string]any{
		"clientName": clientName, "clientVersion": version,
		"hl": "en", "timeZone": "UTC", "utcOffsetMinutes": 0,
	}}
}

// callAPI POSTs body, with the web client's context added, to one of YouTube's web API
// endpoints and returns the answer. visitor is the session's visitor ID, if it has one.
func callAPI(ctx context.Context, hc *http.Client, endpoint string, body map[string]any, version, visitor string) ([]byte, error) {
	body["context"] = innertube(version)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", "https://www.youtube.com")
	req.Header.Set("X-YouTube-Client-Name", clientID)
	req.Header.Set("X-YouTube-Client-Version", innertube(version)["client"].(map[string]any)["clientVersion"].(string))
	req.Header.Set("Cookie", cookies)
	if visitor != "" {
		req.Header.Set("X-Goog-Visitor-Id", visitor)
	}
	return send(hc, req)
}

// getPage fetches one of YouTube's web pages.
func getPage(ctx context.Context, hc *http.Client, link string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", cookies)
	return send(hc, req)
}

func send(hc *http.Client, req *http.Request) ([]byte, error) {
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YouTube answered %s", resp.Status)
	}
	// A page sent elsewhere (consent.youtube.com, say) isn't what was asked for.
	if h := resp.Request.URL.Hostname(); h != req.URL.Hostname() {
		return nil, fmt.Errorf("YouTube sent the request on to %s", h)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}
