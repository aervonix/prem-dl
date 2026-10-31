// Hand-written mirrors of the Go structs (JSON shape). api.ts contains compile-time checks that
// these stay compatible with the classes Wails generates in wailsjs/go/models.ts.

export interface AppError {
  code: string
  title: string
  message: string
  hint: string
  /** 'install_ytdlp' and 'install_ffmpeg' come from failures saved by older versions; they mean 'repair'. */
  action: '' | 'update' | 'cookies' | 'repair' | 'settings' | 'install_ytdlp' | 'install_ffmpeg'
  detail: string
  retryable: boolean
}

export type Preset = 'best' | '2160' | '1440' | '1080' | '720' | '480' | '360' | 'audio' | 'custom'
export type SubMode = 'none' | 'files' | 'embed' | 'both'
export type AudioFormat = 'best' | 'mp3' | 'm4a' | 'opus' | 'flac' | 'wav' | 'aac'

export interface SubtitleOptions {
  mode: SubMode
  langs: string[]
  auto: boolean
  format: '' | 'srt' | 'vtt' | 'ass'
}

export interface DownloadOptions {
  preset: Preset
  formatId: string
  /** The custom format is audio only; it is saved as an audio file in its own codec. */
  formatAudioOnly: boolean
  container: '' | 'mp4' | 'mkv' | 'webm'
  audioFormat: AudioFormat
  audioQuality: string
  subtitles: SubtitleOptions
  embedMetadata: boolean
  embedThumbnail: boolean
  embedChapters: boolean
  writeDescription: boolean
  writeInfoJson: boolean
  writeThumbnail: boolean
  sponsorBlock: string[]
  sectionStart: string
  sectionEnd: string
  /** Download only these chapters (by title), each as its own file. Not with a time range. */
  chapters: string[]
  outputDir: string
  playlistFolder: boolean
  playlistNumbering: boolean
  /** Download even when the download archive lists the video. */
  ignoreArchive: boolean
  /** Mark the SponsorBlock segments as chapters instead of cutting them out. */
  sponsorBlockMark: boolean
  /** Also save each chapter as its own file. */
  splitChapters: boolean
  /** Record a live stream from its beginning. */
  liveFromStart: boolean
  /** Wait for a scheduled stream or premiere to start, then download it. */
  waitForVideo: boolean
  /** Audio track language on dubbed videos; '' = the default track. */
  audioLang: string
}

/** A saved set of options picked by name on the Download page (settings.NamedPreset). */
export interface NamedPreset {
  name: string
  /** Host names (music.youtube.com); a link to one, or a subdomain of one, picks this preset. */
  sites: string[]
  options: DownloadOptions
}

export interface Cookies {
  mode: 'none' | 'browser' | 'file'
  browser: string
  profile: string
  file: string
}

export interface Settings {
  downloadDir: string
  filenameTemplate: string
  maxConcurrent: number
  concurrentFragments: number
  rateLimit: string
  proxy: string
  /** Pass --force-ipv4: for networks whose IPv6 addresses don't connect. */
  forceIPv4: boolean
  retries: number
  /** Retry downloads that failed with a passing problem (network, 403, rate limiting). */
  autoRetry: boolean
  /** Seconds between requests while a download reads the site (0 = none). */
  sleepRequests: number
  /** Seconds to wait before each download starts (0 = none). */
  sleepInterval: number
  useArchive: boolean
  cookies: Cookies
  /** The browser the Chrome helper runs in (a Browser id); "" = the default one. */
  helperBrowser: string
  /** Look for new Prem-dl releases. */
  checkAppUpdates: boolean
  theme: 'system' | 'light' | 'dark'
  /** A click when a download finishes. */
  notifySound: boolean
  /** A card in the corner when a download finishes. */
  notifyPopup: boolean
  /** A Windows notification when a download finishes while the app is in the background. */
  notifySystem: boolean
  /** Closing the window keeps the app running in the tray. */
  closeToTray: boolean
  /** Start in the tray when the user signs in to Windows. */
  startWithWindows: boolean
  /** The computer doesn't go to sleep while downloads run (the screen may still turn off). */
  keepAwake: boolean
  /** Hours between checks of followed channels and playlists: 0 (only when asked), 1, 3, 6, 12 or 24. */
  followEvery: number
  extraArgs: string
  defaults: DownloadOptions
  presets: NamedPreset[]
}

export type JobStatus =
  | 'queued'
  | 'starting'
  | 'downloading'
  | 'processing'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'cancelled'

export interface Job {
  id: string
  seq: number
  url: string
  title: string
  uploader: string
  thumbnail: string
  duration: number
  status: JobStatus
  stage: string
  percent: number
  downloaded: number
  total: number
  speed: number
  eta: number
  playlistTitle: string
  playlistIndex: number
  playlistCount: number
  options: DownloadOptions
  outputFile: string
  note: string
  error: AppError | null
  warnings: string[]
  attempts: number
  /** Set (a real date) while the job waits to be retried automatically after a passing problem. */
  retryAt: string
  autoRetries: number
  /** Set (a real date) while it waits for a scheduled stream to start: when it looks again. */
  waitUntil: string
  createdAt: string
  startedAt: string
  finishedAt: string
}

