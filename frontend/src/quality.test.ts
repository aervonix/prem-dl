import {describe, expect, it} from 'vitest'
import {codecName, downloadSize, expressionFor, formatRows, isAudioFormat, pickAudio, pickVideo, presetEstimate, resolutionLabel} from './quality'
import type {FormatInfo, VideoInfo} from './types'

function fmt(p: Partial<FormatInfo>): FormatInfo {
  return {
    id: '', ext: 'mp4', resolution: '', width: 0, height: 0, fps: 0, vcodec: 'none', acodec: 'none', note: '', filesize: 0,
    tbr: 0, abr: 0, vbr: 0, dynamicRange: '', language: '', hasVideo: false, hasAudio: false, ...p,
  }
}
const v = (id: string, height: number, vcodec: string, vbr: number, filesize: number, extra: Partial<FormatInfo> = {}) =>
  fmt({id, height, fps: 25, vcodec, vbr, tbr: vbr, filesize, dynamicRange: 'SDR', hasVideo: true, ...extra})
const a = (id: string, acodec: string, abr: number, filesize: number, extra: Partial<FormatInfo> = {}) =>
  fmt({id, acodec, abr, tbr: abr, filesize, language: 'en', hasAudio: true, ...extra})

// Trimmed from a real probe of dQw4w9WgXcQ (signed in, so it has the 258 kbps "high" audio).
const formats: FormatInfo[] = [
  a('251', 'opus', 129, 3_433_755, {note: 'medium'}),
  a('141', 'mp4a.40.2', 258, 6_858_879, {note: 'high'}),
  v('18', 360, 'avc1.42001E', 0, 11_300_000, {tbr: 444, acodec: 'mp4a.40.2', hasAudio: true}),
  v('313', 2160, 'vp9', 13466, 342_000_000),
  v('401', 2160, 'av01.0.12M.08', 9025, 229_000_000),
  v('137', 1080, 'avc1.640028', 3038, 77_200_000),
  v('248', 1080, 'vp9', 1158, 29_400_000),
  v('136', 720, 'avc1.4d401f', 993, 25_200_000),
]
const video = {formats, heights: [2160, 1080, 720, 360]} as VideoInfo

describe('pickVideo', () => {
  it('takes the highest resolution, then the highest bitrate', () => {
    expect(pickVideo(formats)?.id).toBe('313')
    expect(pickVideo(formats, 1080)?.id).toBe('137')
    expect(pickVideo(formats, 1440)?.id).toBe('137')
  })
  it('ranks frame rate and HDR above bitrate, as QualitySort does', () => {
    const fs = [v('a', 1080, 'vp9', 5000, 1), v('b', 1080, 'vp9', 1000, 1, {fps: 60}), v('c', 1080, 'vp9', 900, 1, {fps: 60, dynamicRange: 'HDR10'})]
    expect(pickVideo(fs)?.id).toBe('c')
  })
  it('falls back to the nearest height above, then to streams of unknown height', () => {
    expect(pickVideo([v('x', 0, 'avc1', 500, 1)], 720)?.id).toBe('x')
    expect(pickVideo([v('x', 0, 'avc1', 500, 1), v('y', 1080, 'avc1', 500, 1), v('z', 1440, 'avc1', 900, 1)], 720)?.id).toBe('y')
  })
  it('never takes TikTok\'s watermarked copy below its lowest stream', () => {
    // Checked with yt-dlp on a post whose streams start at 576p: 480p took "download" (the
    // watermarked copy, no height) under "<=?"; now the 576p H.265 one, and with Most
    // compatible the only H.264 one, 720p.
    const tiktok = [
      v('download', 0, 'h264', 0, 0, {acodec: 'aac', hasAudio: true}),
      v('bytevc1_540p_222800-0', 576, 'h265', 0, 534_000, {tbr: 222, acodec: 'aac', hasAudio: true}),
      v('h264_720p_461863-0', 720, 'h264', 0, 1_107_000, {tbr: 461, acodec: 'aac', hasAudio: true}),
      v('bytevc1_720p_270254-0', 720, 'h265', 0, 648_000, {tbr: 270, acodec: 'aac', hasAudio: true}),
    ]
    expect(pickVideo(tiktok, 480)?.id).toBe('bytevc1_540p_222800-0')
    expect(pickVideo(tiktok, 360)?.id).toBe('bytevc1_540p_222800-0')
    expect(pickVideo(tiktok, 480, true)?.id).toBe('h264_720p_461863-0')
  })
  it('finds nothing without video streams', () => {
    expect(pickVideo([a('251', 'opus', 129, 1)])).toBeUndefined()
  })
})

