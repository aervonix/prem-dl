# Prem-dl

A desktop app for [yt-dlp](https://github.com/yt-dlp/yt-dlp). The backend is **Go**, the frontend is
**React + TypeScript**, and the two are joined with [Wails v2](https://wails.io) (WebView2 on Windows).

It is not a thin wrapper around a command line: the Go side owns the download queue, parses yt-dlp's
structured output, classifies failures, manages cookies and keeps the yt-dlp binary up to date.

## Features

- **Analyze before you download** – thumbnail, uploader, duration, available resolutions and subtitle tracks.
- **Playlists & channels** – checklist with select all / none / invert, numeric range, title filter, nested
  channel tabs flattened, unavailable entries greyed out. "Video inside a playlist" links ask which you meant.
  Optional per-playlist folder and `001 - Title` numbering.
- **Batch paste** – one link per line; playlists are expanded and queued automatically.
- **Quality** – Best / 4K / 1440p … 360p / audio-only (MP3, M4A, Opus, AAC, FLAC, WAV) / pick an exact format
  or type a `-f` expression. Container: auto, MP4, MKV, WebM.
- **Subtitles** – off, save as file, embed, or both; any language, auto-generated captions, convert to SRT/VTT/ASS.
- **Extras** – embed metadata / chapters / cover art, save thumbnail/description/info JSON, SponsorBlock
  segment removal, download only a time range.
- **Queue** – parallel downloads, pause/resume (partial files are kept), cancel, retry, per-job log, one overall
  progress bar across video+audio streams, persisted across restarts (interrupted jobs come back paused).
- **Error handling** – yt-dlp failures are turned into a plain explanation plus a button that does the likely fix
  (update yt-dlp, open cookie settings, …). Covers bot checks, age gates, private/members-only, geo-blocks,
  rate limits, missing ffmpeg / JS runtime, network, disk and permission problems, invalid formats and yt-dlp crashes.
- **Cookies** – from a browser, or import / paste a `cookies.txt` (validated; only domains and counts are ever shown).
  Each download gets its own private copy because yt-dlp rewrites the cookie file on exit. A "Test" button checks
  the source without sending cookies anywhere.
- **Auto-update** – checks shortly after launch and every few hours, downloads the release for your channel
  (stable / nightly / master), **verifies its SHA-256 against the release checksums**, runs it once, then swaps it in.
  Waits while downloads are running. Also installs yt-dlp from scratch if it is missing.
- **Tool detection** – finds ffmpeg and a JavaScript runtime (Deno / Node 20+ / Bun / QuickJS), which YouTube now requires.

## Cookie caveat on Windows

Recent Chrome / Edge / Brave builds encrypt cookies in a way yt-dlp cannot read
(`Failed to decrypt with DPAPI`, [yt-dlp#10927](https://github.com/yt-dlp/yt-dlp/issues/10927)). This is a yt-dlp
limitation, not an app bug. Use **Firefox** as the cookie source, or export a `cookies.txt` and import it.

## Layout

```
main.go, app.go, tools.go   Wails shell + the methods the frontend can call
internal/apperr             yt-dlp output  ->  structured, actionable errors
internal/ytdlp              binary discovery/install/update, probing, argument building,
                            output parsing + progress tracking, cookie handling
internal/queue              scheduling, process control, persistence, UI events
internal/settings           settings + per-download options, validation, atomic saves
internal/sys                Windows process-tree kill, hidden consoles, open/reveal
frontend/src                React + TS (api.ts is the only file that talks to Go)
```

`frontend/src/api.ts` contains compile-time guards that fail `tsc` if the hand-written TS types drift from
the Go structs, so run `wails generate module` after changing a Go struct.

## Develop

Requirements: Go 1.25+, Node 20+, the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
and the WebView2 runtime (already on Windows 11).

```powershell
wails dev          # live reload; the bridge is also at http://localhost:34115 for browser debugging
wails build        # -> build\bin\Prem-dl.exe
go test ./...      # unit tests (no network)
```

Opt-in tests that use the real yt-dlp and the internet:

```powershell
$env:YTGUI_INTEGRATION = "1"; $env:YTGUI_YTDLP = "C:\path\to\yt-dlp.exe"
go test ./internal/queue ./internal/ytdlp -run Integration -v
```

yt-dlp is looked up in this order: the path in Settings → Advanced, next to the app, the working folder, the
app's own copy (`%APPDATA%\yt-dlp-gui\bin`), then `PATH`. Settings and the queue live in `%APPDATA%\yt-dlp-gui`.