export interface NewJob {
  url: string
  title: string
  uploader: string
  thumbnail: string
  duration: number
  playlistTitle: string
  playlistIndex: number
  playlistCount: number
}

export interface Binary {
  path: string
  source: string
  version: string
}

export interface FFmpeg {
  path: string
  version: string
}

export interface JSRuntime {
  name: string
  path: string
}

export interface ToolStatus {
  checking: boolean
  ytdlp: Binary | null
  ytdlpError: string
  ffmpeg: FFmpeg | null
  jsRuntimes: JSRuntime[]
  /** This build carries its own yt-dlp and ffmpeg and runs only those. */
  builtIn: boolean
}

export interface AppState {
  version: string
  settings: Settings
  jobs: Job[]
  feeds: Feed[]
  tools: ToolStatus
  dataDir: string
  /** A link the app was started with (a prem-dl: link from the Chrome helper), for the Download page. */
  openLink: string
  /** runtime.GOOS: windows, linux, … */
  platform: string
  /** The desktop has no tray (Linux without one), so closing can't keep the app running. */
  noTray: boolean
  /** The last successful check for a new Prem-dl; null until one has run. */
  appUpdate?: AppUpdate | null
}

export interface FormatInfo {
  id: string
  ext: string
  resolution: string
  width: number
  height: number
  fps: number
  vcodec: string
  acodec: string
  note: string
  filesize: number
  tbr: number
  /** Audio / video bitrate, kbit/s. */
  abr: number
  vbr: number
  /** SDR, HDR10, HLG...; '' when unknown. */
  dynamicRange: string
  /** Audio language, e.g. "en" or "es-419". */
  language: string
  hasVideo: boolean
  hasAudio: boolean
}

export interface SubtitleTrack {
  lang: string
  name: string
  auto: boolean
}

export interface VideoInfo {
  id: string
  title: string
  url: string
  uploader: string
  thumbnail: string
  /** A smaller picture for list rows (the queue). */
  thumbnailSmall: string
  /** The filename template expanded for this video; the extension is the one "Best" gives. */
  fileName: string
  duration: number
  uploadDate: string
  viewCount: number
  isLive: boolean
  extractor: string
  formats: FormatInfo[]
  heights: number[]
  subtitles: SubtitleTrack[]
  autoCaptions: SubtitleTrack[]
  /** "Best" will download a YouTube Premium stream for this video. */
  bestPremium: boolean
  /** Audio track languages when there is more than one (dubbed videos), the default first. */
  audioLanguages: string[]
  /** A scheduled live stream or premiere that hasn't started; it has no formats yet. */
  upcoming: boolean
  /** When it is due, in Unix seconds; 0 when unknown. */
  releaseTimestamp: number
  chapters: Chapter[]
}

/** A chapter of a video (ytdlp.Chapter); times in seconds. */
export interface Chapter {
  title: string
  start: number
  end: number
}

export interface PlaylistEntry {
  index: number
  id: string
  title: string
  url: string
  uploader: string
  thumbnail: string
  duration: number
  group: string
  unavailable: boolean
  /** The entry is itself a playlist (e.g. on a channel's Playlists tab); open it to pick videos. */
  isPlaylist: boolean
  /** yt-dlp's live_status where the listing gives it: is_upcoming, is_live, was_live, not_live or ''. */
  liveStatus: string
  /** The extractor that handles the link (Youtube); '' when unknown. */
  extractor: string
  /** 0 when the listing doesn't say. */
  viewCount: number
  /** Upload time in Unix seconds; 0 when unknown. */
  timestamp: number
}

export interface PlaylistInfo {
  id: string
  title: string
  url: string
  uploader: string
  entries: PlaylistEntry[]
  /** A channel's uploads rather than a playlist: it opens with nothing ticked. */
  channel: boolean
}

export interface ProbeResult {
  kind: 'video' | 'playlist'
  video: VideoInfo | null
  playlist: PlaylistInfo | null
}

export interface ProbeResponse {
  result: ProbeResult | null
  error: AppError | null
  cancelled: boolean
  /** Set while the playlist's further entries are still being read: ask moreEntries for them. */
  listing: number
}

/** The next entries of a playlist being read. */
export interface ListingPart {
  entries: PlaylistEntry[]
  done: boolean
  /** Why the rest couldn't be read, when done. */
  error: string
}

export interface SearchResponse {
  result: ProbeResult | null
  error: AppError | null
  cancelled: boolean
  /** There are results after these ("Show more"). */
  more: boolean
}

export interface EnqueueResponse {
  added: number
  skipped: number
  error: string
}

