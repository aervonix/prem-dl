import {useState} from 'react'
import {api} from '../api'
import {parseTime} from '../format'
import {Icon} from '../icons'
import type {DownloadOptions, SubMode, VideoInfo} from '../types'
import {PartPicker} from './PartPicker'
import {QualityPicker} from './QualityPicker'
import {Button, Chip, Field, Segmented, Switch} from './ui'
import {TEMPLATES, optionTemplateProblem} from '../templates'

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

export function OptionsPanel({
  value,
  onChange,
  video,
  playlist,
  mode,
  archiveOn,
  feed,
}: {
  value: DownloadOptions
  onChange: (v: DownloadOptions) => void
  video?: VideoInfo | null
  playlist?: boolean
  /** defaults and preset hold saved options, with nothing tied to one video; a preset can have its own folder. */
  mode: 'download' | 'defaults' | 'preset'
  /** "Remember downloaded videos" is on, so this download can choose to ignore it. */
  archiveOn?: boolean
  /** Options for a followed channel or playlist: its new uploads, which have no position to number. */
  feed?: boolean
}) {
  const set = <K extends keyof DownloadOptions>(k: K, v: DownloadOptions[K]) => onChange({...value, [k]: v})
  const [extrasOpen, setExtrasOpen] = useState(mode !== 'download')
  const [langDraft, setLangDraft] = useState('')

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

  // The download is an audio file: "Audio only", or an audio format picked from the list.
  const audioFile = value.preset === 'audio' || (value.preset === 'custom' && value.formatAudioOnly)
  // A photo post's pictures and sound are saved as they are: nothing to embed, cut or convert.
  const pictures = mode === 'download' && !!video && video.photos > 0 && !audioFile

  return (
    <div>
      {/* ---- quality ---- */}
      <QualityPicker value={value} onChange={onChange} video={video} mode={mode} />

      {/* ---- subtitles ---- */}
      {!pictures && (
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
          {audioFile && (sub.mode === 'embed' || sub.mode === 'both') && (
            <div className="hint">
              {sub.mode === 'embed'
                ? "Audio files don't get embedded subtitles, so none are downloaded. Choose Save as file to keep them."
                : "Audio files don't get embedded subtitles, so they're only saved as files."}
            </div>
          )}
          {sub.mode !== 'none' && (
            <div className="stack">
              <div>
                <div className="hint" style={{marginBottom: 6}}>
                  {sub.langs.length === 0 ? 'Pick at least one language.' : 'Languages to download (click to remove):'}
                </div>
                <div className="chips">
                  {sub.langs.map((l) => (
                    <Chip key={l} onClick={() => toggleLang(l)} title="Remove" removes={langLabel(l)}>
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

      )}

      {/* ---- part of the video ---- */}
      {mode === 'download' && !playlist && !pictures && <PartPicker value={value} onChange={onChange} video={video} />}

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
            {!pictures && (
              <>
                <div className="grid2">
                  <Switch checked={value.embedMetadata} onChange={(v) => set('embedMetadata', v)} label="Embed metadata" description="Title, artist, date and description inside the file" />
                  <Switch checked={value.embedChapters} onChange={(v) => set('embedChapters', v)} label="Embed chapters" description="Chapter markers where the video has them" />
                  <Switch
                    checked={value.embedThumbnail && !(value.container === 'webm' && !audioFile)}
                    disabled={value.container === 'webm' && !audioFile}
                    onChange={(v) => set('embedThumbnail', v)}
                    label="Embed cover art"
                    description={value.container === 'webm' && !audioFile ? "WebM files can't hold cover art. Pick Auto, MP4 or MKV." : "Thumbnail as the file's cover image"}
                  />
                  {audioFile && value.embedThumbnail && (
                    <Switch
                      checked={value.squareCover}
                      onChange={(v) => set('squareCover', v)}
                      label="Square cover art"
                      description="Crops the middle out of the 16:9 thumbnail, where YouTube puts the album art"
                    />
                  )}
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
                  {value.sponsorBlock.length > 0 && (
                    <div style={{marginTop: 8}}>
                      <Segmented
                        label="What to do with them"
                        value={value.sponsorBlockMark ? 'mark' : 'cut'}
                        onChange={(v) => set('sponsorBlockMark', v === 'mark')}
                        options={[
                          {value: 'cut', label: 'Cut out'},
                          {value: 'mark', label: 'Mark as chapters'},
                        ]}
                      />
                    </div>
                  )}
                  <div className="hint" style={{marginTop: 6}}>
                    {value.sponsorBlockMark
                      ? 'Adds the selected segments as chapters, so players can skip them; the video stays whole. YouTube only.'
                      : 'Cuts the selected segments out of the video. YouTube only; needs ffmpeg and re-encodes at cut points.'}
                  </div>
                </div>

                <div className="grid2">
                  {value.chapters.length === 0 && (
                    <Switch
                      checked={value.splitChapters}
                      onChange={(v) => set('splitChapters', v)}
                      label="Also save each chapter as its own file"
                      description="In a folder named after the video, as “01 - Chapter title”. Needs ffmpeg."
                    />
                  )}
                  {mode === 'download' && video?.isLive && (
                    <Switch
                      checked={value.liveFromStart}
                      onChange={(v) => set('liveFromStart', v)}
                      label="Record from the start"
                      description="Downloads the stream from its beginning instead of from now."
                    />
                  )}
                </div>
              </>
            )}

            {feed ? (
              <Switch
                checked={value.playlistFolder}
                onChange={(v) => set('playlistFolder', v)}
                label="Put them in a folder named after the channel or playlist"
              />
            ) : playlist || mode === 'defaults' ? (
              <div className="grid2">
                <Switch checked={value.playlistFolder} onChange={(v) => set('playlistFolder', v)} label="Put playlists in their own folder" />
                <Switch checked={value.playlistNumbering} onChange={(v) => set('playlistNumbering', v)} label="Number files by playlist position" description="01 - Title, 02 - Title…" />
                <Switch
                  checked={value.playlistFile}
                  onChange={(v) => set('playlistFile', v)}
                  label="Write a playlist file (.m3u8)"
                  description="Players such as VLC or foobar then play the folder in playlist order"
                />
                {audioFile && value.embedMetadata && (
                  <Switch
                    checked={value.playlistTags}
                    onChange={(v) => set('playlistTags', v)}
                    label="Tag the playlist as the album"
                    description="Album = the playlist's name, track number = its place in it, so music apps keep it together"
                  />
                )}
              </div>
            ) : null}

            {mode !== 'defaults' && (
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
            {mode !== 'defaults' && <TemplateField value={value.filenameTemplate} onChange={(t) => set('filenameTemplate', t)} />}
            {mode === 'download' && archiveOn && (
              <Switch
                checked={value.ignoreArchive}
                onChange={(v) => set('ignoreArchive', v)}
                label="Download again even if already downloaded"
                description={
                  feed
                    ? '“Remember downloaded videos” is on, so new uploads you already downloaded are skipped. This downloads them anyway.'
                    : '“Remember downloaded videos” is on, so videos downloaded before are skipped. This downloads them anyway.'
                }
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
  return optionTemplateProblem(o.filenameTemplate)
}

/** File names for these options: Settings' template, one of the named ones, or a custom one. */
function TemplateField({value, onChange}: {value: string; onChange: (t: string) => void}) {
  const named = value === '' || TEMPLATES.some((t) => t.value === value)
  const [custom, setCustom] = useState(!named)
  const problem = optionTemplateProblem(value)
  return (
    <Field label="File names" hint={problem ? <span style={{color: 'var(--danger)'}}>{problem}</span> : 'Leave on Settings to use the template from General settings.'}>
      <div className="stack" style={{gap: 6}}>
        <select
          value={custom ? 'custom' : value}
          onChange={(e) => {
            const v = e.target.value
            setCustom(v === 'custom')
            if (v !== 'custom') onChange(v)
          }}>
          <option value="">As in Settings</option>
          {TEMPLATES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
          <option value="custom">Custom template…</option>
        </select>
        {custom && (
          <input
            type="text"
            className="mono"
            aria-label="Custom file name template"
            aria-invalid={!!problem}
            value={value}
            placeholder="%(title)s.%(ext)s"
            onChange={(e) => onChange(e.target.value)}
            spellCheck={false}
          />
        )}
      </div>
    </Field>
  )
}
