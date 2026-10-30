# Prem-dl

Was "yt-dlp GUI"; internal names (Go module `yt-dlp-gui`, data folder `%APPDATA%\yt-dlp-gui`,
single-instance ID, Chrome helper extension ID) keep the old name on purpose, so existing data
and the loaded helper keep working.

Windows desktop app for yt-dlp. **Go** backend, **React + TypeScript** frontend, joined by **Wails v2**
(WebView2). The Go side owns the queue, output parsing, error classification, cookies and the built-in tools;
the UI is not a thin wrapper over a shell command. See `README.md` for the feature list.

## Commands

```powershell
wails dev                 # live reload; bridge also at http://localhost:34115 for browser debugging
wails build               # -> build\bin\Prem-dl.exe (copy yt-dlp.exe next to it)
go run ./cmd/bundle; wails build -tags bundled   # one exe with yt-dlp + ffmpeg inside (bundle.go)
go test -tags bundled . -run BuiltIn             # the embedded tools unpack and run
.\scripts\release.ps1     # build\release: Prem-dl.exe + Prem-dl-Setup.exe (NSIS), both bundled, + SHA256SUMS
go test ./...             # offline unit tests (~10 s)
go vet ./...; gofmt -l app.go main.go tools.go internal
cd frontend; npx tsc --noEmit; npm test             # vitest: format.ts, store.tsx reducers + provider
wails generate module     # REQUIRED after changing any Go struct that crosses to the UI
# opt-in tests against real yt-dlp + internet:
$env:YTGUI_INTEGRATION="1"; $env:YTGUI_YTDLP="C:\Code\yt-dlp-gui\yt-dlp.exe"
go test ./internal/queue ./internal/ytdlp -run Integration -v
```

## Layout

- `main.go`, `app.go`, `tools.go` – Wails shell; every exported `App` method is callable from TS.
- `internal/apperr` – raw yt-dlp output -> `AppError` (code, title, message, hint, action button).
- `internal/ytdlp` – binary resolve/fetch (used by `cmd/bundle`), probe, `args.go` (option -> flags), `progress.go`
  (line parser + multi-stream `Tracker`), cookie file inspect/import.
- `internal/queue` – scheduler, process control, persistence (`queue.json`), batched UI events.
- `internal/follow` – followed channels/playlists (`feeds.json`): baseline of what's there, periodic
  checks that queue unseen video IDs. Deps are injected like `queue`; `following.go` wires them.
- `bundle.go` (+ `bundle_embed.go` under `-tags bundled`) – unpacks the yt-dlp/ffmpeg built into
  the exe into the managed `bin` folder during the first tool detection; `cmd/bundle` fetches them.
  **yt-dlp and ffmpeg change only with a Prem-dl release** (user's decision): no in-app update,
  install or channel, no custom tool paths, and a bundled build runs only its own copies
  (`findYtdlp`/`findFFmpeg`). `bin\bundle.stamp` records which bundle is unpacked; a different one
  replaces it, newer or older. `RepairTools` unpacks again. The main UI doesn't name yt-dlp (one
  version line in Settings → Updates is for bug reports); errors say "Prem-dl" and `ActionUpdate`
  means "check for a new Prem-dl".
- `search.go` + `internal/ytdlp/search.go` – YouTube search: a `youtube.com/results` link read like a
  playlist (`--playlist-items=N:M` pages it). Results are `PlaylistEntry`s shown by `pages/SearchPage.tsx`
  in the shared `VideoGrid`.
- `internal/settings` – `Settings` + `DownloadOptions`, validation, atomic writes. Data lives in `%APPDATA%\yt-dlp-gui`.
- `internal/sys` – hidden consoles, process-tree kill (`taskkill /T`), open/reveal in Explorer, the tray
  icon + taskbar progress (`shell_windows.go`, raw Win32/COM on its own thread), per-user registry entries.
