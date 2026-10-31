import {useMemo, useRef, useState} from 'react'
import {bytes} from '../format'
import {Icon} from '../icons'
import {codecName, expressionFor, formatRows, isAudioFormat, PRESET_HEIGHTS, presetEstimate, downloadSize, resolutionLabel} from '../quality'
import type {AudioFormat, DownloadOptions, FormatInfo, Preset, VideoInfo} from '../types'
import {Field, Segmented, Switch} from './ui'

const languageNames = (() => {
  try {
    return new Intl.DisplayNames(undefined, {type: 'language'})
  } catch {
    return null
  }
})()

/** "German", "Chinese (Simplified)"... for a language code; the code itself if unknown. */
export function languageName(code: string): string {
  try {
    return languageNames?.of(code) ?? code
  } catch {
    return code
  }
}

const AUDIO_FORMATS: {value: AudioFormat; label: string; hint: string}[] = [
  {value: 'mp3', label: 'MP3', hint: 'Plays on everything.'},
  {value: 'm4a', label: 'M4A', hint: 'AAC in an .m4a file. Plays almost everywhere, and well on Apple devices.'},
  {value: 'opus', label: 'Opus', hint: 'The smallest files for the quality; older players may not open them.'},
  {value: 'aac', label: 'AAC', hint: 'A bare .aac stream. M4A is the same sound in a file more players understand.'},
  {value: 'flac', label: 'FLAC', hint: "Lossless, so nothing more is lost, but it can't sound better than the source. Large files."},
  {value: 'wav', label: 'WAV', hint: 'Uncompressed. Very large files.'},
  {value: 'best', label: 'Original', hint: 'The audio exactly as the site sends it (usually .opus or .m4a), with no conversion.'},
]

const kbps = (n: number) => (n ? (n >= 1000 ? `${(n / 1000).toFixed(1)} Mbps` : `${Math.round(n)} kbps`) : '')
const approx = (n: number) => (n ? `~${bytes(n)}` : '')

