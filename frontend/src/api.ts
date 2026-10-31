// The only module that talks to the Go backend. Everything else imports `api` from here.
import * as App from '../wailsjs/go/main/App'
import type {main, queue, settings, ytdlp, apperr, follow} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import type * as T from './types'

// ---- Compile-time drift guards -------------------------------------------------------------
// Each line fails to compile if the generated Go model has a field that our hand-written type
// lacks or declares with an incompatible type. Regenerate bindings (wails generate module) after
// changing Go structs, then fix whatever this reports.
// Generated classes carry a convertValues() helper and use plain `string` where we use unions,
// and `x?: T` where Go sends null; the helpers below look past those differences.
type Keys<X> = Exclude<keyof X, 'convertValues'>
type SameKeys<Gen, Mine> = [Keys<Gen>] extends [Keys<Mine>] ? ([Keys<Mine>] extends [Keys<Gen>] ? true : false) : false
type Widen<X> = X extends null
  ? null | undefined
  : X extends string
    ? string
    : X extends readonly (infer U)[]
      ? Widen<U>[]
      : X extends object
        ? {[K in keyof X]?: Widen<X[K]>}
        : X
type Compatible<Gen, Mine> = [Gen] extends [Widen<Mine>] ? true : false
type Guard<Gen, Mine> = SameKeys<Gen, Mine> extends true ? Compatible<Gen, Mine> : false
type Expect<X extends true> = X
export type _Guards = [
  Expect<Guard<apperr.AppError, T.AppError>>,
  Expect<Guard<settings.DownloadOptions, T.DownloadOptions>>,
  Expect<Guard<settings.SubtitleOptions, T.SubtitleOptions>>,
  Expect<Guard<settings.Cookies, T.Cookies>>,
  Expect<Guard<settings.NamedPreset, T.NamedPreset>>,
  Expect<Guard<settings.Settings, T.Settings>>,
  Expect<Guard<queue.Job, T.Job>>,
  Expect<Guard<queue.NewJob, T.NewJob>>,
  Expect<Guard<main.AppState, T.AppState>>,
  Expect<Guard<main.ToolStatus, T.ToolStatus>>,
  Expect<Guard<main.ProbeResponse, T.ProbeResponse>>,
  Expect<Guard<main.SearchResponse, T.SearchResponse>>,
  Expect<Guard<main.ListingPart, T.ListingPart>>,
  Expect<Guard<main.EnqueueResponse, T.EnqueueResponse>>,
  Expect<Guard<main.BulkResponse, T.BulkResponse>>,
  Expect<Guard<main.LinkFailure, T.LinkFailure>>,
  Expect<Guard<main.BulkLink, T.BulkLink>>,
  Expect<Guard<main.SettingsResponse, T.SettingsResponse>>,
  Expect<Guard<main.CookieResponse, T.CookieResponse>>,
  Expect<Guard<main.ChromeHelperInfo, T.ChromeHelperInfo>>,
  Expect<Guard<main.Browser, T.Browser>>,
  Expect<Guard<main.BrowserProfile, T.BrowserProfile>>,
  Expect<Guard<main.UpdateResult, T.UpdateResult>>,
  Expect<Guard<main.AppUpdate, T.AppUpdate>>,
  Expect<Guard<main.ArchiveInfo, T.ArchiveInfo>>,
  Expect<Guard<ytdlp.Binary, T.Binary>>,
  Expect<Guard<ytdlp.FFmpeg, T.FFmpeg>>,
  Expect<Guard<ytdlp.JSRuntime, T.JSRuntime>>,
  Expect<Guard<ytdlp.CookieTest, T.CookieTest>>,
  Expect<Guard<ytdlp.CookieInfo, T.CookieInfo>>,
  Expect<Guard<ytdlp.FormatInfo, T.FormatInfo>>,
  Expect<Guard<ytdlp.SubtitleTrack, T.SubtitleTrack>>,
  Expect<Guard<ytdlp.Chapter, T.Chapter>>,
  Expect<Guard<ytdlp.VideoInfo, T.VideoInfo>>,
  Expect<Guard<ytdlp.PlaylistEntry, T.PlaylistEntry>>,
  Expect<Guard<ytdlp.PlaylistInfo, T.PlaylistInfo>>,
  Expect<Guard<ytdlp.ProbeResult, T.ProbeResult>>,
  Expect<Guard<ytdlp.SearchFilter, T.SearchFilter>>,
  Expect<Guard<follow.Feed, T.Feed>>,
  Expect<Guard<follow.NewFeed, T.NewFeed>>,
  Expect<Guard<follow.Target, T.FeedTarget>>,
  Expect<Guard<main.FeedResponse, T.FeedResponse>>,
  Expect<Guard<main.TargetResponse, T.TargetResponse>>,
]

