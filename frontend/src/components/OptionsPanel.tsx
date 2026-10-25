import {useMemo, useState} from 'react'
import {api} from '../api'
import {bytes, parseTime} from '../format'
import {Icon} from '../icons'
import type {AudioFormat, DownloadOptions, FormatInfo, Preset, SubMode, VideoInfo} from '../types'
import {Button, Chip, Field, Segmented, Switch} from './ui'
import type {SegOption} from './ui'

const COMMON_LANGS = ['en', 'es', 'fr', 'de', 'pt', 'it', 'ja', 'ko', 'zh-Hans', 'ru', 'ar', 'hi']

const SPONSOR_CATEGORIES: {id: string; label: string}[] = [
  {id: 'sponsor', label: 'Sponsors'},
  {id: 'intro', label: 'Intro'},
  {id: 'outro', label: 'Outro / credits'},
  {id: 'selfpromo', label: 'Self-promotion'},
  {id: 'preview', label: 'Preview / recap'},
  {id: 'filler', label: 'Filler'},
  {id: 'interaction', label: 'Like & subscribe reminders'},
  {id: 'music_offtopic', label: 'Non-music sections'},
]

const PRESET_HEIGHTS: Record<string, number> = {'2160': 2160, '1440': 1440, '1080': 1080, '720': 720, '480': 480, '360': 360}

/** The yt-dlp expression for downloading a specific format (adding best audio if it has none). */
function expressionFor(f: FormatInfo): string {
  if (f.hasVideo && !f.hasAudio) return `${f.id}+ba/${f.id}`
  return f.id
}

function describeFormat(f: FormatInfo): string {
  const parts: string[] = []
  if (f.hasVideo) {
    parts.push(f.height ? `${f.height}p${f.fps && f.fps > 30 ? Math.round(f.fps) : ''}` : f.resolution || 'video')
  } else if (f.hasAudio) {
    parts.push(f.note || 'audio')
  } else {
    parts.push(f.resolution || f.note || f.id)
  }
  parts.push(f.ext)
  const codec = f.hasVideo ? f.vcodec : f.acodec
  if (codec && codec !== 'none') parts.push(codec.split('.')[0])
  if (f.filesize) parts.push(bytes(f.filesize))
  return `${parts.join(' · ')}  (${f.id})`
}

