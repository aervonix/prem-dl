// Package tiktok reads a TikTok post (a video or a photo post) from its web page itself, without
// starting yt-dlp. The page's __UNIVERSAL_DATA_FOR_REHYDRATION__ script carries everything
// yt-dlp reads from it: the caption, the author, the sound and every rendition with its size.
// One request, ~0.4 s, against ~1.2 s for yt-dlp -J.
//
// What it returns is the JSON yt-dlp's TikTok extractor would (info.go mirrors yt-dlp
// 2026.10.26's _parse_aweme_video_web and _extract_web_formats, format IDs included), so
// ytdlp.ParseProbe reads it like yt-dlp's own and a download hands it back to yt-dlp with
// --load-info-json. Two things go further than yt-dlp:
//
//   - The player's own file ("play") gets its size from PlayAddrStruct. On edits it is often the
//     only file there is (20-80 MB, no list of renditions), and yt-dlp leaves its size unknown.
//   - Photo posts. yt-dlp rejects /photo/ links and reads only the sound from a /video/ link to
//     one. Here a photo post is a playlist of its sound and its pictures, with photo_count set
//     (see ytdlp.ParseProbe and ytdlp.Target.Photos for how it is downloaded).
//
// The rendition links answer 403 without the page's cookies and a tiktok.com Referer (either
// alone is refused), so every format carries both, as yt-dlp's own -J output does.
//
// When anything here fails (a post TikTok withholds, a private account, a page without the
// payload, a challenge page), callers use yt-dlp instead, which can also sign in.
//
// The mobile app's feed endpoint (/aweme/v1/feed/) isn't used: since 2026-08-31 it answers
// status 0 with a feed that rarely holds the post asked for.
package tiktok