// Bindings return Wails' generated classes; at runtime they are plain JSON, so the hand-written
// interfaces are what the rest of the app sees.
const cast = <R>(p: Promise<unknown>) => p as Promise<R>

export const api = {
  getState: () => cast<T.AppState>(App.GetState()),
  saveSettings: (s: T.Settings) => cast<T.SettingsResponse>(App.SaveSettings(s as unknown as settings.Settings)),
  refreshTools: () => cast<T.ToolStatus>(App.RefreshTools()),

  probe: (url: string, noPlaylist: boolean) => cast<T.ProbeResponse>(App.Probe(url, noPlaylist)),
  /** The entries of a probe's listing read since the last call; waits for some when none are new. */
  moreEntries: (listing: number) => cast<T.ListingPart>(App.MoreEntries(listing)),
  cancelProbe: () => App.CancelProbe(),
  enqueue: (items: T.NewJob[], opts: T.DownloadOptions) =>
    cast<T.EnqueueResponse>(App.Enqueue(items as unknown as queue.NewJob[], opts as unknown as settings.DownloadOptions)),
  enqueueLinks: (links: T.BulkLink[]) => cast<T.BulkResponse>(App.EnqueueLinks(links as unknown as main.BulkLink[])),
  cancelBulk: App.CancelBulk,

  pauseJob: App.PauseJob,
  resumeJob: App.ResumeJob,
  cancelJob: App.CancelJob,
  retryJob: App.RetryJob,
  /** Run a failed or cancelled download again with new options. */
  retryJobWith: (id: string, opts: T.DownloadOptions) => App.RetryJobWith(id, opts as unknown as settings.DownloadOptions),
  /** Read a queued download's video (formats, subtitles) without touching the Download page's analysis. */
  probeJob: (id: string) => cast<T.ProbeResponse>(App.ProbeJob(id)),
  removeJob: App.RemoveJob,
  pauseAll: App.PauseAll,
  resumeAll: App.ResumeAll,
  clearFinished: App.ClearFinished,
  retryFailed: App.RetryFailed,
  moveJobToTop: App.MoveJobToTop,
  jobLog: App.GetJobLog,

  openFile: App.OpenFile,
  revealFile: App.RevealFile,
  openDownloadFolder: App.OpenDownloadFolder,
  openDataFolder: App.OpenDataFolder,
  closeShown: App.CloseShown,
  cancelClose: App.CancelClose,
  confirmClose: App.ConfirmClose,
  hideWindow: App.HideWindow,
  setWindowFocused: App.SetWindowFocused,
  getArchiveInfo: () => cast<T.ArchiveInfo>(App.GetArchiveInfo()),
  clearArchive: () => cast<T.ArchiveInfo>(App.ClearArchive()),
  revealArchive: App.RevealArchive,
  pickDirectory: App.PickDirectory,
  pickFile: App.PickFile,
  openExternal: App.OpenExternal,
  readClipboard: App.ReadClipboard,
  copyText: App.CopyText,

  inspectCookieFile: (p: string) => cast<T.CookieInfo>(App.InspectCookieFile(p)),
  importCookieFile: (p: string) => cast<T.CookieResponse>(App.ImportCookieFile(p)),
  importCookieText: (t: string) => cast<T.CookieResponse>(App.ImportCookieText(t)),
  removeCookieFile: () => cast<T.CookieResponse>(App.RemoveCookieFile()),
  testCookies: () => cast<T.CookieTest>(App.TestCookies()),
  chromeHelperSetup: () => cast<T.ChromeHelperInfo>(App.ChromeHelperSetup()),
  openChromeExtensions: App.OpenChromeExtensions,
  getBrowsers: () => cast<T.Browser[]>(App.GetBrowsers()),
  getChromeCookies: () => cast<T.CookieResponse>(App.GetChromeCookies()),
  cancelChromeCookies: App.CancelChromeCookies,

  /** Unpack the tools built into Prem-dl again and re-detect them. */
  repairTools: () => cast<T.UpdateResult>(App.RepairTools()),
  checkAppUpdate: () => cast<T.AppUpdate>(App.CheckAppUpdate()),
  /** Download the new Prem-dl's installer and run it; on success the app quits and restarts. */
  installAppUpdate: () => cast<T.UpdateResult>(App.InstallAppUpdate()),

  listFeeds: () => cast<T.Feed[]>(App.ListFeeds()),
  inspectFeed: (url: string) => cast<T.TargetResponse>(App.InspectFeed(url)),
  followFeed: (f: T.NewFeed) => cast<T.FeedResponse>(App.FollowFeed(f as unknown as follow.NewFeed)),
  editFeed: (id: string, tabs: T.ChannelTab[], opts: T.DownloadOptions) =>
    cast<T.FeedResponse>(App.EditFeed(id, tabs, opts as unknown as settings.DownloadOptions)),
  unfollowFeed: App.UnfollowFeed,
  pauseFeed: App.PauseFeed,
  /** List a followed feed's videos: a channel tab ('' = the first followed one), up to limit (0 = all). */
  browseFeed: (id: string, tab: T.ChannelTab | '', limit: number) => cast<T.ProbeResponse>(App.BrowseFeed(id, tab, limit)),
  /** Read a browseFeed listing on to `want` entries in all (0 = to the end) and return the new ones. */
  moreFeedEntries: (listing: number, want: number) => cast<T.ListingPart>(App.MoreFeedEntries(listing, want)),
  cancelBrowse: App.CancelBrowse,
  /** YouTube results for query from the 1-based position first: at most count, fewer when one page of YouTube's holds fewer. */
  search: (query: string, filter: T.SearchFilter, first: number, count: number) =>
    cast<T.SearchResponse>(App.Search(query, filter as unknown as ytdlp.SearchFilter, first, count)),
  /** Check one feed now, or with '' every feed that isn't paused. */
  checkFeed: App.CheckFeed,
}