describe('compatible (ytdlp.CompatibleSort)', () => {
  it('takes H.264 before resolution, but not a low format just for its AAC', () => {
    // Observed with yt-dlp: 1080p avc1 + mp4a, not VP9 2160p, nor the 360p "18" that carries AAC.
    expect(pickVideo(formats, 0, true)?.id).toBe('137')
    expect(pickVideo(formats, 720, true)?.id).toBe('136')
    expect(pickAudio(formats, '', true)?.id).toBe('141')
    expect(pickAudio([a('251', 'opus', 160, 1), a('140', 'mp4a.40.2', 129, 1)], '', true)?.id).toBe('140')
  })
  it('estimates what it would download', () => {
    expect(presetEstimate(video, 'best', '', true)).toEqual({format: formats[5], size: 77_200_000 + 6_858_879})
  })
})

describe('pickAudio', () => {
  it('takes the highest bitrate', () => {
    expect(pickAudio(formats)?.id).toBe('141')
  })
  it('prefers the original track of a dubbed video, or the chosen language', () => {
    const dubbed = [a('de', 'opus', 160, 1, {language: 'de'}), a('en', 'opus', 129, 1, {note: 'English original, medium'}), a('es', 'opus', 140, 1, {language: 'es'})]
    expect(pickAudio(dubbed)?.id).toBe('en')
    expect(pickAudio(dubbed, 'es')?.id).toBe('es')
    expect(pickAudio(dubbed, 'fr')?.id).toBe('en')
  })
})

describe('downloadSize', () => {
  it('adds the best audio to a stream without sound', () => {
    expect(downloadSize(formats, formats[5])).toBe(77_200_000 + 6_858_879)
  })
  it('is the stream itself when it has sound or is audio', () => {
    expect(downloadSize(formats, formats[2])).toBe(11_300_000)
    expect(downloadSize(formats, formats[0])).toBe(3_433_755)
  })
  it('is unknown when either part is', () => {
    expect(downloadSize(formats, v('z', 720, 'avc1', 1, 0))).toBe(0)
    expect(downloadSize([a('n', 'opus', 1, 0)], v('z', 720, 'avc1', 1, 5))).toBe(0)
  })
})

describe('presetEstimate', () => {
  it('estimates Best and each height', () => {
    expect(presetEstimate(video, 'best')).toEqual({format: formats[3], size: 342_000_000 + 6_858_879})
    expect(presetEstimate(video, '720').format?.id).toBe('136')
  })
})

describe('formatRows', () => {
  it('lists video best first and audio by bitrate', () => {
    const rows = formatRows(video)
    expect(rows.video.map((f) => f.id)).toEqual(['313', '401', '137', '248', '136', '18'])
    expect(rows.audio.map((f) => f.id)).toEqual(['141', '251'])
  })
})

describe('expressions', () => {
  it('adds audio only to streams without it', () => {
    expect(expressionFor(formats[5])).toBe('137+ba/137')
    expect(expressionFor(formats[2])).toBe('18')
    expect(expressionFor(formats[0])).toBe('251')
  })
  it('tells audio picks apart', () => {
    expect(isAudioFormat(video, '251')).toBe(true)
    expect(isAudioFormat(video, '137+ba/137')).toBe(false)
    expect(isAudioFormat(video, 'bestaudio')).toBe(false)
  })
})

describe('labels', () => {
  it('names codecs', () => {
    expect(codecName('avc1.640028')).toBe('H.264')
    expect(codecName('vp09.00.51.08')).toBe('VP9')
    expect(codecName('av01.0.12M.08')).toBe('AV1')
    expect(codecName('mp4a.40.2')).toBe('AAC')
    expect(codecName('none')).toBe('')
    expect(codecName('weird.1')).toBe('weird')
  })
  it('names resolutions', () => {
    expect(resolutionLabel(2160)).toBe('4K')
    expect(resolutionLabel(1080, 60)).toBe('1080p60')
    expect(resolutionLabel(720, 30)).toBe('720p')
  })
})
