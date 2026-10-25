// The only module that talks to the Go backend. Everything else imports `api` from here.
import * as App from '../wailsjs/go/main/App'
import type {main, queue, settings, ytdlp, apperr} from '../wailsjs/go/models'
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
  Expect<Guard<settings.Settings, T.Settings>>,
  Expect<Guard<queue.Job, T.Job>>,
  Expect<Guard<queue.NewJob, T.NewJob>>,
  Expect<Guard<main.AppState, T.AppState>>,
  Expect<Guard<main.ToolStatus, T.ToolStatus>>,
  Expect<Guard<main.ProbeResponse, T.ProbeResponse>>,
  Expect<Guard<main.EnqueueResponse, T.EnqueueResponse>>,
  Expect<Guard<main.BulkResponse, T.BulkResponse>>,
  Expect<Guard<main.LinkFailure, T.LinkFailure>>,
  Expect<Guard<main.SettingsResponse, T.SettingsResponse>>,
  Expect<Guard<main.CookieResponse, T.CookieResponse>>,
  Expect<Guard<main.UpdateInfo, T.UpdateInfo>>,
  Expect<Guard<main.UpdateResult, T.UpdateResult>>,
  Expect<Guard<ytdlp.Binary, T.Binary>>,
  Expect<Guard<ytdlp.FFmpeg, T.FFmpeg>>,
  Expect<Guard<ytdlp.JSRuntime, T.JSRuntime>>,
  Expect<Guard<ytdlp.CookieTest, T.CookieTest>>,
  Expect<Guard<ytdlp.CookieInfo, T.CookieInfo>>,
  Expect<Guard<ytdlp.FormatInfo, T.FormatInfo>>,
  Expect<Guard<ytdlp.SubtitleTrack, T.SubtitleTrack>>,
  Expect<Guard<ytdlp.VideoInfo, T.VideoInfo>>,
  Expect<Guard<ytdlp.PlaylistEntry, T.PlaylistEntry>>,
  Expect<Guard<ytdlp.PlaylistInfo, T.PlaylistInfo>>,
  Expect<Guard<ytdlp.ProbeResult, T.ProbeResult>>,
]

// Bindings return Wails' generated classes; at runtime they are plain JSON, so the hand-written
// interfaces are what the rest of the app sees.
const cast = <R>(p: Promise<unknown>) => p as Promise<R>

export const api = {
  getState: () => cast<T.AppState>(App.GetState()),
  saveSettings: (s: T.Settings) => cast<T.SettingsResponse>(App.SaveSettings(s as unknown as settings.Settings)),
  refreshTools: () => cast<T.ToolStatus>(App.RefreshTools()),

  probe: (url: string, noPlaylist: boolean) => cast<T.ProbeResponse>(App.Probe(url, noPlaylist)),
  cancelProbe: () => App.CancelProbe(),
  enqueue: (items: T.NewJob[], opts: T.DownloadOptions) =>
    cast<T.EnqueueResponse>(App.Enqueue(items as unknown as queue.NewJob[], opts as unknown as settings.DownloadOptions)),
  enqueueLinks: (links: string[], opts: T.DownloadOptions) =>
    cast<T.BulkResponse>(App.EnqueueLinks(links, opts as unknown as settings.DownloadOptions)),

  pauseJob: App.PauseJob,
  resumeJob: App.ResumeJob,
  cancelJob: App.CancelJob,
  retryJob: App.RetryJob,
  removeJob: App.RemoveJob,
  pauseAll: App.PauseAll,
  resumeAll: App.ResumeAll,
  clearFinished: App.ClearFinished,
  jobLog: App.GetJobLog,

  openFile: App.OpenFile,
  revealFile: App.RevealFile,
  openDownloadFolder: App.OpenDownloadFolder,
  openDataFolder: App.OpenDataFolder,
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

  checkUpdate: () => cast<T.UpdateInfo>(App.CheckUpdate()),
  updateYtdlp: () => cast<T.UpdateResult>(App.UpdateYtdlp()),
}

// ---- events from Go ------------------------------------------------------------------------
export interface BackendEvents {
  'jobs:update': T.Job[]
  'jobs:remove': string[]
  'tools:status': T.ToolStatus
  'tools:progress': {done: number; total: number}
  'tools:update-available': T.UpdateInfo
  'tools:auto-updated': T.UpdateResult
  'settings:changed': T.Settings
  'app:open-url': string
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
