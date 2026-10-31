package ytweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Peek is what YouTube's oEmbed says about a video: enough to show while it's analysed.
type Peek struct {
	Title     string `json:"title"`
	Uploader  string `json:"uploader"`
	Thumbnail string `json:"thumbnail"`
}

// oembed is replaced in tests.
var oembed = "https://www.youtube.com/oembed"

// PeekVideo asks oEmbed for a YouTube video's title, channel and thumbnail (~0.15 s, where an
// analysis takes 2 s or more). Private, age-restricted and missing videos fail; the analysis
// says why.
func PeekVideo(ctx context.Context, o Options, link string) (Peek, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := url.Values{"url": {link}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oembed+"?"+q.Encode(), nil)
	if err != nil {
		return Peek{}, err
	}
	resp, err := pool.get(o).Do(req)
	if err != nil {
		return Peek{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Peek{}, errors.New(resp.Status)
	}
	var raw struct {
		Title        string `json:"title"`
		AuthorName   string `json:"author_name"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&raw); err != nil {
		return Peek{}, err
	}
	if u, err := url.Parse(raw.ThumbnailURL); err != nil || u.Scheme != "https" {
		raw.ThumbnailURL = ""
	}
	return Peek{Title: raw.Title, Uploader: raw.AuthorName, Thumbnail: raw.ThumbnailURL}, nil
}