- `internal/linkbridge` – loopback endpoint the Chrome helper posts links to (only its origin accepted).
- `background.go`, `activity.go`, `launch.go` – tray, close-to-tray, notifications, start with Windows,
  `prem-dl:` links and `--background`; `activity` turns queue changes into taskbar progress and toasts.
- `chromehelper/` – the unpacked extension (cookies, toolbar button, right-click menu); embedded in the exe.
- `frontend/src` – `api.ts` is the ONLY file that calls Go; `types.ts` mirrors the Go JSON by hand;
  `store.tsx` is the reducer fed by backend events; `pages/`, `components/`, one `styles.css`.
  `pages/settings/` holds one component per Settings tab (all stay mounted, hidden when not shown);
  `SettingsPage.tsx` owns the draft and the save bar. `components/QualityPicker.tsx` is the Quality
  section; `quality.ts` mirrors yt-dlp's stream choice (`QualitySort`) to estimate each preset's size.
  Tests sit next to their file (`*.test.ts[x]`) and mock `./api` (see `store.test.tsx`).

## Conventions

- Git: commit straight to `main`. Only make a branch for big changes.
- Errors users can act on are `*apperr.AppError` with an `Action` (update / cookies / settings / ...),
  not strings. Add a rule + test in `apperr` when you meet a new yt-dlp failure; use real captured messages.
- `api.ts` has compile-time guards that fail `tsc` if `types.ts` drifts from the generated models. Keep them
  (they were mutation-tested). `time.Time` fields need `ts_type:"string"` on the Go tag.
- Free-form values go to yt-dlp as `--opt=value` and URLs after `--`, so input can never become a flag.
  URLs pass `ytdlp.NormalizeURL` (http/https only).
- Plain CSS with tokens in `:root`; light/dark/system. No UI libraries; icons are inline SVG in `icons.tsx`.

## Gotchas (each one bit us)

**yt-dlp behaviour**
- Filter syntax is `[height<=?720]` (the `?` goes after the operator). Unit tests only compared strings
  and missed this; the integration test caught it.
- Embed-only subtitles must NOT pass `--write-subs`, or yt-dlp keeps the sidecar files. Exception:
  with auto captions on, `--embed-subs --write-auto-subs` embeds the AUTO captions even where uploaded
  subtitles exist; pass `--write-subs --compat-options=no-keep-subs` (sidecars still get deleted).
- WebM can't hold h264 (ffmpeg fails) or a thumbnail (`Supported filetypes for thumbnail embedding...`).
  We pass `webm/mkv` + `webm>webm/mkv` and skip `--embed-thumbnail` for WebM.
- `--download-sections` fetches all streams in ONE ffmpeg run into the final file (no `.fNNN` parts)
  and reports a single `finished` event sized for the cut, not the plan's full size (`Tracker.Cut`).
- Several `--download-sections` are separate downloads. As time ranges (`*0-67`) they get the same
  name and only the first is kept; picked chapters go by title regex (`^Title$`), which fills
  `section_title`/`section_number`. That number counts from 0 there (1 for `--split-chapters`).
- yt-dlp's default ranking prefers newer codecs before bitrate. Signed in, YouTube offers two
  "1080p Premium" streams (VP9 ~2.8 Mbps, AV1 ~0.4 Mbps) and the default picked AV1. Quality presets
  and the probe pass `--format-sort=res,fps,hdr:12,vbr` (`ytdlp.QualitySort`); keep them in step.
  The probe's default choice (`requested_formats`) is what "Best" downloads; `bestPremium` reads it.
- A channel's Playlists tab lists playlists as flat `_type: url` entries (`ie_key: YoutubeTab`). Queued
  as a video, one job downloads the whole playlist; they are flagged `isPlaylist`.
- `--print` implies `--quiet`; we pass `--no-quiet` so "already downloaded" lines are visible.
- Post-processors can run BEFORE the media downloads (subtitle conversion). Only show 100% once
  `Tracker.AllDone()`. Subtitle/thumbnail files emit their own progress events; they are ignored.
