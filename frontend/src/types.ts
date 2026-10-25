// Hand-written mirrors of the Go structs (JSON shape). api.ts contains compile-time checks that
// these stay compatible with the classes Wails generates in wailsjs/go/models.ts.

export interface AppError {
  code: string
  title: string
  message: string
  hint: string
  action: '' | 'update' | 'cookies' | 'install_ytdlp' | 'settings' | 'install_ffmpeg'
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
  outputDir: string
  playlistFolder: boolean
  playlistNumbering: boolean
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
  retries: number
  /** Retry downloads that failed with a passing problem (network, 403, rate limiting). */
  autoRetry: boolean
  useArchive: boolean
  cookies: Cookies
  autoUpdate: boolean
  updateChannel: 'stable' | 'nightly' | 'master'
  lastUpdateCheck: string
  ytdlpPath: string
  ffmpegPath: string
  theme: 'system' | 'light' | 'dark'
  /** A click when a download finishes. */
  notifySound: boolean
  /** A card in the corner when a download finishes. */
  notifyPopup: boolean
  extraArgs: string
  defaults: DownloadOptions
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
  managedPath: string
  /** The ffmpeg in use is the one the app installed. */
  ffmpegManaged: boolean
  canInstallFfmpeg: boolean
}

export interface AppState {
  version: string
  settings: Settings
  jobs: Job[]
  tools: ToolStatus
  dataDir: string
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
}

export interface PlaylistInfo {
  id: string
  title: string
  url: string
  uploader: string
  entries: PlaylistEntry[]
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
}

export interface EnqueueResponse {
  added: number
  skipped: number
  error: string
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
  error: string
}

export interface CookieTest {
  ok: boolean
  count: number
  browser: string
  error: AppError | null
}

export interface UpdateInfo {
  current: string
  latest: string
  available: boolean
  channel: string
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
