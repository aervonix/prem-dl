package ytdlp

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// InfoCache keeps the JSON of videos analysed recently, so a download that starts soon after
// can load it (--load-info-json) instead of reading the video's page again: that is the
// webpage, the player API and the JavaScript challenge, about 2.5 s before any bytes flow.
//
// Only YouTube videos are kept. Their stream links last 6 hours from the analysis (the
// "expire" parameter) and are tied to the IP address that asked; other sites' links can
// expire within minutes. Live and upcoming streams are left out: they have to be read when
// they start. Each entry is used once, so a retry reads the page as before.
type InfoCache struct {
	mu    sync.Mutex
	items map[string]*cachedInfo
	gen   uint64 // counts Clear calls
	now   func() time.Time
}

type cachedInfo struct {
	gz    []byte // the JSON, gzipped: about 40 KB rather than 700 KB for a YouTube video
	added time.Time
}

const (
	// InfoTTL is how long an analysis is used for. Far inside YouTube's 6 hours, so a
	// download that starts late in the window still has plenty of time.
	InfoTTL = time.Hour
	// maxInfos bounds the cache (a bulk add analyses every link it is given).
	maxInfos = 200
)

// NewInfoCache returns an empty cache.
func NewInfoCache() *InfoCache {
	return &InfoCache{items: map[string]*cachedInfo{}, now: time.Now}
}

// Gen is read before an analysis starts and handed to PutFrom, so an analysis made under
// settings that a Clear has since replaced isn't kept.
func (c *InfoCache) Gen() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// Put remembers the analysis of a video under the link it was read from and its own URL
// (the link the UI queues). Results that can't be reused are ignored.
func (c *InfoCache) Put(link string, res *ProbeResult) { c.PutFrom(c.Gen(), link, res) }

// PutFrom is Put for an analysis that started when the cache was at gen.
func (c *InfoCache) PutFrom(gen uint64, link string, res *ProbeResult) {
	if res == nil || res.Kind != "video" || res.Video == nil || len(res.info) == 0 {
		return
	}
	v := res.Video
	if v.Extractor != "Youtube" || v.IsLive || v.Upcoming {
		return
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if _, err := zw.Write(res.info); err != nil || zw.Close() != nil {
		return
	}
	item := &cachedInfo{gz: buf.Bytes(), added: c.now()}
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return // cleared while it ran: it used the old cookies or network settings
	}
	c.pruneLocked()
	for _, u := range []string{link, v.URL} {
		if k := infoKey(u); k != "" {
			c.items[k] = item
		}
	}
	for len(c.items) > maxInfos {
		c.dropOldestLocked()
	}
}

// Take removes the analysis of link and writes it to a private temp file for
// --load-info-json. It returns "" when there is none, or it is too old. done deletes the file.
func (c *InfoCache) Take(link string) (path string, done func()) {
	k := infoKey(link)
	c.mu.Lock()
	c.pruneLocked()
	item := c.items[k]
	if item != nil {
		for key, it := range c.items {
			if it == item {
				delete(c.items, key) // every link it was stored under
			}
		}
	}
	c.mu.Unlock()
	if item == nil {
		return "", func() {}
	}
	zr, err := gzip.NewReader(bytes.NewReader(item.gz))
	if err != nil {
		return "", func() {}
	}
	// It holds cookies and signed stream links: CreateTemp makes it readable by the user only.
	tmp, err := os.CreateTemp("", "ytgui-info-*.json")
	if err != nil {
		return "", func() {}
	}
	name := tmp.Name()
	done = func() { os.Remove(name) }
	_, err = io.Copy(tmp, zr)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		done()
		return "", func() {}
	}
	return name, done
}

// Has reports whether a fresh analysis of link is kept.
func (c *InfoCache) Has(link string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	_, ok := c.items[infoKey(link)]
	return ok
}

// IsYouTubeVideo reports whether link is a single YouTube video (a watch, youtu.be, Shorts or
// live link), the kind of link whose analysis the cache keeps.
func IsYouTubeVideo(link string) bool {
	return YouTubeVideoID(link) != ""
}

// YouTubeVideoID is the video ID of a single YouTube video's link (watch, youtu.be, Shorts, live),
// or "" for any other link.
func YouTubeVideoID(link string) string {
	norm, err := NormalizeURL(link)
	if err != nil {
		return ""
	}
	u, err := url.Parse(norm)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "m.")
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	switch {
	case host == "youtu.be":
		if len(segs) == 1 {
			return segs[0]
		}
	case host != "youtube.com" && host != "music.youtube.com":
	case u.Path == "/watch":
		return u.Query().Get("v")
	case len(segs) == 2 && (segs[0] == "shorts" || segs[0] == "live"):
		return segs[1]
	}
	return ""
}

// Clear forgets everything, for when settings that change what a page returns (cookies, proxy,
// IPv4) were changed.
func (c *InfoCache) Clear() {
	c.mu.Lock()
	clear(c.items)
	c.gen++
	c.mu.Unlock()
}

func (c *InfoCache) pruneLocked() {
	cutoff := c.now().Add(-InfoTTL)
	for k, it := range c.items {
		if it.added.Before(cutoff) {
			delete(c.items, k)
		}
	}
}

func (c *InfoCache) dropOldestLocked() {
	var oldest *cachedInfo
	for _, it := range c.items {
		if oldest == nil || it.added.Before(oldest.added) {
			oldest = it
		}
	}
	for k, it := range c.items {
		if it == oldest {
			delete(c.items, k)
		}
	}
}

func infoKey(link string) string {
	u, err := NormalizeURL(link)
	if err != nil {
		return ""
	}
	return u
}
