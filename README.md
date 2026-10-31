# Prem-dl

Prem-dl is a desktop app for Windows and Linux for downloading video and audio with
[yt-dlp](https://github.com/yt-dlp/yt-dlp). Paste a link, check what you're about to get, pick a
quality and hit Download. It handles the parts of yt-dlp that are fiddly on the command line:
playlists, queues, cookies and subtitles. yt-dlp and ffmpeg are built in, and each release of
Prem-dl carries the versions it was tested with.

It also knows about YouTube Premium. Sign in through Chrome once and it downloads the higher
bitrate "Premium" streams where YouTube offers them, and tells you when it will.

## What it does

- **Look before you download.** Every link is analysed first, so you see the title, channel, length,
  available resolutions and subtitle languages before anything is saved.
- **Playlists and channels.** Every video shows as a thumbnail grid, the way YouTube lays out a
  channel, with a checkbox on each (Shift+click ticks a run); a compact list is one click away.
  There's select all, invert, a number range and a title filter. A channel's Playlists tab lists its playlists so you can open the one you
  want. Downloads can go into a folder per playlist, numbered in order.
- **Follow channels and playlists.** Follow a YouTube channel (its Videos, Shorts or Live tabs) or
  any playlist, and Prem-dl checks it every few hours and queues new uploads with the options you
  chose, even from the tray. What's already there isn't downloaded; streams wait until they've
  ended, and videos you downloaded before are skipped when "Remember downloaded videos" is on.
  Open a followed channel to browse its Videos, Shorts and Live tabs and tick older videos to
  download; ones already downloaded or in the queue are marked. Filters skip uploads by length or
  title words, and a channel can record its live streams as they happen instead of waiting for
  the replay. Import your YouTube subscriptions (Google Takeout's `subscriptions.csv`), or move your
  follows to another computer with Export and Import.
- **Search YouTube.** Search from inside Prem-dl and the results show as the same thumbnail grid,
  with filters for type (videos, channels, playlists), upload date and length. Tick videos to
  download them with your default options, or open them on the Download page to choose. Ticks are
  kept across searches, so you can collect videos from several.
- **Lots of links at once.** Paste several links, one per line, and they're all queued. Playlists are
  expanded for you. You can also drop links, or a `.txt` file of them, anywhere on the window.
- **Send links from Chrome.** With the helper extension, click its toolbar button, or right-click a
  page or a link: **Open in Prem-dl** to review it, **Download now** (or **Download as** one of your
  presets) to queue it straight away, or **Follow this channel or playlist** on YouTube. Prem-dl
  starts first if it isn't running; Download now then opens the link for review, since any web
  page can start Prem-dl that way. A playlist or channel sent with Download now opens for review
  too, so nothing big is queued by surprise.
- **Quality.** Best, 4K down to 360p, each with about how big the file will be, or audio only
  (MP3, M4A, Opus, AAC, FLAC, WAV or the original stream). Or pick an exact format from a table of
  resolution, codec, bitrate and size. Container can be automatic, MP4, MKV or WebM. Within the
  same resolution, Best goes for the stream with the most data rather than the newest codec.
  "Most compatible" picks H.264 and AAC, for editors, TVs and older phones (1080p at most on
  YouTube).
- **Subtitles.** Save them as files, embed them in the video, or both, in any language, with or
  without auto-generated captions, and optionally converted to SRT, VTT or ASS.
- **Extras.** Embed metadata, chapters and cover art (cropped square for music), cut out
  SponsorBlock segments, or download just part of a video by start and end time. A playlist can get
  a `.m3u8` playlist file, and its audio the playlist as album with track numbers.
- **File names.** One template in Settings, and presets (or a single download) can have their
  own: a podcasts preset dated, an archive one with the video ID.
- **Presets.** Save option sets under a name, like "Music" (MP3 with cover art) or "Archive" (MKV,
  subtitles, info JSON), and pick one on the Download page. Tie a preset to a site and its links pick
  it by themselves: music.youtube.com links start as Music.
- **A real queue.** Several downloads at once, with pause, resume, cancel and retry, and a log for
  each one. Tick several to act on them together. The queue survives a restart; anything that was
  running comes back paused. A download that won't fit on the drive stops before it starts, and
  downloads can be limited to certain hours (for a data cap that's free at night).
- **Works in the background.** Progress shows on the taskbar button, and a Windows notification
  (with Open and Show in folder) tells you when downloads finish while you're in another app. It can
  keep running in the notification area when you close the window, and start there with Windows.
- **Errors you can act on.** When a download fails, you get a plain explanation and, usually, a
  button for the fix: check for a new Prem-dl, open the cookie settings, and so on.
- **Changes only with a release.** Prem-dl runs only the yt-dlp and ffmpeg built into it and never
  updates them by itself, so the app you have keeps behaving the same until you update it. It tells
  you when a new version is out; a copy installed with Prem-dl-Setup.exe downloads the new
  installer, checks it and restarts updated. Fixes for sites that changed come that way.

## Getting started

Each [release](https://github.com/aervonix/prem-dl/releases/latest) has two downloads. Both have
yt-dlp and ffmpeg built in, so there's nothing else to get.

- **`Prem-dl-Setup.exe`** (about 110 MB) installs Prem-dl just for you, without asking for admin
  rights, into `%LOCALAPPDATA%\Programs\Prem-dl`, with a Start menu and desktop shortcut. It can
  update itself. Uninstall it from Settings → Apps; it asks whether to keep your settings and queue.
- **`Prem-dl.exe`** (about 118 MB) is the app on its own, with nothing to install. Put it in a
  folder of its own and start it. To update it, download the new one from the release page.

On Linux (Ubuntu 22.04 or newer, Debian 12 or newer, and others with WebKitGTK 4.1), the
release has two more:

- **`prem-dl_<version>_amd64.deb`** installs Prem-dl for Debian and Ubuntu:
  `sudo apt install ./prem-dl_<version>_amd64.deb`. It brings in what it needs.
- **`Prem-dl-x86_64.AppImage`** is the app on its own. Make it executable (`chmod +x`) and start
  it; it needs the system's WebKitGTK (`sudo apt install libwebkit2gtk-4.1-0` on Ubuntu). On its
  first start it adds itself to the app menu and to `prem-dl:` links.

The Linux files are listed in `SHA256SUMS-linux.txt`. Linux copies don't update themselves: when
a new version is out, Prem-dl says so and opens its release page.

Neither Windows file is code-signed, so Windows SmartScreen may say it's from an unknown publisher the first
time. Click **More info**, then **Run anyway**. The release notes list each file's SHA-256 if you
want to check it first. (Or build it yourself, see below.)

YouTube also needs a JavaScript runtime for some videos. If you have Node 20 or newer, Deno, Bun
or QuickJS installed, the app finds it and uses it.

Your downloads go to your Downloads folder unless you change it in Settings. Each download can
also go somewhere else via the "Save to" field under More options.

On Linux, the tray icon needs a desktop with a tray. Ubuntu has one; on plain GNOME, turn on the
AppIndicator extension. Without one, closing the window quits Prem-dl.

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

The same helper puts Prem-dl on Chrome's toolbar and right-click menu. If you added it before
the menu had Download now and Follow, click the reload arrow on it in
`chrome://extensions` once.

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

On Linux, Prem-dl reads cookies straight from Chrome and other Chrome-based browsers as well
(Settings → Cookies → From a browser), so the helper is optional there. It lists the browsers it
finds, including ones yt-dlp doesn't know by name, such as Helium or a Flatpak Chrome. The helper
still adds **Download with Prem-dl** to the browser, and works in whichever Chrome-based browser
you pick.

When Best is going to download a Premium stream, the quality picker shows a small "Premium" label
above it. YouTube sometimes serves Premium streams without signing in too, so you may see the
label even with cookies turned off.

## Running in the background

Settings → General has the options for this:

- **Keep running when the window is closed.** Closing the window leaves Prem-dl in the notification
  area (by the clock), where downloads carry on. Click the icon to bring the window back, or
  right-click it to pause, resume or quit. Even with this off, closing while downloads run offers to
  let them finish in the background.
- **Start with Windows.** Prem-dl starts in the notification area when you sign in, without opening
  the window.
- **Windows notifications.** Shown when downloads finish while Prem-dl isn't the app you're in.
- **Keep the computer awake while downloading.** It doesn't go to sleep until the downloads are
  done (the screen can still turn off, and closing a laptop's lid still sleeps).

On first start Prem-dl registers `prem-dl:` links for your Windows account (no admin rights needed),
which is how the Chrome helper starts it with a link. If you move `Prem-dl.exe`, starting it once
from the new place updates that.

## Where things are kept

Settings, the queue, imported cookies and the unpacked tools live in `%APPDATA%\yt-dlp-gui`
(`~/.config/yt-dlp-gui` on Linux; the folder kept its old name so existing setups carry over). Settings → Advanced has a button that
opens it.

Windows can't run a program from inside another, so on start Prem-dl unpacks its built-in yt-dlp
and ffmpeg into `%APPDATA%\yt-dlp-gui\bin`, replacing whatever version was there before, and runs
only those. Any other yt-dlp or ffmpeg on the computer is ignored. If the files go missing or stop
running (antivirus, a clean-up tool), **Repair** in Settings → Updates unpacks them again.

A build without the tools inside (plain `wails build`, `wails dev`) looks for yt-dlp next to
`Prem-dl.exe`, in the folder it was started from, in `%APPDATA%\yt-dlp-gui\bin` and on `PATH`,
and for ffmpeg next to yt-dlp, there and on `PATH`.

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
wails build        # builds build\bin\Prem-dl.exe without tools; put yt-dlp.exe next to it
wails dev          # runs it with live reload while you work on it
```

To build the exe the way releases are, with yt-dlp and ffmpeg inside:

```powershell
go run ./cmd/bundle               # fetches both into bundle\, checked against their SHA-256 sums
wails build -tags bundled
```

`go run ./cmd/bundle -ffmpeg=false` leaves ffmpeg out (about 31 MB); that build then looks for an
ffmpeg on the computer.

To make both release files, you also need NSIS (`winget install NSIS.NSIS`). Then:

```powershell
.\scripts\release.ps1   # build\release: Prem-dl.exe, Prem-dl-Setup.exe and SHA256SUMS.txt
```

It fetches the newest yt-dlp and ffmpeg first (`-SkipFetch` reuses `bundle\`), checks the
built-in tools unpack, then builds both files with them inside. That fetch is the only time
yt-dlp's version changes. The installer is `build\windows\installer\project.nsi`.

On Linux (Ubuntu 24.04 or newer) you'll also need
`sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev`, and every
`wails` command takes `-tags webkit2_41`:

```bash
wails dev -tags webkit2_41
go run ./cmd/bundle && wails build -tags "webkit2_41 bundled" -trimpath
scripts/release.sh    # build/release: the .deb, the AppImage and SHA256SUMS-linux.txt
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
internal/ytdlp              finding and fetching yt-dlp, analysing links,
                            building arguments, reading progress, cookie files
internal/queue              scheduling, process control, saving the queue, UI events
internal/apperr             turning yt-dlp output into errors with a suggested fix
internal/settings           settings and per-download options, validation, safe saves
internal/sys                processes, files and folders, tray, notifications, sign-in start,
                            link registration (Windows registry, Linux desktop entries)
frontend/src                the interface (api.ts is the only file that talks to Go)
```

## Limitations

- Windows and Linux (x86-64) only. Linux is tested on Ubuntu with GNOME.
- Premium streams depend on what YouTube offers for a given video and account, which can change.
- It's only as good as yt-dlp. If a site changes and downloads break, the fix arrives with the next
  Prem-dl release that carries a newer yt-dlp.

## Licence

MIT. See [LICENSE](LICENSE).

Prem-dl isn't affiliated with YouTube, Google or the yt-dlp project. Please only download things
you have the right to.
