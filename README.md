# Prem-dl

Prem-dl is a Windows desktop app for downloading video and audio with
[yt-dlp](https://github.com/yt-dlp/yt-dlp). Paste a link, check what you're about to get, pick a
quality and hit Download. It handles the parts of yt-dlp that are fiddly on the command line:
playlists, queues, cookies, subtitles and keeping yt-dlp itself up to date.

It also knows about YouTube Premium. Sign in through Chrome once and it downloads the higher
bitrate "Premium" streams where YouTube offers them, and tells you when it will.

## What it does

- **Look before you download.** Every link is analysed first, so you see the title, channel, length,
  available resolutions and subtitle languages before anything is saved.
- **Playlists and channels.** You get a checklist of every video with select all, invert, a number
  range and a title filter. A channel's Playlists tab lists its playlists so you can open the one you
  want. Downloads can go into a folder per playlist, numbered in order.
- **Lots of links at once.** Paste several links, one per line, and they're all queued. Playlists are
  expanded for you.
- **Quality.** Best, 4K down to 360p, audio only (MP3, M4A, Opus, AAC, FLAC or WAV), or pick an
  exact format from the list. Container can be automatic, MP4, MKV or WebM. Within the same
  resolution, Best goes for the stream with the most data rather than the newest codec.
- **Subtitles.** Save them as files, embed them in the video, or both, in any language, with or
  without auto-generated captions, and optionally converted to SRT, VTT or ASS.
- **Extras.** Embed metadata, chapters and cover art, cut out SponsorBlock segments, or download just
  part of a video by start and end time.
- **A real queue.** Several downloads at once, with pause, resume, cancel and retry, and a log for
  each one. The queue survives a restart; anything that was running comes back paused.
- **Errors you can act on.** When yt-dlp fails, you get a plain explanation and, usually, a button for
  the fix: update yt-dlp, open the cookie settings, and so on.
- **Stays current.** yt-dlp is checked for updates on launch and every few hours. New releases are
  verified against their published SHA-256 checksums before they replace the old one, and updates
  wait until nothing is downloading.

## Getting started

1. Download `Prem-dl.exe` from the [latest release](https://github.com/aervonix/prem-dl/releases/latest)
   and put it in a folder of its own. (Or build it yourself, see below.)

   The exe isn't code-signed, so Windows SmartScreen may say it's from an unknown publisher the
   first time. Click **More info**, then **Run anyway**. The release notes list its SHA-256 if you
   want to check the file first.
2. Start it. If yt-dlp isn't found, the app offers to download it for you. You can also drop
   `yt-dlp.exe` next to `Prem-dl.exe` yourself.
3. Install ffmpeg if you don't have it. Without it, video and audio can't be merged, so you'd
   only get lower quality single-file downloads. The easiest way is:

   ```powershell
   winget install Gyan.FFmpeg
   ```

4. YouTube also needs a JavaScript runtime for some videos. If you have Node 20 or newer, Deno,
   Bun or QuickJS installed, the app finds it and passes it to yt-dlp.

Your downloads go to your Downloads folder unless you change it in Settings. Each download can
also go somewhere else via the "Save to" field under More options.

## YouTube Premium and cookies

To download as yourself (Premium quality, age-restricted videos, or to get past YouTube's
"confirm you're not a bot" check), yt-dlp needs your YouTube cookies.

yt-dlp can't read Chrome's cookies on Windows anymore, because Chrome encrypts them in a way only
Chrome can undo. Prem-dl gets around this with a small helper extension that asks Chrome for them
instead. You set it up once:

1. In Prem-dl, go to **Settings → Cookies** and open **Set up once: add the helper to Chrome**.
2. Click **Show the helper folder**, then **Open chrome://extensions**.
3. In Chrome, turn on **Developer mode** (top right), click **Load unpacked** and choose that folder.
   Use the Chrome profile you're signed in to YouTube with.

After that, click **Get cookies from Chrome** whenever you need fresh cookies. Chrome opens a page
asking whether to share your YouTube cookies; click Allow and approve Chrome's own prompt.

A few things worth knowing:

- The helper only reads youtube.com cookies, only after you click Allow, and only sends them to
  Prem-dl on your own computer. Chrome's access is removed again straight afterwards, so it asks
  you every time.
- The cookies are a key to your signed-in YouTube session. Prem-dl keeps them in its own data
  folder and gives each download a temporary copy.
- YouTube rotates login cookies, so they stop working after a while. When downloads start asking
  you to sign in again, just get them again.
- yt-dlp's own documentation warns that heavy downloading with account cookies can get an account
  flagged. Go easy on very large playlists.

If you'd rather not use the extension, you can export a `cookies.txt` file with any "cookies.txt"
browser extension and import it, or read cookies straight from Firefox, which still works.

When Best is going to download a Premium stream, the quality picker shows a small "Premium" label
above it. YouTube sometimes serves Premium streams without signing in too, so you may see the
label even with cookies turned off.

## Where things are kept

Settings, the queue, imported cookies and the app's own copy of yt-dlp live in
`%APPDATA%\yt-dlp-gui` (the folder kept its old name so existing setups carry over). Settings →
Advanced has a button that opens it.

yt-dlp is looked for in this order: the path set in Settings → Advanced, next to `Prem-dl.exe`, the
folder the app was started from, the app's own copy in `%APPDATA%\yt-dlp-gui\bin`, and finally
`PATH`.

## Building from source

You'll need:

- Go 1.25 or newer
- Node 20 or newer
- The Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- The WebView2 runtime, which Windows 11 already has

Then:

```powershell
git clone https://github.com/aervonix/prem-dl.git
cd prem-dl
wails build        # builds build\bin\Prem-dl.exe
wails dev          # runs it with live reload while you work on it
```

Run the tests with:

```powershell
go test ./...                    # unit tests, no network needed
cd frontend; npx tsc --noEmit    # type-check the frontend
```

There are also a few tests that use the real yt-dlp and the internet. They're off by default:

```powershell
$env:YTGUI_INTEGRATION = "1"; $env:YTGUI_YTDLP = "C:\path\to\yt-dlp.exe"
go test ./internal/queue ./internal/ytdlp -run Integration -v
```

If you change a Go type that the frontend uses, run `wails generate module` afterwards.
`frontend/src/api.ts` has compile-time checks that fail `tsc` when the TypeScript types and the
Go types drift apart, so you'll notice if you forget.

## How it's put together

The backend is Go and the interface is React with TypeScript, joined by
[Wails v2](https://wails.io). The Go side does the real work; the interface only shows state and
sends requests.

```
main.go, app.go, tools.go   app shell and the methods the interface can call
chrome.go, chromehelper/    the Chrome cookie helper and the extension itself
internal/cookiebridge       one-time local handover of cookies from the extension
internal/ytdlp              finding, installing and updating yt-dlp, analysing links,
                            building arguments, reading progress, cookie files
internal/queue              scheduling, process control, saving the queue, UI events
internal/apperr             turning yt-dlp output into errors with a suggested fix
internal/settings           settings and per-download options, validation, safe saves
internal/sys                Windows process handling, opening files and folders
frontend/src                the interface (api.ts is the only file that talks to Go)
```

## Limitations

- Windows only. That's what it's built and tested on.
- Premium streams depend on what YouTube offers for a given video and account, which can change.
- It's only as good as yt-dlp. If a site changes and downloads break, updating yt-dlp (Settings →
  Updates & tools) usually fixes it.

## Licence

MIT. See [LICENSE](LICENSE).

Prem-dl isn't affiliated with YouTube, Google or the yt-dlp project. Please only download things
you have the right to.
