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
func (a *App) EditFeed(id string, tabs []string, opts settings.DownloadOptions) FeedResponse {
	f, err := a.feeds.Edit(id, tabs, opts)
	if err != nil {
		return FeedResponse{Error: err.Error()}
	}
	return FeedResponse{Feed: &f}
}

// UnfollowFeed stops following. Downloads it queued stay in the queue.
func (a *App) UnfollowFeed(id string) error { return a.feeds.Unfollow(id) }

// PauseFeed stops (or restarts) the automatic checks of a feed.
func (a *App) PauseFeed(id string, paused bool) error { return a.feeds.SetPaused(id, paused) }

// BrowseFeed lists a followed feed's videos, newest first on a channel tab ("" for the feed's
// own link, or the first followed tab), up to limit of them (0 means all). Starting another
// cancels it.
func (a *App) BrowseFeed(id, tab string, limit int) ProbeResponse {
	link, err := a.feeds.Source(id, tab)
	if err != nil {
		return ProbeResponse{Error: toAppError(err)}
	}
	ctx, cancel := context.WithCancel(a.root)
	a.browseMu.Lock()
	if a.browseCancel != nil {
		a.browseCancel()
	}
	a.browseCancel = cancel
	a.browseMu.Unlock()
	defer cancel()

	if res, ok := a.listNative(ctx, link, max(limit, 0)); ok {
		return ProbeResponse{Result: res}
	}
	env, cleanup, err := a.buildEnv(ctx)
	if err != nil {
		return ProbeResponse{Error: toAppError(err)}
	}
	defer cleanup()
	res, err := ytdlp.List(ctx, env, link, max(limit, 0))
	switch {
	case errors.Is(err, context.Canceled):
		return ProbeResponse{Cancelled: true}
	case err != nil:
		return ProbeResponse{Error: toAppError(err)}
	}
	return ProbeResponse{Result: res}
}

// CancelBrowse stops a BrowseFeed in progress, if any.
func (a *App) CancelBrowse() {
	a.browseMu.Lock()
	defer a.browseMu.Unlock()
	if a.browseCancel != nil {
		a.browseCancel()
		a.browseCancel = nil
	}
}

// CheckFeed checks a feed for new uploads now, or with id "" every feed that isn't paused.
func (a *App) CheckFeed(id string) error { return a.feeds.CheckNow(id) }
