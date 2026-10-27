package main

import (
	"context"
	"errors"

	"yt-dlp-gui/internal/ytdlp"
)

// Search lists YouTube's results for query, count of them from the 1-based position first
// ("Show more" asks for the next ones). Starting another search cancels the one in progress.
func (a *App) Search(query string, filter ytdlp.SearchFilter, first, count int) ProbeResponse {
	ctx, cancel := context.WithCancel(a.root)
	a.searchMu.Lock()
	if a.searchCancel != nil {
		a.searchCancel()
	}
	a.searchCancel = cancel
	a.searchMu.Unlock()
	defer cancel()

	env, cleanup, err := a.buildEnv(ctx)
	if err != nil {
		return ProbeResponse{Error: toAppError(err)}
	}
	defer cleanup()
	res, err := ytdlp.Search(ctx, env, query, filter, first, min(max(count, 1), 100))
	switch {
	case errors.Is(err, context.Canceled):
		return ProbeResponse{Cancelled: true}
	case err != nil:
		return ProbeResponse{Error: toAppError(err)}
	}
	return ProbeResponse{Result: res}
}
