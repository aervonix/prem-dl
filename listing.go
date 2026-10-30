package main

import (
	"context"
	"sync"
	"time"

	"yt-dlp-gui/internal/ytdlp"
	"yt-dlp-gui/internal/ytweb"
)

// A channel or playlist analysed on the Download page is shown as soon as its first page is
// read (~0.4 s): YouTube gives a channel 30 videos per request, one request after another, so
// a channel of 1,000 videos took 5 s to list in full and one of 8,000 nearly a minute. The page
// asks for the rest with MoreEntries while it is read. Entries only ever come after the ones
// shown, so their numbers and the ticks on them stay put.

// ListingPart is the next batch of a listing being read.
type ListingPart struct {
	Entries []ytdlp.PlaylistEntry `json:"entries"`
	Done    bool                  `json:"done"`
	// Error says why the rest couldn't be read, when Done; the entries shown stay.
	Error string `json:"error"`
}

// listing is the rest of a listing on its way to the page.
type listing struct {
	id      int64
	mu      sync.Mutex
	pending []ytdlp.PlaylistEntry
	done    bool
	err     string
	wake    chan struct{} // closed, and replaced, when something arrives
}

func (l *listing) add(es []ytdlp.PlaylistEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = append(l.pending, es...)
	l.signal()
}

func (l *listing) finish(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.done = true
	if err != nil {
		l.err = err.Error()
	}
	l.signal()
}

func (l *listing) signal() {
	close(l.wake)
	l.wake = make(chan struct{})
}

// startListing lists link without yt-dlp and returns the listing as far as its first page,
// with the ID MoreEntries gives the rest under. ok is false when yt-dlp has to list it (see
// listNative), or nothing could be read; ctx is then left as it was. Otherwise the listing owns
// ctx and calls cancel when it ends, and cancelling ctx stops it.
func (a *App) startListing(ctx context.Context, cancel context.CancelFunc, link string) (res *ytdlp.ProbeResult, id int64, ok bool) {
	s := a.store.Get()
	if s.ExtraArgs != "" || !ytweb.Handles(link) {
		return nil, 0, false
	}
	a.listingMu.Lock()
	a.listingSeq++
	l := &listing{id: a.listingSeq, wake: make(chan struct{})}
	a.listingMu.Unlock()

	first := make(chan *ytdlp.ProbeResult, 1)
	failed := make(chan struct{})
	go func() {
		started := false
		_, err := a.lister.List(ctx, ytweb.Options{Proxy: s.Proxy, IPv4: s.ForceIPv4}, link, 0, func(p *ytdlp.ProbeResult) {
			if !started {
				started = true
				first <- p
				return
			}
			l.add(p.Playlist.Entries)
		})
		if !started {
			if ctx.Err() == nil {
				a.warn("listing without yt-dlp failed, so yt-dlp lists it instead: " + err.Error())
			}
			close(failed)
			return
		}
		defer cancel()
		if ctx.Err() != nil {
			err = nil // another link was analysed; nobody is waiting for this one
		}
		l.finish(err)
	}()
	select {
	case p := <-first:
		a.listingMu.Lock()
		a.listing = l
		a.listingMu.Unlock()
		return p, l.id, true
	case <-failed:
		return nil, 0, false
	}
}

// moreWait is how long MoreEntries waits for something new before answering with nothing;
// the page then asks again. Replaced in tests.
var moreWait = 30 * time.Second

// MoreEntries returns the entries of listing id read since the last call, waiting for some
// when there are none yet. Done is set once there are no more; a listing another analysis
// replaced is done at once.
func (a *App) MoreEntries(id int64) ListingPart {
	a.listingMu.Lock()
	l := a.listing
	a.listingMu.Unlock()
	if l == nil || l.id != id {
		return ListingPart{Entries: []ytdlp.PlaylistEntry{}, Done: true}
	}
	deadline := time.After(moreWait)
	for {
		l.mu.Lock()
		if len(l.pending) > 0 || l.done {
			part := ListingPart{Entries: l.pending, Done: l.done, Error: l.err}
			l.pending = nil
			l.mu.Unlock()
			if part.Entries == nil {
				part.Entries = []ytdlp.PlaylistEntry{}
			}
			return part
		}
		wake := l.wake
		l.mu.Unlock()
		select {
		case <-wake:
		case <-deadline:
			return ListingPart{Entries: []ytdlp.PlaylistEntry{}}
		case <-a.root.Done():
			return ListingPart{Entries: []ytdlp.PlaylistEntry{}, Done: true}
		}
	}
}
