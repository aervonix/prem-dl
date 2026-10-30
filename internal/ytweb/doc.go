// Package ytweb reads YouTube's web pages and web API itself, without starting yt-dlp, for
// what doesn't need yt-dlp: searches (search.go) and listings of channel tabs and playlists
// (browse.go). yt-dlp spends most of the time those take starting up and fetching pages it
// then discards.
//
// What it returns is the JSON yt-dlp would print for the same pages (parse.go mirrors yt-dlp's
// YouTube extractors), read by ytdlp.ParseProbe like yt-dlp's own; testdata holds real pages
// next to yt-dlp's output for them. When anything here fails, callers use yt-dlp instead.
package ytweb
