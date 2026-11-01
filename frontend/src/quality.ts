import type {FormatInfo, VideoInfo} from './types'

/** The resolutions offered as quality presets, highest first. */
export const PRESET_HEIGHTS = [2160, 1440, 1080, 720, 480, 360] as const

/** The yt-dlp expression for downloading a specific format (adding best audio if it has none). */
export function expressionFor(f: FormatInfo): string {
  if (f.hasVideo && !f.hasAudio) return `${f.id}+ba/${f.id}`
  return f.id
}

/** Whether a format expression picks one of the video's audio-only formats. */
export function isAudioFormat(video: VideoInfo, formatId: string): boolean {
  return video.formats.some((f) => !f.hasVideo && f.hasAudio && expressionFor(f) === formatId)
}

/** "H.264", "VP9", "AV1"... for a codec string like "avc1.640028"; '' for none. */
export function codecName(codec: string): string {
  if (!codec || codec === 'none') return ''
  const c = codec.toLowerCase()
  if (c.startsWith('avc') || c.startsWith('h264')) return 'H.264'
  if (c.startsWith('hev') || c.startsWith('hvc') || c.startsWith('h265')) return 'H.265'
  if (c.startsWith('vp09') || c.startsWith('vp9')) return 'VP9'
  if (c.startsWith('vp8')) return 'VP8'
  if (c.startsWith('av01')) return 'AV1'
  if (c.startsWith('mp4a')) return 'AAC'
  if (c.startsWith('opus')) return 'Opus'
  if (c.startsWith('vorbis')) return 'Vorbis'
  if (c.startsWith('mp3')) return 'MP3'
  if (c.startsWith('flac')) return 'FLAC'
  if (c.startsWith('ac-3') || c.startsWith('ac3')) return 'AC-3'
  if (c.startsWith('ec-3') || c.startsWith('eac3')) return 'E-AC-3'
  return codec.split('.')[0]
}

/** "4K", "1080p60"... for a resolution preset or a format's height and frame rate. */
export function resolutionLabel(height: number, fps = 0): string {
  const rate = fps > 30 ? String(Math.round(fps)) : ''
  if (height >= 2160 && height < 4320) return `4K${rate}`
  if (height >= 4320) return `8K${rate}`
  return `${height}p${rate}`
}

// Matches the "hdr:12" in ytdlp.QualitySort: HDR ranks above SDR, Dolby Vision is past the limit.
const HDR_RANK: Record<string, number> = {HDR12: 4, 'HDR10+': 3, HDR10: 2, HLG: 1, SDR: 0, DV: -1}

function hdrRank(f: FormatInfo): number {
  return HDR_RANK[f.dynamicRange] ?? 0
}

// ytdlp.CompatibleSort's "+vcodec:avc" and "+acodec:m4a": H.264 and AAC first, the rest as equals.
const isAvc = (f: FormatInfo) => (/^(avc|h264)/i.test(f.vcodec) ? 1 : 0)
const isAac = (f: FormatInfo) => (/^(mp4a|aac)/i.test(f.acodec) ? 1 : 0)

/**
 * The video stream a preset picks under ytdlp.QualitySort (res,fps,hdr:12,vbr), or with
 * compatible ytdlp.CompatibleSort (H.264 first): "bv*[height<=h]", the best at or below h, else
 * the fallback "bv*" under "height:h" (ytdlp.heightSort), the nearest height above h with
 * unknown heights last. 0 means no limit.
 */
export function pickVideo(formats: FormatInfo[], maxHeight = 0, compatible = false): FormatInfo | undefined {
  const video = formats.filter((f) => f.hasVideo)
  const codec = (a: FormatInfo, b: FormatInfo) => (compatible ? isAvc(b) - isAvc(a) : 0)
  const audio = (a: FormatInfo, b: FormatInfo) => (compatible ? isAac(b) - isAac(a) : 0)
  const rest = (a: FormatInfo, b: FormatInfo) => b.fps - a.fps || audio(a, b) || hdrRank(b) - hdrRank(a) || (b.vbr || b.tbr) - (a.vbr || a.tbr)
  const highest = (pool: FormatInfo[]) => [...pool].sort((a, b) => codec(a, b) || b.height - a.height || rest(a, b))[0]
  if (maxHeight <= 0) return highest(video)
  const under = video.filter((f) => f.height > 0 && f.height <= maxHeight)
  if (under.length > 0) return highest(under)
  const above = (f: FormatInfo) => (f.height > 0 ? f.height - maxHeight : Number.MAX_SAFE_INTEGER)
  return [...video].sort((a, b) => codec(a, b) || above(a) - above(b) || rest(a, b))[0]
}

/**
 * The audio stream "ba" picks: the track in lang when given (as withAudioLang does), else the
 * original track of a dubbed video, then the highest bitrate.
 */
export function pickAudio(formats: FormatInfo[], lang = '', compatible = false): FormatInfo | undefined {
  let pool = formats.filter((f) => f.hasAudio && !f.hasVideo)
  const narrow = (keep: (f: FormatInfo) => boolean) => {
    const kept = pool.filter(keep)
    if (kept.length > 0) pool = kept
    return kept.length > 0
  }
  if (!lang || !narrow((f) => f.language.startsWith(lang))) narrow((f) => /original/i.test(f.note))
  return [...pool].sort((a, b) => (compatible ? isAac(b) - isAac(a) : 0) || (b.abr || b.tbr) - (a.abr || a.tbr))[0]
}

/** About how big a format downloads: with the best audio added when it has none; 0 when unknown. */
export function downloadSize(formats: FormatInfo[], f: FormatInfo, audioLang = '', compatible = false): number {
  if (!f.filesize) return 0
  if (!f.hasVideo || f.hasAudio) return f.filesize
  const audio = pickAudio(formats, audioLang, compatible)
  if (!audio) return f.filesize
  return audio.filesize ? f.filesize + audio.filesize : 0
}

/** What a video preset ("best" or a height) would download from this video, and about how big. */
export function presetEstimate(video: VideoInfo, preset: string, audioLang = '', compatible = false): {format?: FormatInfo; size: number} {
  const format = pickVideo(video.formats, preset === 'best' ? 0 : Number(preset) || 0, compatible)
  return {format, size: format ? downloadSize(video.formats, format, audioLang, compatible) : 0}
}

/** A video's formats for the format table, best first; audio-only ones by bitrate. */
export function formatRows(video: VideoInfo): {video: FormatInfo[]; audio: FormatInfo[]} {
  const byQuality = (a: FormatInfo, b: FormatInfo) => b.height - a.height || b.fps - a.fps || hdrRank(b) - hdrRank(a) || b.tbr - a.tbr
  return {
    video: video.formats.filter((f) => f.hasVideo).sort(byQuality),
    audio: video.formats.filter((f) => f.hasAudio && !f.hasVideo).sort((a, b) => (b.abr || b.tbr) - (a.abr || a.tbr)),
  }
}