/** A link of a bulk add and the options it is queued with. */
export interface BulkLink {
  url: string
  options: DownloadOptions
}

export interface LinkFailure {
  url: string
  error: AppError
}

export interface BulkResponse {
  added: number
  skipped: number
  failed: LinkFailure[]
  error: string
  cancelled: boolean
}

export interface SettingsResponse {
  settings: Settings
  error: string
}

export interface CookieInfo {
  path: string
  valid: boolean
  problem: string
  count: number
  expired: number
  session: number
  domains: string[]
  domainCount: number
}

export interface CookieResponse {
  info: CookieInfo
  settings: Settings
  error: string
}

/** Where the Chrome cookie helper extension was written, for "Load unpacked". */
export interface ChromeHelperInfo {
  path: string
  extensionId: string
  chromeFound: boolean
  /** The browser the helper runs in: "Chrome" on Windows. */
  browser: string
  error: string
}

export interface BrowserProfile {
  path: string
  name: string
}

/** A browser installed here (GetBrowsers): to read cookies from, and maybe to run the helper in. */
export interface Browser {
  /** chrome, helium, flatpak:com.google.Chrome, … */
  id: string
  label: string
  /** The name yt-dlp knows it by; it also picks the key that decrypts the cookies. */
  family: string
  /** Its profiles' folder when yt-dlp can't find it by name: profiles are then passed as paths. */
  root: string
  profiles: BrowserProfile[]
  /** Found here; yt-dlp may still find one that isn't. */
  installed: boolean
  /** A Chromium browser the helper can run in. */
  helper: boolean
  /** The system's default browser. */
  default: boolean
}

export interface CookieTest {
  ok: boolean
  count: number
  browser: string
  error: AppError | null
}

/** What a check for a new Prem-dl found (main.AppUpdate). */
export interface AppUpdate {
  current: string
  latest: string
  available: boolean
  /** The release on GitHub. */
  page: string
  /** The release's description, as Markdown. */
  notes: string
  /** The installer's size in bytes. */
  size: number
  /** This copy came from the installer, so the update can download and install itself. */
  canInstall: boolean
  error: string
}

export interface UpdateResult {
  ok: boolean
  previous: string
  version: string
  path: string
  message: string
  error: AppError | null
}

export interface ArchiveInfo {
  path: string
  /** Videos the download archive lists. */
  count: number
  error: string
}

export type ChannelTab = 'videos' | 'shorts' | 'streams'

/** Narrows a YouTube search (ytdlp.SearchFilter); '' means any. */
export interface SearchFilter {
  type: '' | 'video' | 'channel' | 'playlist'
  uploaded: '' | 'hour' | 'today' | 'week' | 'month' | 'year'
  /** short is under 4 minutes, medium 4 to 20, long over 20. */
  length: '' | 'short' | 'medium' | 'long'
}

/** A followed channel or playlist (follow.Feed). */
export interface Feed {
  id: string
  /** A YouTube channel's home page when tabs is set. */
  url: string
  title: string
  uploader: string
  /** The YouTube channel tabs checked; empty for anything else, whose url is checked as it is. */
  tabs: ChannelTab[]
  options: DownloadOptions
  filter: FeedFilter
  paused: boolean
  addedAt: string
  /** When the last check ended; Go's zero time (year 1) before the first. */
  lastCheck: string
  lastFound: string
  /** Videos the last check queued. */
  lastNew: number
  /** Videos queued since following. */
  queued: number
  /** The videos already there when it was followed have been noted (they aren't downloaded). */
  ready: boolean
  checking: boolean
  /** Why the last check failed; null when it worked. */
  error: AppError | null
}

/**
 * What a check of a followed feed queues. Videos turned away are noted like ones already
 * downloaded; a change applies to uploads from then on.
 */
export interface FeedFilter {
  /** Shorter videos are skipped; 0 = no minimum. A video without a length passes. */
  minMinutes: number
  /** Longer ones are skipped; 0 = no maximum. */
  maxMinutes: number
  /** The title must contain one of these (ignoring case); empty = any title. */
  include: string[]
  /** Titles containing any of these are skipped. */
  exclude: string[]
}

export interface NewFeed {
  url: string
  title: string
  uploader: string
  tabs: ChannelTab[]
  options: DownloadOptions
  filter: FeedFilter
}

/** How a link would be followed (follow.Target). */
export interface FeedTarget {
  url: string
  /** A YouTube channel: it chooses which tabs to check; tabs is the suggestion. */
  channel: boolean
  tabs: ChannelTab[]
  /** ID of the feed already following it, or ''. */
  followed: string
}

export interface FeedResponse {
  feed: Feed | null
  error: string
}

export interface TargetResponse {
  target: FeedTarget
  error: string
}

/** Sent as "feeds:found" when a check queues new videos. */
export interface FeedFound {
  feedId: string
  title: string
  count: number
  /** Titles of the first few. */
  videos: string[]
}