export function OptionsPanel({
  value,
  onChange,
  video,
  playlist,
  mode,
  archiveOn,
}: {
  value: DownloadOptions
  onChange: (v: DownloadOptions) => void
  video?: VideoInfo | null
  playlist?: boolean
  mode: 'download' | 'defaults'
  /** "Remember downloaded videos" is on, so this download can choose to ignore it. */
  archiveOn?: boolean
}) {
  const set = <K extends keyof DownloadOptions>(k: K, v: DownloadOptions[K]) => onChange({...value, [k]: v})
  const [extrasOpen, setExtrasOpen] = useState(mode === 'defaults')
  const [langDraft, setLangDraft] = useState('')

  const maxHeight = video && video.heights.length > 0 ? video.heights[0] : 0
  const presetOptions: SegOption<Preset>[] = [
    video?.bestPremium
      ? {value: 'best', label: <>Best<span className="seg-tag">Premium</span></>, title: 'Best downloads the YouTube Premium stream for this video'}
      : {value: 'best', label: 'Best'},
    ...(['2160', '1440', '1080', '720', '480', '360'] as const).map((h) => ({
      value: h as Preset,
      label: h === '2160' ? '4K' : `${h}p`,
      disabled: maxHeight > 0 && PRESET_HEIGHTS[h] > maxHeight,
      title: maxHeight > 0 && PRESET_HEIGHTS[h] > maxHeight ? `This video tops out at ${maxHeight}p` : undefined,
    })),
    {value: 'audio', label: 'Audio only'},
    ...(video && video.formats.length > 0 ? [{value: 'custom' as Preset, label: 'Pick format…'}] : []),
  ]

  const customFormats = useMemo(() => {
    if (!video) return {both: [], videoOnly: [], audioOnly: []}
    const sorted = [...video.formats].sort((a, b) => b.height - a.height || b.tbr - a.tbr)
    return {
      both: sorted.filter((f) => f.hasVideo && f.hasAudio),
      videoOnly: sorted.filter((f) => f.hasVideo && !f.hasAudio),
      audioOnly: sorted.filter((f) => !f.hasVideo).sort((a, b) => b.tbr - a.tbr),
    }
  }, [video])

  const sub = value.subtitles
  const setSub = (patch: Partial<DownloadOptions['subtitles']>) => set('subtitles', {...sub, ...patch})
  const toggleLang = (l: string) => setSub({langs: sub.langs.includes(l) ? sub.langs.filter((x) => x !== l) : [...sub.langs, l]})
  const addLang = (raw: string) => {
    const l = raw.trim().replace(/,+$/, '')
    if (l && !sub.langs.includes(l)) setSub({langs: [...sub.langs, l]})
    setLangDraft('')
  }
  const manualLangs = video?.subtitles ?? []
  const autoLangs = video?.autoCaptions ?? []
  const langLabel = (l: string) => manualLangs.find((t) => t.lang === l)?.name ?? autoLangs.find((t) => t.lang === l)?.name ?? l

  const startOk = value.sectionStart === '' || parseTime(value.sectionStart) !== null
  const endOk = value.sectionEnd === '' || parseTime(value.sectionEnd) !== null
  const orderOk = (() => {
    const s = parseTime(value.sectionStart)
    const e = parseTime(value.sectionEnd)
    return s === null || e === null || s < e
  })()

  const isAudio = value.preset === 'audio'
  const lossy = ['mp3', 'm4a', 'opus', 'aac'].includes(value.audioFormat)

  return (
    <div>
      {/* ---- quality ---- */}
      <div className="optgroup">
        <div className="opt-head">
          Quality
        </div>
        <div className={video?.bestPremium ? 'seg-tagged' : undefined}>
          <Segmented label="Quality" value={value.preset} options={presetOptions} onChange={(v) => set('preset', v)} />
        </div>

        {isAudio && (
          <div className="grid3" style={{marginTop: 4}}>
            <Field label="Audio format">
              <select value={value.audioFormat} onChange={(e) => set('audioFormat', e.target.value as AudioFormat)}>
                <option value="mp3">MP3</option>
                <option value="m4a">M4A (AAC)</option>
                <option value="opus">Opus</option>
                <option value="aac">AAC</option>
                <option value="flac">FLAC (lossless)</option>
                <option value="wav">WAV (lossless)</option>
                <option value="best">Original (no conversion)</option>
              </select>
            </Field>
            {lossy && (
              <Field label="Quality">
                <select value={value.audioQuality} onChange={(e) => set('audioQuality', e.target.value)}>
                  <option value="0">Best</option>
                  <option value="2">High</option>
                  <option value="5">Medium</option>
                  <option value="8">Low (small files)</option>
                </select>
              </Field>
            )}
          </div>
        )}

        {value.preset === 'custom' && video && (
          <div className="stack">
            <Field label="Format" hint="Video-only formats get the best audio stream added automatically.">
              <select value={value.formatId} onChange={(e) => set('formatId', e.target.value)}>
                <option value="">Choose a format…</option>
                {customFormats.both.length > 0 && (
                  <optgroup label="Video + audio">
                    {customFormats.both.map((f) => (
                      <option key={f.id} value={expressionFor(f)}>
                        {describeFormat(f)}
                      </option>
                    ))}
                  </optgroup>
                )}
                {customFormats.videoOnly.length > 0 && (
                  <optgroup label="Video only (+ best audio)">
                    {customFormats.videoOnly.map((f) => (
                      <option key={f.id} value={expressionFor(f)}>
                        {describeFormat(f)}
                      </option>
                    ))}
                  </optgroup>
                )}
                {customFormats.audioOnly.length > 0 && (
                  <optgroup label="Audio only">
                    {customFormats.audioOnly.map((f) => (
                      <option key={f.id} value={expressionFor(f)}>
                        {describeFormat(f)}
                      </option>
                    ))}
                  </optgroup>
                )}
              </select>
            </Field>
            <Field label="Format expression" hint="Advanced: edit the yt-dlp -f expression directly.">
              <input type="text" className="mono" value={value.formatId} onChange={(e) => set('formatId', e.target.value)} spellCheck={false} />
            </Field>
          </div>
        )}

        {!isAudio && (
          <Field label="Container" hint="Auto picks what suits the codecs. MP4 plays everywhere; MKV holds anything.">
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
      </div>

      {/* ---- subtitles ---- */}
      <div className="optgroup">
        <div className="opt-head">
          Subtitles
        </div>
        <Segmented
          label="Subtitles"
          value={sub.mode}
          options={[
            {value: 'none', label: 'Off'},
            {value: 'files', label: 'Save as file'},
            {value: 'embed', label: 'Embed in video'},
            {value: 'both', label: 'Both'},
          ]}
          onChange={(v: SubMode) => setSub({mode: v})}
        />
        {sub.mode !== 'none' && (
          <div className="stack">
            <div>
              <div className="hint" style={{marginBottom: 6}}>
                {sub.langs.length === 0 ? 'Pick at least one language.' : 'Languages to download (click to remove):'}
              </div>
              <div className="chips">
                {sub.langs.map((l) => (
                  <Chip key={l} pressed onClick={() => toggleLang(l)} title="Remove">
                    {langLabel(l)} <Icon name="x" size={12} />
                  </Chip>
                ))}
              </div>
            </div>
            <div>
              <div className="hint" style={{marginBottom: 6}}>
                {video ? (manualLangs.length > 0 ? 'Available for this video:' : 'This video has no uploaded subtitles.') : 'Common languages:'}
              </div>
              <div className="chips">
                {(video ? manualLangs.map((t) => t.lang) : COMMON_LANGS).map((l) => (
                  <Chip key={l} pressed={sub.langs.includes(l)} onClick={() => toggleLang(l)} title={langLabel(l)}>
                    {video ? langLabel(l) : l}
                  </Chip>
                ))}
              </div>
            </div>
            <div className="row wrap">
              <div className="grow" style={{minWidth: 200, maxWidth: 320}}>
                <input
                  type="text"
                  placeholder="Add a language code (pt-BR, en.*, all) ↵"
                  value={langDraft}
                  onChange={(e) => setLangDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ',') {
                      e.preventDefault()
                      addLang(langDraft)
                    }
                  }}
                  onBlur={() => langDraft && addLang(langDraft)}
                  aria-label="Add subtitle language code"
                />
              </div>
              {video && sub.auto && autoLangs.length > 0 && (
                <select
                  style={{width: 240}}
                  value=""
                  aria-label="Add an auto-generated caption language"
                  onChange={(e) => e.target.value && toggleLang(e.target.value)}>
                  <option value="">Add auto-caption language…</option>
                  {autoLangs.map((t) => (
                    <option key={t.lang} value={t.lang}>
                      {t.name} ({t.lang})
                    </option>
                  ))}
                </select>
              )}
            </div>
            <div className="grid2">
              <Switch
                checked={sub.auto}
                onChange={(v) => setSub({auto: v})}
                label="Include auto-generated captions"
                description={video ? `${autoLangs.length} languages available (machine-generated or translated)` : 'Used when a language has no uploaded subtitles'}
              />
              <Field label="Convert to">
                <select value={sub.format} onChange={(e) => setSub({format: e.target.value as DownloadOptions['subtitles']['format']})}>
                  <option value="">Keep original format</option>
                  <option value="srt">SRT</option>
                  <option value="vtt">VTT</option>
                  <option value="ass">ASS</option>
                </select>
              </Field>
            </div>
          </div>
        )}
      </div>

      {/* ---- extras ---- */}
      <div className="optgroup">
        <button type="button" className="collapse-btn" aria-expanded={extrasOpen} onClick={() => setExtrasOpen(!extrasOpen)}>
          <span className="chev">
            <Icon name="chevronRight" size={16} />
          </span>
          <span className="opt-head">
            More options
          </span>
        </button>
        {extrasOpen && (
          <div className="stack" style={{marginTop: 4}}>
            <div className="grid2">
              <Switch checked={value.embedMetadata} onChange={(v) => set('embedMetadata', v)} label="Embed metadata" description="Title, artist, date and description inside the file" />
              <Switch checked={value.embedChapters} onChange={(v) => set('embedChapters', v)} label="Embed chapters" description="Chapter markers where the video has them" />
              <Switch
                checked={value.embedThumbnail && !(value.container === 'webm' && !isAudio)}
                disabled={value.container === 'webm' && !isAudio}
                onChange={(v) => set('embedThumbnail', v)}
                label="Embed cover art"
                description={value.container === 'webm' && !isAudio ? "WebM files can't hold cover art. Pick Auto, MP4 or MKV." : "Thumbnail as the file's cover image"}
              />
              <Switch checked={value.writeThumbnail} onChange={(v) => set('writeThumbnail', v)} label="Save thumbnail file" />
              <Switch checked={value.writeDescription} onChange={(v) => set('writeDescription', v)} label="Save description (.description)" />
              <Switch checked={value.writeInfoJson} onChange={(v) => set('writeInfoJson', v)} label="Save info (.info.json)" />
            </div>

            <div>
              <div className="section-title">Skip sections with SponsorBlock</div>
              <div className="chips">
                {SPONSOR_CATEGORIES.map((c) => (
                  <Chip
                    key={c.id}
                    pressed={value.sponsorBlock.includes(c.id)}
                    onClick={() =>
                      set('sponsorBlock', value.sponsorBlock.includes(c.id) ? value.sponsorBlock.filter((x) => x !== c.id) : [...value.sponsorBlock, c.id])
                    }>
                    {c.label}
                  </Chip>
                ))}
              </div>
              <div className="hint" style={{marginTop: 6}}>
                Cuts the selected segments out of the video. YouTube only; needs ffmpeg and re-encodes at cut points.
              </div>
            </div>

            {mode === 'download' && !playlist && (
              <div>
                <div className="section-title">
                  <Icon name="scissors" size={13} /> Download only part of the video
                </div>
                <div className="row wrap">
                  <Field label="From" hint="e.g. 1:30">
                    <input
                      type="text"
                      value={value.sectionStart}
                      placeholder="0:00"
                      aria-invalid={!startOk}
                      style={{width: 110, ...(startOk ? {} : {borderColor: 'var(--danger)'})}}
                      onChange={(e) => set('sectionStart', e.target.value)}
                    />
                  </Field>
                  <Field label="To" hint="blank = until the end">
                    <input
                      type="text"
                      value={value.sectionEnd}
                      placeholder="end"
                      aria-invalid={!endOk}
                      style={{width: 110, ...(endOk ? {} : {borderColor: 'var(--danger)'})}}
                      onChange={(e) => set('sectionEnd', e.target.value)}
                    />
                  </Field>
                </div>
                {(!startOk || !endOk) && <div className="hint" style={{color: 'var(--danger)'}}>Use seconds (90), minutes:seconds (1:30) or h:mm:ss.</div>}
                {startOk && endOk && !orderOk && <div className="hint" style={{color: 'var(--danger)'}}>The start must be before the end.</div>}
              </div>
            )}

            {playlist || mode === 'defaults' ? (
              <div className="grid2">
                <Switch checked={value.playlistFolder} onChange={(v) => set('playlistFolder', v)} label="Put playlists in their own folder" />
                <Switch checked={value.playlistNumbering} onChange={(v) => set('playlistNumbering', v)} label="Number files by playlist position" description="01 - Title, 02 - Title…" />
              </div>
            ) : null}

            {mode === 'download' && (
              <Field label="Save to" hint="Leave empty to use your default download folder.">
                <div className="row">
                  <input type="text" className="grow" value={value.outputDir} placeholder="Default download folder" onChange={(e) => set('outputDir', e.target.value)} />
                  <Button
                    onClick={async () => {
                      const dir = await api.pickDirectory('Choose a download folder', value.outputDir)
                      if (dir) set('outputDir', dir)
                    }}>
                    Browse…
                  </Button>
                  {value.outputDir && (
                    <Button variant="ghost" onClick={() => set('outputDir', '')}>
                      Reset
                    </Button>
                  )}
                </div>
              </Field>
            )}
            {mode === 'download' && archiveOn && (
              <Switch
                checked={value.ignoreArchive}
                onChange={(v) => set('ignoreArchive', v)}
                label="Download again even if already downloaded"
                description="“Remember downloaded videos” is on, so videos downloaded before are skipped. This downloads them anyway."
              />
            )}
          </div>
        )}
      </div>
    </div>
  )
}

/** Whether the time-range inputs hold something yt-dlp would reject. */
export function optionsProblem(o: DownloadOptions): string {
  if (o.preset === 'custom' && !o.formatId.trim()) return 'Choose a format, or pick a quality preset.'
  if (o.subtitles.mode !== 'none' && o.subtitles.langs.length === 0) return 'Pick at least one subtitle language, or turn subtitles off.'
  const s = parseTime(o.sectionStart)
  const e = parseTime(o.sectionEnd)
  if (o.sectionStart && s === null) return 'The start time isn’t a valid time.'
  if (o.sectionEnd && e === null) return 'The end time isn’t a valid time.'
  if (s !== null && e !== null && s >= e) return 'The start time must be before the end time.'
  return ''
}
