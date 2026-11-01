package main

import (
	"context"
	"sync"
	"time"

	"yt-dlp-gui/internal/ytdlp"
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

	// A paced listing (a followed channel's view) reads only as far as the page wants: the
	// reader waits once it has want entries, until the page asks for more.
	paced  bool
	want   int           // 0 = all
	got    int           // entries read so far
	demand chan struct{} // closed, and replaced, when want grows
}

func newListing(id int64, paced bool, want int) *listing {
	return &listing{id: id, wake: make(chan struct{}), paced: paced, want: want, demand: make(chan struct{})}
}

func (l *listing) add(es []ytdlp.PlaylistEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = append(l.pending, es...)
	l.got += len(es)
	l.signal()
}

// pace holds the reader while it has read what the page wants.
func (l *listing) pace(ctx context.Context) {
	for {
		l.mu.Lock()
		if !l.paced || l.want == 0 || l.got < l.want {
			l.mu.Unlock()
			return
		}
		d := l.demand
		l.mu.Unlock()
		select {
		case <-d:
		case <-ctx.Done():
			return
		}
	}
}

// ask raises how far a paced listing reads (0 = to the end).
func (l *listing) ask(want int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.want == 0 || (want != 0 && want <= l.want) {
		return
	}
	l.want = want
	close(l.demand)
	l.demand = make(chan struct{})
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
	a.listingMu.Lock()
	a.listingSeq++
	l := newListing(a.listingSeq, false, 0)
	a.listingMu.Unlock()
	if res, ok = a.readListing(ctx, cancel, link, l); !ok {
		return nil, 0, false
	}
	a.listingMu.Lock()
	a.listing = l
	a.listingMu.Unlock()
	return res, l.id, true
}

// readListing starts reading link into l, and returns its first page; see startListing.
func (a *App) readListing(ctx context.Context, cancel context.CancelFunc, link string, l *listing) (res *ytdlp.ProbeResult, ok bool) {
	list, ok := a.nativeLister(link)
	if !ok {
		return nil, false
	}

	first := make(chan *ytdlp.ProbeResult, 1)
	failed := make(chan struct{})
	go func() {
		started := false
		_, err := list(ctx, 0, func(p *ytdlp.ProbeResult) {
			if !started {
				started = true
				if p.Playlist != nil {
					l.mu.Lock()
					l.got = len(p.Playlist.Entries)
					l.mu.Unlock()
				}
				first <- p
				l.pace(ctx)
				return
			}
			l.add(p.Playlist.Entries)
			l.pace(ctx)
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
		return p, true
	case <-failed:
		return nil, false
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
	return a.nextPart(l)
}

// nextPart returns what l has read since the last call, waiting for something.
func (a *App) nextPart(l *listing) ListingPart {
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
