package main

import (
	"context"
	"time"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/ytdlp"
)

// Downloads that weren't analysed first (ticked in search results, added in bulk, queued by a
// followed channel) read each video's page when they start: ~2.5 s before the first bytes. While
// others download, prefetcher analyses the YouTube videos next in line, one at a time, so they
// start from the saved analysis instead (ytdlp.InfoCache, --load-info-json): ~0.7 s.

type prefetcher struct {
	jobs    func() []queue.Job                     // the queue, in order
	slots   func() int                             // downloads that run at once (MaxConcurrent)
	has     func(link string) bool                 // a fresh analysis is kept
	analyse func(ctx context.Context, link string) // analyses link and keeps the result
	now     func() time.Time

	kick  chan struct{}
	tried map[string]time.Time // links analysed ahead, and when; one try each per InfoTTL
}

func newPrefetcher(jobs func() []queue.Job, slots func() int, has func(string) bool, analyse func(context.Context, string)) *prefetcher {
	return &prefetcher{jobs: jobs, slots: slots, has: has, analyse: analyse, now: time.Now,
		kick: make(chan struct{}, 1), tried: map[string]time.Time{}}
}

// wake has the prefetcher look at the queue again; the queue calls it on every change.
func (p *prefetcher) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// run analyses ahead until ctx ends. A video whose download starts while it is analysed is
// left alone: the download reads the page itself, and the analysis would be wasted.
func (p *prefetcher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.kick:
		}
		for link := p.next(); link != "" && ctx.Err() == nil; link = p.next() {
			p.tried[link] = p.now()
			actx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				p.analyse(actx, link)
			}()
		wait:
			for {
				select {
				case <-done:
					break wait
				case <-p.kick:
					if !p.waiting(link) {
						cancel()
					}
				case <-ctx.Done():
					cancel()
				}
			}
			cancel()
		}
	}
}

// next is the first video worth analysing among those next in line: the queued jobs that start
// as the running ones finish, one per download slot. Nothing waits while nothing runs (queued
// jobs then start at once).
func (p *prefetcher) next() string {
	for link, at := range p.tried {
		if p.now().Sub(at) > ytdlp.InfoTTL {
			delete(p.tried, link)
		}
	}
	jobs := p.jobs()
	running := 0
	for _, j := range jobs {
		if isRunning(j.Status) {
			running++
		}
	}
	if running == 0 {
		return ""
	}
	ahead := p.slots()
	for _, j := range jobs {
		if ahead <= 0 {
			break
		}
		if j.Status != queue.StatusQueued || j.RetryAt.After(p.now()) {
			continue
		}
		ahead--
		_, tried := p.tried[j.URL]
		if !tried && ytdlp.IsYouTubeVideo(j.URL) && !j.Options.WaitForVideo && !j.Options.LiveFromStart && !p.has(j.URL) {
			return j.URL
		}
	}
	return ""
}

// waiting reports whether link's job is still queued.
func (p *prefetcher) waiting(link string) bool {
	for _, j := range p.jobs() {
		if j.URL == link && j.Status == queue.StatusQueued {
			return true
		}
	}
	return false
}

func isRunning(s queue.Status) bool {
	return s == queue.StatusStarting || s == queue.StatusDownloading || s == queue.StatusProcessing
}

// analyseAhead analyses a queued video for prefetcher and keeps the result for its download.
func (a *App) analyseAhead(ctx context.Context, link string) {
	env, cleanup, err := a.buildEnv(ctx)
	if err != nil {
		return
	}
	defer cleanup()
	if res, err := ytdlp.Probe(ctx, env, link, true); err == nil {
		a.infos.Put(link, res)
	}
}
