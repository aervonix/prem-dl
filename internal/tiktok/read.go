package tiktok

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sync"
	"time"
)

const (
	// userAgent is a desktop Chrome's, like the one yt-dlp's TikTok downloads go out with.
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	referer   = "https://www.tiktok.com/"
	maxPage   = 8 << 20 // a post page is ~400 KB
)

// Options are the network settings a read honours.
type Options struct {
	Proxy string // "" for the system's
	IPv4  bool   // connect over IPv4 only (Settings → Network)
}

// transports keeps one HTTP transport per Options, so reads reuse their connection to TikTok.
// Each read has its own cookie jar: the cookies a page sets are the ones its renditions need.
var transports struct {
	mu sync.Mutex
	m  map[Options]*http.Transport
}

func transport(o Options) *http.Transport {
	transports.mu.Lock()
	defer transports.mu.Unlock()
	if tr, ok := transports.m[o]; ok {
		return tr
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
	if transports.m == nil {
		transports.m = map[Options]*http.Transport{}
	}
	transports.m[o] = tr
	return tr
}

// Post is one post as its page describes it.
type Post struct {
	item    map[string]any
	link    string         // the link it was read from
	page    string         // the page that answered (a share link's destination)
	cookies []*http.Cookie // what the page set; its renditions need them
}

// ID is the post's ID.
func (p *Post) ID() string { return str(p.item, "id") }

// Photo reports whether it is a photo post (pictures and a sound rather than a video).
func (p *Post) Photo() bool { return len(p.pictures()) > 0 }

// StatusError is TikTok's own answer about a post it didn't give: 10204 (deleted, or "only
// me"), 10216 (a private post), 10222 (a private account), 10231 (not shown in this country).
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("TikTok gave no post (status %d, %s)", e.Code, e.Message)
}

// ErrWithheld is a post TikTok shows only to signed-in viewers: the page gives its details
// and sound but no video. yt-dlp, with the user's cookies, may still read it.
var ErrWithheld = errors.New("TikTok shows this post only to signed-in viewers")

var rehydration = regexp.MustCompile(`(?s)<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__"[^>]*>(.*?)</script>`)

// Read fetches a post's page and reads the post from it.
func Read(ctx context.Context, o Options, link string) (*Post, error) {
	return read(ctx, transport(o), "https://www.tiktok.com", link)
}

// read is Read from site (a fake TikTok in tests).
func read(ctx context.Context, tr http.RoundTripper, site, link string) (*Post, error) {
	l, ok := ParseLink(link)
	if !ok {
		return nil, fmt.Errorf("not a TikTok post link: %s", link)
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: tr, Jar: jar, Timeout: 30 * time.Second}
	page := l.pageURL(site)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TikTok answered %s", resp.Status)
	}
	final := resp.Request.URL
	if final.Path == "/login" {
		return nil, ErrWithheld // yt-dlp's "TikTok is requiring login for access to this content"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return nil, err
	}
	want := l.ID
	if l.Short {
		// A share link lands on the post's page; its path names the post.
		m := postPath.FindStringSubmatch(final.Path)
		if m == nil {
			return nil, fmt.Errorf("the share link led to %s, not a post", final.Path)
		}
		want = m[3]
	}
	item, err := parsePage(body, want)
	if err != nil {
		return nil, err
	}
	return &Post{item: item, link: link, page: final.String(), cookies: jar.Cookies(final)}, nil
}

// parsePage finds the post's itemStruct in a page.
func parsePage(body []byte, id string) (map[string]any, error) {
	m := rehydration.FindSubmatch(body)
	if m == nil {
		// A page without the payload is a challenge page or a block, which yt-dlp knows how to
		// get past (its _solve_challenge_and_set_cookies).
		return nil, errors.New("TikTok's page carried no post data")
	}
	dec := json.NewDecoder(bytes.NewReader(m[1]))
	dec.UseNumber() // IDs and counts as they are, never rounded through float64
	var data map[string]any
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("TikTok's post data didn't parse: %w", err)
	}
	detail := obj(obj(data, "__DEFAULT_SCOPE__"), "webapp.video-detail")
	item := obj(obj(detail, "itemInfo"), "itemStruct")
	if code := num(detail, "statusCode"); code != 0 || len(item) == 0 {
		msg := str(detail, "statusMsg")
		if msg == "" {
			msg = "no reason given"
		}
		return nil, &StatusError{Code: int(code), Message: msg}
	}
	if str(item, "id") != id {
		// Refuse someone else's post rather than hand it back as the one asked for.
		return nil, errors.New("TikTok's page carried a different post")
	}
	if b, _ := item["isContentClassified"].(bool); b && len(obj(item, "video")) == 0 {
		return nil, ErrWithheld
	}
	return item, nil
}