// ---- events from Go ------------------------------------------------------------------------
export interface BackendEvents {
  'jobs:update': T.Job[]
  'jobs:remove': string[]
  'tools:status': T.ToolStatus
  'bulk:progress': {done: number; total: number}
  /** A check for a new Prem-dl ran (only sent when it worked). */
  'app:update': T.AppUpdate
  'app:update-progress': {done: number; total: number}
  'settings:changed': T.Settings
  'feeds:update': T.Feed[]
  'feeds:found': T.FeedFound
  'app:open-url': string
  'app:confirm-close': CloseRequest
  /** Show a page: a click on a Windows notification. */
  'app:navigate': string
  /** Something the user asked for outside the window failed (a notification's button). */
  'app:error': string
  /** The desktop turned out to have no tray. */
  'app:no-tray': boolean
}

/** Why closing needs confirming (main.CloseRequest; only sent as an event, so not generated). */
export interface CloseRequest {
  downloads: number
  updating: boolean
  /** The window can hide in the tray instead (api.hideWindow). */
  background: boolean
}

export function on<K extends keyof BackendEvents>(event: K, fn: (data: BackendEvents[K]) => void): () => void {
  return EventsOn(event, fn as (...d: unknown[]) => void)
}

/** Message from a rejected binding promise (Go returned an error). */
export function errMessage(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return 'Something went wrong'
}