- `--cookies <missing file>` is silently ignored, so cookie files are validated by us. Each run gets a
  private copy because yt-dlp rewrites the jar on exit.
- Chrome/Edge cookies fail on Windows (`Failed to decrypt with DPAPI`, yt-dlp#10927). Firefox or cookies.txt work.
- YouTube needs a JS runtime; only deno is on by default, so detected node/bun/quickjs are passed via `--js-runtimes`.
- A `null` playlist entry unmarshals into a zeroed struct without error; check for it explicitly.
- Channel tabs (`/videos`, `/shorts`, `/streams`) list newest first and `--playlist-end` applies per
  nested tab, so follow checks read the top 50; playlists append at the end and are read in full.
  Flat entries carry `live_status`; upcoming/live ones aren't marked seen so they're queued once over.
- A scheduled stream fails `-J` with `This live event will begin in 3 days.`; `Probe` reads it again with
  `--ignore-no-formats-error` (title, `release_timestamp`) so it can be waited for. `--wait-for-video`
  prints `[wait] Waiting for 77:19:39 - ...` once per wait, then a `\r` countdown every second (dropped).
- A missing tab is `This channel does not have a shorts tab` (`no_tab`, skipped when other tabs are
  checked). A wrong/deleted channel says `Playlists that require authentication may not extract
  correctly...`, not 404 (`page_unreadable`, matched before the generic sign-in rule).
- Search filters go in the results link's `sp` parameter, a protobuf `{2: {1: date, 2: type, 3: length}}`
  (`ytdlp.SearchURL`). YouTube still honours those, but ignores its sort orders (date, views) since
  its 2025 filter redesign, so there is no sort. Results say only "3 years ago";
  `--extractor-args=youtubetab:approximate_date` turns that into `timestamp`. Channel avatars come
  as `//yt3.ggpht.com/...` (`pickThumb` adds `https:`).
- The default file name is just the title (user's choice; was `Title [id]`). Two videos with one
  title get one name, and yt-dlp skips the second as "already downloaded". Chapter folders follow
  the filename template (`videoFolder`). Saved settings keep whatever template they had.
- Quality sizes in the UI are estimates from the probe's `filesize`/`filesize_approx`: `quality.ts`
  picks streams like `bv*[height<=h]+ba` under `QualitySort`. Change one, change the other.

**Wails / Go**
- Wails runtime helpers (`ClipboardGetText`, dialogs, ...) call `log.Fatal` on a nil/bad context, killing
  the app. Guard with `a.ready()`; the context is an atomic pointer set in `startup`.
- Wails' Windows clipboard calls must not overlap: two at once crashed the process with
  `0xC0000374` (heap corruption, no log). StrictMode's double effect made `wails dev` die ~1 s after
  start. `ReadClipboard`/`CopyText` hold `App.clipMu`.
- Single-instance lock: `wails dev` exits immediately if the packaged app is running, and `wails build`
  cannot overwrite a running exe. Never kill the user's running app; ask them to close it.
- Dev builds (`-tags dev`, `devBuild`) never write the `prem-dl:` link type or the Run entry, so a dev exe
  can't take over from the real one. Test those with a built exe, and the registry code with scratch keys.
- Wails' notifications register the app under the exe's file name; we write `DisplayName` first or toasts
  say "Prem-dl.exe". Toasts always play Windows' sound, so the page skips its click while unfocused.
- Wails v2 has no tray. `sys.Shell` owns it: a hidden top-level window (not message-only, or Explorer's
  `TaskbarCreated` broadcast is missed) on a locked thread. Its callbacks run on new goroutines because
  Wails runtime calls from the shell thread could block it.
- `go:embed bundle` fails to compile without the files, so it lives behind the `bundled` tag; plain
  builds and tests never need `bundle/`. Don't launch a release build to test it: it re-registers
  the `prem-dl:` link type and toast name for its own path. Use the `-tags bundled` test instead.
- The installer is per-user (`project.nsi`; `wails_tools.nsh` is regenerated, don't edit it). Its
  uninstaller deletes `%APPDATA%\Prem-dl.exe` (WebView2 data, shared with any other copy named
  Prem-dl.exe): back it up before test-uninstalling on a dev machine, and use `uninstall.exe /S
  _?=<dir>` so it runs synchronously.
- Prem-dl's own update (`app_update.go`, `internal/selfupdate`) reads GitHub's `releases/latest`
  anonymously, so it only works while the repo is public; a private repo answers 404. Installed
  copies (found by the uninstall entry's `uninstall.exe` next to the exe) run the new setup as
  `/update /D=<dir>`: it skips its pages, waits for the app to close and starts it again.
  Try the flow in dev with `$env:PREMDL_UPDATE_API` pointing at a fake API (dev builds only).
- Editing Go files while `wails dev` runs triggers rebuilds that drop the bridge; wait for port 34115.
- No C compiler on the Windows machine, so run `go test -race` on Linux (see below). Keep lock ordering simple
  (`lineHandler.mu` -> `Manager.mu` and `Manager.saveMu` -> `Manager.mu`, never the reverse).
  queue.json is encoded and written outside `Manager.mu`; don't move disk work back under it.

**Frontend / CSS**
- The scroll container needs `min-height: 0` on every flex/grid ancestor, or it grows instead of scrolling.
- Hidden checkbox inputs must be positioned inside their label, or focusing them scrolls the whole shell.
- `scrollIntoView` and the browser tool's ref-clicks can scroll `overflow:hidden` ancestors, so they
  hide scroll bugs. Verify scrolling with real wheel events.

## Linux

Being ported (Ubuntu, Wails on WebKitGTK). Until it's done, `internal/sys` uses the `_other.go`
stubs: no tray, autostart or `prem-dl:` links, and the tools still have Windows names.

```bash
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0     # keep in step with go.mod
wails dev -tags webkit2_41        # every wails command needs this tag (only webkit2gtk 4.1 exists)
wails build -tags webkit2_41      # -> build/bin/Prem-dl
go test ./...; go vet ./...; gofmt -l app.go main.go tools.go internal
GORACE=atexit_sleep_ms=0 go test -race ./...
cd frontend && npm ci && npx tsc --noEmit && npm test
YTGUI_INTEGRATION=1 YTGUI_YTDLP=/path/to/yt-dlp go test ./internal/queue ./internal/ytdlp -run Integration -v
```

- `wails doctor` asks for `libwebkit2gtk-4.0-dev`, which Ubuntu 24.04+ doesn't have; 4.1 plus the tag is right.
- `wails dev` logs `Dev server request rejected: Host "wails.localhost:34115"` once: Vite's HMR socket
  tries the page's host first, then connects to :5173 directly. Live reload still works; ignore it.
  `tray icon unavailable` is the `_other.go` stub.
- `GORACE=atexit_sleep_ms=0`: the queue tests run the test binary itself as a fake yt-dlp, and a race
  build sleeps 1 s on exit, so `TestPauseAfterYtdlpExitedStillCompletes` pauses before "yt-dlp" exits.
- Data folder: `~/.config/yt-dlp-gui` (`os.UserConfigDir`).

## Windows environment notes

- Go 1.27 and the Wails CLI were installed during setup. New shells need PATH refreshed:
  `$env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User') + ';' + $env:USERPROFILE + '\go\bin'`
- PowerShell does not expand `*.go` for native tools; list files explicitly.
- Test downloads must go to the scratchpad via the per-download "Save to" field, never `~\Downloads`.
  After UI tests, clear test jobs from the real queue (`ClearFinished`) and reset any cookie settings.
- Test videos: `dQw4w9WgXcQ` works; `jNQXAC9IeRw` is unavailable for this account (a handy error case).
