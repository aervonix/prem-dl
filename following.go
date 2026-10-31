package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"yt-dlp-gui/internal/follow"
	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// Followed channels and playlists: the feed list lives in internal/follow; this connects it to
// yt-dlp, the queue and the UI.

func (a *App) newFeeds() *follow.Manager {
	return follow.New(follow.Deps{
		List: func(ctx context.Context, link string, limit int) (*ytdlp.ProbeResult, error) {
			if res, ok := a.listNative(ctx, link, limit); ok {
				return res, nil
			}
			env, cleanup, err := a.buildEnv(ctx)
			if err != nil {
				return nil, err
			}
			defer cleanup()
			return ytdlp.List(ctx, env, link, limit)
		},
		Enqueue: func(items []queue.NewJob, opts settings.DownloadOptions) (int, error) {
			jobs, _, err := a.q.Add(items, opts)
			return len(jobs), err
		},
		Archive: a.archiveEntries,
		Every:   func() time.Duration { return time.Duration(a.store.Get().FollowEvery) * time.Hour },
		Emit:    a.emit,
		Path:    filepath.Join(a.dataDir, "feeds.json"),
	})
}

// archiveEntries returns the download archive's lines ("youtube abc123") when "Remember
// downloaded videos" is on, so checks don't queue what was downloaded before.
func (a *App) archiveEntries() map[string]bool {
	if !a.store.Get().UseArchive {
		return nil
	}
	raw, err := settings.ReadFileRetry(a.archivePath())
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out[l] = true
		}
	}
	return out
}

// FeedResponse carries a feed or a message for the user.
type FeedResponse struct {
	Feed  *follow.Feed `json:"feed"`
	Error string       `json:"error"`
}

// TargetResponse says how a link would be followed, or why it can't be.
type TargetResponse struct {
	Target follow.Target `json:"target"`
	Error  string        `json:"error"`
}

// ListFeeds returns the followed channels and playlists.
func (a *App) ListFeeds() []follow.Feed { return a.feeds.List() }

// InspectFeed says how a link would be followed (a YouTube channel chooses its tabs), and whether
// it already is.
func (a *App) InspectFeed(link string) TargetResponse {
	t, err := a.feeds.Inspect(link)
	if err != nil {
		return TargetResponse{Target: follow.Target{Tabs: []string{}}, Error: err.Error()}
	}
	return TargetResponse{Target: t}
}

// FollowFeed starts following a channel or playlist. Only uploads that appear from now on are
// queued.
func (a *App) FollowFeed(nf follow.NewFeed) FeedResponse {
	f, err := a.feeds.Follow(nf)
	if err != nil {
		return FeedResponse{Error: err.Error()}
	}
	return FeedResponse{Feed: &f}
}

// EditFeed changes a feed's download options and, for a channel, which tabs are checked.
func (a *App) EditFeed(id string, tabs []string, opts settings.DownloadOptions, filter follow.Filter) FeedResponse {
	f, err := a.feeds.Edit(id, tabs, opts, filter)
	if err != nil {
		return FeedResponse{Error: err.Error()}
	}
	return FeedResponse{Feed: &f}
}

// UnfollowFeed stops following. Downloads it queued stay in the queue.
func (a *App) UnfollowFeed(id string) error { return a.feeds.Unfollow(id) }

// PauseFeed stops (or restarts) the automatic checks of a feed.
func (a *App) PauseFeed(id string, paused bool) error { return a.feeds.SetPaused(id, paused) }

// feedTab is one tab of a followed feed being read for its view.
type feedTab struct {
	l      *listing
	cancel context.CancelFunc
}

// BrowseFeed lists a followed feed's videos, newest first on a channel tab ("" for the feed's
// own link, or the first followed tab), up to limit of them (0 means all).
//
// Read without yt-dlp, the answer is the first page, and Listing names the rest: the tab is
// read only as far as MoreFeedEntries asks, so "Show more" carries on from where it stopped
// instead of reading the channel again from the top, and "Show all" streams. Each tab of the
// feed keeps its own listing; another feed, or CancelBrowse, stops them. Through yt-dlp the
// whole answer comes at once, and starting another cancels it.
func (a *App) BrowseFeed(id, tab string, limit int) ProbeResponse {
	link, err := a.feeds.Source(id, tab)
	if err != nil {
		return ProbeResponse{Error: toAppError(err)}
	}
	limit = max(limit, 0)

	a.listingMu.Lock()
	a.listingSeq++
	l := newListing(a.listingSeq, true, limit)
	a.listingMu.Unlock()
	ctx, cancel := context.WithCancel(a.root)
	a.browseMu.Lock()
	if a.browseFeed != id {
		a.stopBrowseLocked()
		a.browseFeed = id
	}
	if old, ok := a.browseTabs[tab]; ok {
		old.cancel() // read again (Reload)
	}
	if a.browseTabs == nil {
		a.browseTabs = map[string]feedTab{}
	}
	a.browseTabs[tab] = feedTab{l: l, cancel: cancel}
	a.browseMu.Unlock()

	if res, ok := a.readListing(ctx, cancel, link, l); ok {
		return ProbeResponse{Result: res, Listing: l.id}
	}
	if ctx.Err() != nil {
		return ProbeResponse{Cancelled: true} // the view closed, or another feed was opened
	}
	a.browseMu.Lock()
	if cur, ok := a.browseTabs[tab]; ok && cur.l == l {
		delete(a.browseTabs, tab)
	}
	if a.browseCancel != nil {
		a.browseCancel()
	}
	a.browseCancel = cancel
	a.browseMu.Unlock()
	defer cancel()

	env, cleanup, err := a.buildEnv(ctx)
	if errors.Is(err, context.Canceled) {
		return ProbeResponse{Cancelled: true}
	}
	if err != nil {
		return ProbeResponse{Error: toAppError(err)}
	}
	defer cleanup()
	res, err := ytdlp.List(ctx, env, link, limit)
	switch {
	case errors.Is(err, context.Canceled):
		return ProbeResponse{Cancelled: true}
	case err != nil:
		return ProbeResponse{Error: toAppError(err)}
	}
	return ProbeResponse{Result: res}
}

// MoreFeedEntries asks a BrowseFeed listing to read on to want entries in all (0 = to the
// end), and returns those read since the last call, waiting for some. Done is set at the end,
// or when the listing was stopped.
func (a *App) MoreFeedEntries(id int64, want int) ListingPart {
	a.browseMu.Lock()
	var l *listing
	for _, t := range a.browseTabs {
		if t.l.id == id {
			l = t.l
		}
	}
	a.browseMu.Unlock()
	if l == nil {
		return ListingPart{Entries: []ytdlp.PlaylistEntry{}, Done: true}
	}
	l.ask(max(want, 0))
	return a.nextPart(l)
}

// CancelBrowse stops what BrowseFeed is reading, when the view closes.
func (a *App) CancelBrowse() {
	a.browseMu.Lock()
	defer a.browseMu.Unlock()
	a.stopBrowseLocked()
	a.browseFeed = ""
}

func (a *App) stopBrowseLocked() {
	if a.browseCancel != nil {
		a.browseCancel()
		a.browseCancel = nil
	}
	for tab, t := range a.browseTabs {
		t.cancel()
		delete(a.browseTabs, tab)
	}
}

// CheckFeed checks a feed for new uploads now, or with id "" every feed that isn't paused.
func (a *App) CheckFeed(id string) error { return a.feeds.CheckNow(id) }