/** The Quality section of the options: Video or Audio, then a preset or an exact format. */
export function QualityPicker({
  value,
  onChange,
  video,
  mode,
}: {
  value: DownloadOptions
  onChange: (v: DownloadOptions) => void
  video?: VideoInfo | null
  mode: 'download' | 'defaults' | 'preset'
}) {
  const set = <K extends keyof DownloadOptions>(k: K, v: DownloadOptions[K]) => onChange({...value, [k]: v})
  const custom = value.preset === 'custom'
  const audioKind = value.preset === 'audio' || (custom && value.formatAudioOnly)
  // The video choice to go back to after trying Audio: a preset, or an exact format.
  const lastVideo = useRef<{preset: Preset; formatId: string}>({preset: audioKind ? 'best' : value.preset, formatId: value.formatId})
  const hasFormats = !!video && video.formats.length > 0
  const rows = useMemo(() => (video ? formatRows(video) : {video: [], audio: []}), [video])

  const setKind = (kind: 'video' | 'audio') => {
    if (kind === (audioKind ? 'audio' : 'video')) return
    if (kind === 'audio') {
      lastVideo.current = {preset: value.preset, formatId: value.formatId}
      onChange({...value, preset: 'audio'})
    } else {
      onChange({...value, ...lastVideo.current, formatAudioOnly: false})
    }
  }
  // Without the video's list (a queued job's options), or with the box emptied, it stays as it was.
  const pickFormat = (formatId: string) => onChange({...value, preset: 'custom', formatId, formatAudioOnly: video && formatId ? isAudioFormat(video, formatId) : value.formatAudioOnly})
  const chooseExact = (audio: boolean) => onChange({...value, preset: 'custom', formatId: '', formatAudioOnly: audio})

  const maxHeight = video && video.heights.length > 0 ? video.heights[0] : 0
  const lossy = ['mp3', 'm4a', 'opus', 'aac'].includes(value.audioFormat)
  const audioHint = AUDIO_FORMATS.find((f) => f.value === value.audioFormat)?.hint

  return (
    <div className="optgroup">
      <div className="opt-head">Quality</div>
      <Segmented
        label="Video or audio"
        value={audioKind ? 'audio' : 'video'}
        options={[
          {value: 'video', label: <><Icon name="film" size={14} /> Video</>},
          {value: 'audio', label: <><Icon name="music" size={14} /> Audio only</>},
        ]}
        onChange={setKind}
      />

      {!audioKind && (
        <>
          <VideoTiles value={value} video={video} maxHeight={maxHeight} onPick={(p) => onChange({...value, preset: p, formatAudioOnly: false})} />
          {maxHeight > 0 && Number(value.preset) > maxHeight && <div className="hint">This video tops out at {maxHeight}p, so that's what it downloads.</div>}
        </>
      )}

      {audioKind && (
        <>
          <Segmented
            label="Audio format"
            value={value.preset === 'audio' ? value.audioFormat : ('' as AudioFormat)}
            options={AUDIO_FORMATS.map((f) => ({value: f.value, label: f.label}))}
            onChange={(v) => onChange({...value, preset: 'audio', audioFormat: v})}
          />
          {value.preset === 'audio' && (
            <div className="row wrap" style={{alignItems: 'flex-end'}}>
              {lossy && (
                <Field label="Quality">
                  <select value={value.audioQuality} onChange={(e) => set('audioQuality', e.target.value)} style={{width: 180}}>
                    <option value="0">Best</option>
                    <option value="2">High</option>
                    <option value="5">Medium</option>
                    <option value="8">Low (small files)</option>
                  </select>
                </Field>
              )}
              <div className="hint grow" style={{marginBottom: lossy ? 8 : 0}}>
                {audioHint}
              </div>
            </div>
          )}
        </>
      )}

      {mode === 'download' && video && video.audioLanguages.length > 1 && !custom && (
        <Field label="Audio language" hint="This video has dubbed audio tracks. Formats without the chosen language use the default one.">
          <select value={value.audioLang} onChange={(e) => set('audioLang', e.target.value)}>
            <option value="">{languageName(video.audioLanguages[0])} (default)</option>
            {video.audioLanguages.slice(1).map((l) => (
              <option key={l} value={l}>
                {languageName(l)}
              </option>
            ))}
          </select>
        </Field>
      )}

      {!custom && hasFormats && (audioKind ? rows.audio.length > 0 : rows.video.length > 0) && (
        <button type="button" className="link" style={{alignSelf: 'flex-start'}} onClick={() => chooseExact(audioKind)}>
          {audioKind ? 'Choose an exact audio stream…' : 'Choose an exact format…'}
        </button>
      )}

      {custom && (
        <ExactFormat
          key={audioKind ? 'audio' : 'video'}
          video={video}
          rows={audioKind ? rows.audio : rows.video}
          audio={audioKind}
          formatId={value.formatId}
          audioLang={value.audioLang}
          onPick={pickFormat}
        />
      )}
      {custom && value.formatAudioOnly && <div className="hint">Saved as an audio file in its own format (.opus or .m4a), without converting it.</div>}

      {!audioKind && (
        <Field label="Container" hint="Auto saves as MP4 where it can. MP4 plays everywhere; MKV holds anything.">
          <Segmented
            label="Container"
            value={value.container || 'auto'}
            options={[
              {value: 'auto', label: 'Auto'},
              {value: 'mp4', label: 'MP4'},
              {value: 'mkv', label: 'MKV'},
              {value: 'webm', label: 'WebM'},
            ]}
            onChange={(v) => set('container', v === 'auto' ? '' : (v as DownloadOptions['container']))}
          />
        </Field>
      )}
      {!audioKind && !custom && value.container !== 'webm' && (
        <Switch
          checked={value.compatible}
          onChange={(v) => set('compatible', v)}
          label="Most compatible (H.264 + AAC)"
          description="For video editors, TVs and older phones that won't play VP9, AV1 or Opus. On YouTube that means 1080p at most."
        />
      )}
    </div>
  )
}

/** Best and the resolutions as tiles, each with what it would download for this video. */
function VideoTiles({value, video, maxHeight, onPick}: {value: DownloadOptions; video?: VideoInfo | null; maxHeight: number; onPick: (p: Preset) => void}) {
  const sized = !!video && video.formats.length > 0
  // A resolution as high as the video's top one is what Best gets anyway, so only lower ones
  // are offered; one already chosen (from saved options) stays so it can be seen.
  const heights = PRESET_HEIGHTS.filter((h) => !maxHeight || h < maxHeight || String(h) === value.preset)
  const tiles = [
    {preset: 'best' as Preset, name: 'Best'},
    ...heights.map((h) => ({preset: String(h) as Preset, name: resolutionLabel(h)})),
  ].map((t) => {
    const est = sized ? presetEstimate(video, t.preset, value.audioLang, value.compatible) : {size: 0}
    const f = est.format
    const tags: string[] = []
    if (f && t.preset === 'best') tags.push(resolutionLabel(f.height, f.fps))
    else if (f && f.height < Number(t.preset)) tags.push(`${f.height}p`)
    else if (f && f.fps > 30) tags.push(`${Math.round(f.fps)} fps`)
    if (f && f.dynamicRange && f.dynamicRange !== 'SDR') tags.push('HDR')
    if (t.preset === 'best' && video?.bestPremium) tags.push('Premium')
    const title = f ? [resolutionLabel(f.height, f.fps), codecName(f.vcodec), kbps(f.vbr || f.tbr)].filter(Boolean).join(' · ') : undefined
    return {...t, size: est.size, tags, title}
  })
  return (
    <div className="qtiles" role="radiogroup" aria-label="Video quality">
      {tiles.map((t) => (
        <button key={t.preset} type="button" role="radio" aria-checked={value.preset === t.preset} className="qtile" title={t.title} onClick={() => onPick(t.preset)}>
          <span className="qtile-name">
            {t.name}
            {t.tags.map((tag) => (
              <span key={tag} className="qtile-tag">
                {tag}
              </span>
            ))}
          </span>
          {sized && <span className="qtile-size">{approx(t.size) || 'size unknown'}</span>}
        </button>
      ))}
    </div>
  )
}

/** The video's formats as a table to pick from, and the raw expression behind "Advanced". */
function ExactFormat({
  video,
  rows,
  audio,
  formatId,
  audioLang,
  onPick,
}: {
  video?: VideoInfo | null
  rows: FormatInfo[]
  audio: boolean
  formatId: string
  audioLang: string
  onPick: (formatId: string) => void
}) {
  const listed = rows.some((f) => expressionFor(f) === formatId)
  const [advanced, setAdvanced] = useState(!video || (!!formatId && !listed))
  return (
    <div className="stack">
      {video && rows.length > 0 && (
        <div className="ftable-wrap">
          <table className="ftable">
            <thead>
              <tr>
                <th aria-label="Chosen" />
                {audio ? <th>Language</th> : <th>Resolution</th>}
                <th>Codec</th>
                <th>Type</th>
                <th className="num">Bitrate</th>
                <th className="num">Size</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((f) => {
                const expr = expressionFor(f)
                const on = expr === formatId
                const name = audio ? (f.language ? languageName(f.language) : 'Audio') : resolutionLabel(f.height, f.fps) || f.resolution
                return (
                  <tr key={f.id} aria-selected={on} onClick={() => onPick(expr)}>
                    <td>
                      <input type="radio" name={`fmt-${audio}`} checked={on} onChange={() => onPick(expr)} aria-label={`${name} ${codecName(audio ? f.acodec : f.vcodec)} (${f.id})`} />
                    </td>
                    <td>
                      {name}
                      {!audio && f.dynamicRange && f.dynamicRange !== 'SDR' && <span className="qtile-tag">{f.dynamicRange}</span>}
                      {!audio && /premium/i.test(f.note) && <span className="qtile-tag">Premium</span>}
                      {audio && /original/i.test(f.note) && <span className="qtile-tag">original</span>}
                    </td>
                    <td>
                      {codecName(audio ? f.acodec : f.vcodec)}
                      {!audio && f.hasAudio && <span className="muted"> + {codecName(f.acodec) || 'audio'}</span>}
                    </td>
                    <td className="muted">{f.ext}</td>
                    <td className="num">{kbps(audio ? f.abr || f.tbr : f.vbr || f.tbr)}</td>
                    <td className="num">{approx(downloadSize(video.formats, f, audioLang))}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {video && !audio && rows.length > 0 && <div className="hint">Formats without sound get the best audio added; sizes include it.</div>}
      <div>
        {video && (
          <button type="button" className="collapse-btn" aria-expanded={advanced} onClick={() => setAdvanced(!advanced)}>
            <span className="chev">
              <Icon name="chevronRight" size={16} />
            </span>
            <span>Advanced</span>
          </button>
        )}
        {advanced && (
          <Field label="Format expression" hint="Edit the format expression directly, e.g. 137+140 or bv*[height<=720]+ba.">
            <input type="text" className="mono" value={formatId} onChange={(e) => onPick(e.target.value)} spellCheck={false} />
          </Field>
        )}
      </div>
    </div>
  )
}
