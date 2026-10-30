package main

import (
	"context"
	"errors"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/ytdlp"
	"yt-dlp-gui/internal/ytweb"
)

// SearchResponse is a page of search results.
type SearchResponse struct {
	Result    *ytdlp.ProbeResult `json:"result"`
	Error     *apperr.AppError   `json:"error"`
	Cancelled bool               `json:"cancelled"`
	// More is set when there are results after these ("Show more").
	More bool `json:"more"`
}

// Search lists YouTube's results for query from the 1-based position first: at most count,
// and fewer when that is what one page of YouTube's holds. Starting another search cancels the
// one in progress.
//
// Results are read without yt-dlp (internal/ytsearch), which is ~3 times as fast; yt-dlp reads
// them when that fails (YouTube changed its pages, say), and when the user's extra yt-dlp
// arguments could route or change requests in ways only yt-dlp would honour.
func (a *App) Search(query string, filter ytdlp.SearchFilter, first, count int) SearchResponse {
	ctx, cancel := context.WithCancel(a.root)
	a.searchMu.Lock()
	if a.searchCancel != nil {
		a.searchCancel()
	}
	a.searchCancel = cancel
	a.searchMu.Unlock()
	defer cancel()

	query, err := ytdlp.CleanQuery(query)
	if err != nil {
		return SearchResponse{Error: toAppError(err)}
	}
	count = min(max(count, 1), 100)
	s := a.store.Get()
	if s.ExtraArgs == "" {
		res, more, err := a.searcher.Search(ctx, ytweb.Options{Proxy: s.Proxy, IPv4: s.ForceIPv4}, query, filter, first, count)
		switch {
		case err == nil:
			return SearchResponse{Result: res, More: more}
		case ctx.Err() != nil:
			return SearchResponse{Cancelled: true}
		}
		a.warn("searching without yt-dlp failed, so yt-dlp searches instead: " + err.Error())
	}

	env, cleanup, err := a.buildEnv(ctx)
	if err != nil {
		return SearchResponse{Error: toAppError(err)}
	}
	defer cleanup()
	res, err := ytdlp.Search(ctx, env, query, filter, first, count)
	switch {
	case errors.Is(err, context.Canceled):
		return SearchResponse{Cancelled: true}
	case err != nil:
		return SearchResponse{Error: toAppError(err)}
	}
	// yt-dlp returns as many as asked for while there are more.
	return SearchResponse{Result: res, More: len(res.Playlist.Entries) == count}
}
