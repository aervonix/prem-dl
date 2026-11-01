package main

import (
	"context"
	"time"

	"yt-dlp-gui/internal/tiktok"
	"yt-dlp-gui/internal/ytdlp"
)

// A TikTok post (a video or a photo post) is read from its page here (internal/tiktok): ~0.4 s
// against ~1.2 s for yt-dlp -J, the player file's size, which yt-dlp leaves unknown, and photo
// posts, which yt-dlp can't download. The download still runs through yt-dlp, from that
// analysis (--load-info-json). yt-dlp reads the post itself when this can't: a post TikTok
// shows only to signed-in viewers, a private one, a challenge page, or extra yt-dlp arguments.

// readTikTok analyses a TikTok post without yt-dlp. ok is false when yt-dlp has to.
func (a *App) readTikTok(ctx context.Context, link string) (*ytdlp.ProbeResult, bool) {
	s := a.store.Get()
	if s.ExtraArgs != "" || !tiktok.Handles(link) {
		return nil, false
	}
	res, err := func() (*ytdlp.ProbeResult, error) {
		post, err := tiktok.Read(ctx, tiktok.Options{Proxy: s.Proxy, IPv4: s.ForceIPv4}, link)
		if err != nil {
			return nil, err
		}
		data, err := post.Info()
		if err != nil {
			return nil, err
		}
		return ytdlp.FromInfo(data)
	}()
	if err != nil {
		if ctx.Err() == nil {
			a.warn("reading the TikTok post without yt-dlp failed, so yt-dlp reads it instead: " + err.Error())
		}
		return nil, false
	}
	return res, true
}

// savedInfo is the queue's Info: the analysis made on the Download page, or for a TikTok post
// nobody analysed (or analysed over an hour ago) a fresh read of its page. A photo post can only
// download that way.
func (a *App) savedInfo(link string) (ytdlp.Saved, func()) {
	if saved, done := a.infos.Take(link); saved.Path != "" {
		return saved, done
	}
	if !tiktok.Handles(link) {
		return ytdlp.Saved{}, func() {}
	}
	ctx, cancel := context.WithTimeout(a.root, time.Minute)
	defer cancel()
	gen := a.infos.Gen()
	res, ok := a.readTikTok(ctx, link)
	if !ok {
		return ytdlp.Saved{}, func() {}
	}
	a.infos.PutFrom(gen, link, res)
	return a.infos.Take(link)
}
