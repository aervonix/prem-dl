// YouTube search. yt-dlp takes ~2.2 s for a page of results: its own start, a page fetched for
// its client settings, then every page up to the one asked for, again on each "Show more". Here
// the first page is one request (~0.7 s, most of it YouTube's), the session keeps its
// continuation token, and the next page is read in the background while the first is looked at.

package ytweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

const (
	searchAPI = "https://www.youtube.com/youtubei/v1/search?prettyPrint=false"

	// maxPages bounds the pages one call reads to reach the position asked for.
	maxPages = 10
)

// Searcher runs searches. It keeps the newest search's pages, so "Show more" continues from
// where it stopped instead of reading every page again.
type Searcher struct {
	mu      sync.Mutex
	current *session

	api string           // replaced in tests
	now func() time.Time // dates "3 years ago"
}

// New returns a Searcher.
func New() *Searcher { return NewAt(searchAPI) }

// NewAt returns a Searcher that asks api instead of YouTube's search endpoint, for tests.
func NewAt(api string) *Searcher {
	return &Searcher{api: api, now: time.Now}
}

type session struct {
	key    string
	query  string
	params string
	client *http.Client
	// ctx bounds the background read of the next page; cancelled when another search starts.
	ctx    context.Context
	cancel context.CancelFunc
	// lock is held while a page is read, so "Show more" waits for a read already under way
	// rather than starting the same one.
	lock chan struct{}

	entries []entry
	next    string // the next page's token
	visitor string
	started bool // the first page has been read
}

// Search returns count results from the 1-based position first, or fewer: it stops reading
// pages at three quarters of count, so a first page of ~20 comes back on its own when 24 are
// asked for, while sparse results (playlists, of which a page may hold 5) are read on until
// there are enough. more says whether YouTube has results after these. query must be cleaned
// (ytdlp.CleanQuery).
func (s *Searcher) Search(ctx context.Context, o Options, query string, f ytdlp.SearchFilter, first, count int) (res *ytdlp.ProbeResult, more bool, err error) {
	first, count = max(first, 1), max(count, 1)
	enough := (count*3 + 3) / 4
	sess := s.session(o, query, ytdlp.SearchParams(f), first == 1)
	select {
	case sess.lock <- struct{}{}:
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	if err := s.fill(ctx, sess, first-1+enough); err != nil {
		<-sess.lock
		return nil, false, err
	}
	start := min(first-1, len(sess.entries))
	end := min(start+count, len(sess.entries))
	got := slices.Clone(sess.entries[start:end]) // a read ahead appends to sess.entries
	more = len(sess.entries) > end || sess.next != ""
	// The next "Show more" is read while these are looked at.
	ahead := end + enough
	prefetch := len(sess.entries) < ahead && sess.next != ""
	<-sess.lock
	if prefetch {
		go s.prefetch(sess, ahead)
	}
	res, err = result(query, f, got, first)
	return res, more, err
}

// session returns the search's session, starting a new one for a new search (or a search
// started again from its first result, whose results may have changed).
func (s *Searcher) session(o Options, query, params string, restart bool) *session {
	key := fmt.Sprintf("%q %q %+v", query, params, o)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.current; c != nil && c.key == key && !restart {
		return c
	}
	if s.current != nil {
		s.current.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.current = &session{key: key, query: query, params: params, client: pool.get(o),
		ctx: ctx, cancel: cancel, lock: make(chan struct{}, 1)}
	return s.current
}

// fill reads pages until the session holds n results, or there are no more. It reads at most
// maxPages, so a search whose pages bring nothing usable can't go on for ever. Call with
// sess.lock held.
func (s *Searcher) fill(ctx context.Context, sess *session, n int) error {
	for pages := 0; len(sess.entries) < n && (!sess.started || sess.next != "") && pages < maxPages; pages++ {
		if err := s.read(ctx, sess); err != nil {
			return err
		}
	}
	return nil
}

// prefetch reads ahead to n results, unless another read got there first.
func (s *Searcher) prefetch(sess *session, n int) {
	select {
	case sess.lock <- struct{}{}:
	case <-sess.ctx.Done():
		return
	}
	defer func() { <-sess.lock }()
	_ = s.fill(sess.ctx, sess, n) // a failure leaves the token for "Show more" to try again
}

// read fetches the session's next page (its first, to begin with) and adds its results.
// Call with sess.lock held.
func (s *Searcher) read(ctx context.Context, sess *session) error {
	body := map[string]any{}
	if sess.started {
		body["continuation"] = sess.next
	} else {
		body["query"] = sess.query
		if sess.params != "" {
			body["params"] = sess.params
		}
	}
	raw, err := callAPI(ctx, sess.client, s.api, body, "", sess.visitor)
	if err != nil {
		return err
	}
	p, err := parseSearch(raw, extractor{now: s.now(), dates: true})
	if err != nil {
		return err
	}
	sess.entries = append(sess.entries, p.entries...)
	sess.next = p.next
	if p.visitor != "" {
		sess.visitor = p.visitor
	}
	sess.started = true
	return nil
}

// result wraps entries as yt-dlp's playlist JSON for a search and reads it with ParseProbe, the
// same way yt-dlp's answer is read. Entries are numbered from first.
func result(query string, f ytdlp.SearchFilter, entries []entry, first int) (*ytdlp.ProbeResult, error) {
	if entries == nil {
		entries = []entry{}
	}
	data, err := json.Marshal(map[string]any{
		"_type": "playlist", "id": query, "title": query,
		"webpage_url": ytdlp.SearchURL(query, f), "entries": entries,
	})
	if err != nil {
		return nil, err
	}
	res, err := ytdlp.ParseProbe(data)
	if err != nil {
		return nil, err
	}
	if res.Playlist == nil {
		return nil, errors.New("search results didn't read as a list")
	}
	for i := range res.Playlist.Entries {
		res.Playlist.Entries[i].Index = first + i
	}
	return res, nil
}
