import {memo, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {api, errMessage, on} from '../api'
import {ErrorPanel} from '../components/ErrorPanel'
import {OptionsPanel, optionsProblem} from '../components/OptionsPanel'
import {Thumb} from '../components/Thumb'
import {Button, Card, Empty, Spinner} from '../components/ui'
import {compactCount, duration, hostOf, plural, uploadDate} from '../format'
import {Icon} from '../icons'
import {useApp} from '../store'
import type {AppError, DownloadOptions, NewJob, PlaylistEntry, ProbeResult, VideoInfo} from '../types'

type Phase = 'idle' | 'analyzing' | 'ready' | 'error'

function splitLinks(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean)
}

/**
 * The name a download will get: yt-dlp's expansion of the filename template from the analysis,
 * with the extension the chosen options produce. With no container chosen yt-dlp settles the
 * extension while processing (embedding subtitles or a thumbnail can turn WebM into MKV), so
 * the extension is shown only when a container or an audio format fixes it.
 */
function plannedName(v: VideoInfo, o: DownloadOptions): string {
  if (!v.fileName) return ''
  const dot = v.fileName.lastIndexOf('.')
  const stem = dot > 0 ? v.fileName.slice(0, dot) : v.fileName
  const ext = o.preset === 'audio' ? (o.audioFormat === 'best' ? '' : o.audioFormat) : o.container
  return ext ? `${stem}.${ext}` : stem
}

function looksLikeUrl(s: string): boolean {
  return /^https?:\/\/\S+$/i.test(s.trim()) && s.length < 2000
}

/** For links like watch?v=X&list=Y that mean both "this video" and "this playlist". */
function isVideoInPlaylist(link: string): boolean {
  try {
    const u = new URL(link)
    return u.searchParams.has('v') && u.searchParams.has('list')
  } catch {
    return false
  }
}

/**
 * Drops choices that belong to the video they were made on: a time range, "download again", and
 * a format picked from its list ("custom" is only offered once a video is analysed, so it never
 * makes a default).
 */
function forNewVideo(o: DownloadOptions, defaults: DownloadOptions): DownloadOptions {
  const fallback = defaults.preset === 'custom' ? {preset: 'best' as const, formatId: ''} : {preset: defaults.preset, formatId: defaults.formatId}
  return {...o, sectionStart: '', sectionEnd: '', ignoreArchive: false, liveFromStart: false, ...(o.preset === 'custom' ? fallback : {})}
}

const PlaylistRow = memo(function PlaylistRow({entry, checked, onToggle, onOpen}: {entry: PlaylistEntry; checked: boolean; onToggle: (index: number) => void; onOpen: (url: string) => void}) {
  const off = entry.unavailable
  if (entry.isPlaylist && !off) {
    // A playlist inside the list (a channel's Playlists tab): it can't be queued as a video.
    return (
      <div className="pl-row" onClick={() => onOpen(entry.url)}>
        <Icon name="list" size={16} />
        <span className="idx">{entry.index}</span>
        <PlaylistThumb src={entry.thumbnail} />
        <div style={{minWidth: 0}}>
          <div className="ptitle" title={entry.title || entry.id}>
            {entry.title || entry.id}
          </div>
          <div className="psub truncate">Playlist</div>
        </div>
        <Button
          size="sm"
          onClick={(e) => {
            e.stopPropagation()
            onOpen(entry.url)
          }}>
          Open
        </Button>
      </div>
    )
  }
  return (
    // A label, so the whole row toggles the checkbox and assistive tech meets one control, not two.
    <label className={`pl-row${off ? ' off' : ''}`}>
      <input type="checkbox" checked={checked && !off} disabled={off} onChange={() => onToggle(entry.index)} aria-label={`Select ${entry.title || entry.id}`} />
      <span className="idx">{entry.index}</span>
      <PlaylistThumb src={entry.thumbnail} />
      <div style={{minWidth: 0}}>
        <div className="ptitle" title={entry.title || entry.id}>
            {entry.title || entry.id}
          </div>
        <div className="psub truncate">{off ? 'Unavailable (private or deleted)' : entry.uploader}</div>
      </div>
      <span className="psub">{duration(entry.duration)}</span>
    </label>
  )
})

// A thumbnail that failed to load shows the empty placeholder instead of a broken image.
function PlaylistThumb({src}: {src: string}) {
  const [failed, setFailed] = useState(false)
  if (!src || failed) return <span className="pthumb" />
  return <img className="pthumb" src={src} alt="" loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailed(true)} />
}

export function DownloadPage() {
  const {state, go, toast, attempt, setSettings} = useApp()
  const settings = state.settings
  const [text, setText] = useState('')
  const [phase, setPhase] = useState<Phase>('idle')
  const [result, setResult] = useState<ProbeResult | null>(null)
  const [probedUrl, setProbedUrl] = useState('')
  const [noPlaylist, setNoPlaylist] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const [opts, setOpts] = useState<DownloadOptions | null>(null)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [filter, setFilter] = useState('')
  const [rangeFrom, setRangeFrom] = useState('')
  const [rangeTo, setRangeTo] = useState('')
  const [adding, setAdding] = useState(false)
  const [bulkProgress, setBulkProgress] = useState<{done: number; total: number} | null>(null)
  const [clip, setClip] = useState('')
  const requestId = useRef(0)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const lastLink = useRef('')
  const defaults = settings?.defaults
  const defaultsRef = useRef(defaults)
  const seeded = useRef<DownloadOptions | null>(null)

  // Seed the form from the saved defaults, and follow later changes to them unless the user has
  // changed the form since.
  useEffect(() => {
    defaultsRef.current = defaults
    if (!defaults) return
    const fresh = forNewVideo(structuredClone(defaults), defaults)
    const base = seeded.current
    setOpts((o) => (!o || (base && JSON.stringify(o) === JSON.stringify(base)) ? fresh : o))
    seeded.current = fresh
  }, [defaults])

  const links = useMemo(() => splitLinks(text), [text])
  const bulk = links.length > 1

  // ---- clipboard suggestion ----
  const refreshClip = useCallback(async () => {
    try {
      const t = (await api.readClipboard()).trim()
      setClip(looksLikeUrl(t) ? t : '')
    } catch {
      setClip('')
    }
  }, [])
  useEffect(() => {
    void refreshClip()
    const onFocus = () => void refreshClip()
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [refreshClip])

  // ---- analysing ----
  const analyze = useCallback(
    async (raw: string, noPl = false) => {
      const link = raw.trim()
      if (!link) return
      const id = ++requestId.current
      if (link !== lastLink.current) {
        lastLink.current = link
        const d = defaultsRef.current
        if (d) setOpts((o) => (o ? forNewVideo(o, d) : o))
      }
      setPhase('analyzing')
      setError(null)
      setResult(null)
      setNoPlaylist(noPl)
      setProbedUrl(link)
      try {
        const res = await api.probe(link, noPl)
        if (id !== requestId.current) return
        if (res.cancelled) {
          // A cancel meant for the previous probe can reach this one; don't leave the spinner up.
          setPhase('idle')
          return
        }
        if (res.error || !res.result) {
          setError(res.error)
          setPhase('error')
          return
        }
        setResult(res.result)
        if (res.result.kind === 'playlist' && res.result.playlist) {
          // "Show the whole playlist" keeps the link, so a format picked for the one video would
          // otherwise be applied to every entry (and fail where they don't have it).
          const d = defaultsRef.current
          if (d) setOpts((o) => (o ? forNewVideo(o, d) : o))
          setSelected(new Set(res.result.playlist.entries.filter((e) => !e.unavailable && !e.isPlaylist).map((e) => e.index)))
          setFilter('')
          setRangeFrom('')
          setRangeTo('')
        }
        setPhase('ready')
      } catch (e) {
        if (id !== requestId.current) return
        setError({code: 'unknown', title: 'Analysis failed', message: errMessage(e), hint: '', action: '', detail: '', retryable: true})
        setPhase('error')
      }
    },
    [],
  )

  const reset = useCallback(() => {
    requestId.current++
    void api.cancelProbe()
    setPhase('idle')
    setResult(null)
    setError(null)
  }, [])

  // Adding a second link switches to the bulk form; drop the single-link review so the two
  // option panels don't show at once.
  useEffect(() => {
    if (bulk && (phase === 'ready' || phase === 'error')) reset()
  }, [bulk, phase, reset])

  // Editing the link drops the review of the old one, so Download can't queue a link that is no
  // longer in the box.
  useEffect(() => {
    if (!bulk && phase !== 'idle' && links[0] !== probedUrl) reset()
  }, [bulk, links, phase, probedUrl, reset])

  // A link handed over by a second launch of the app.
  useEffect(() => {
    if (state.incomingUrl) {
      setText(state.incomingUrl.url)
      void analyze(state.incomingUrl.url)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.incomingUrl?.n])

  const submit = useCallback(async () => {
    if (!opts || links.length === 0 || adding) return
    if (links.length === 1) {
      void analyze(links[0])
      return
    }
    // Several links: skip the review step and queue everything, expanding playlists.
    const problem = optionsProblem(opts)
    if (problem) return toast('error', problem)
    setAdding(true)
    setBulkProgress({done: 0, total: links.length})
    const off = on('bulk:progress', setBulkProgress)
    try {
      const res = await api.enqueueLinks(links, {...opts, sectionStart: '', sectionEnd: ''})
      if (res.error) return toast('error', res.error)
      if (res.added > 0 && res.failed.length === 0) {
        // Everything was queued: show it downloading. Otherwise stay, with the leftovers in the box.
        if (res.skipped > 0) toast('info', `${plural(res.skipped, 'download')} already in the queue`)
        setText('')
        go('queue')
        return
      }
      if (res.added > 0) {
        toast('success', `Added ${plural(res.added, 'download')}${res.skipped ? ` (${res.skipped} already queued)` : ''}`, {label: 'View queue', run: () => go('queue')})
      }
      // Keep only the links that weren't queued in the box, so they can be fixed and retried.
      setText(res.failed.map((f) => f.url).join('\n'))
      const failed = res.failed.filter((f) => f.error.code !== 'cancelled')
      if (res.cancelled) {
        toast('info', `Stopped. The ${plural(res.failed.length, 'link')} not added ${res.failed.length === 1 ? 'is' : 'are'} still in the box.`)
      } else if (failed.length > 0) {
        const first = failed[0]
        toast('error', `${plural(failed.length, 'link')} couldn't be added. ${hostOf(first.url)}: ${first.error.title}`)
      }
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      off()
      setAdding(false)
      setBulkProgress(null)
    }
  }, [adding, analyze, go, links, opts, toast])

  const paste = useCallback(async () => {
    const t = (await api.readClipboard()).trim()
    if (!t) return toast('info', 'The clipboard is empty')
    const next = text.trim() ? `${text.trim()}\n${t}` : t
    setText(next)
    if (splitLinks(next).length === 1 && looksLikeUrl(t)) void analyze(t)
  }, [analyze, text, toast])

  // ---- playlist selection ----
  const playlist = result?.kind === 'playlist' ? result.playlist : null
  const video = result?.kind === 'video' ? result.video : null
  const available = useMemo(() => playlist?.entries.filter((e) => !e.unavailable && !e.isPlaylist) ?? [], [playlist])
  const nestedCount = useMemo(() => playlist?.entries.filter((e) => e.isPlaylist && !e.unavailable).length ?? 0, [playlist])
  const openNested = useCallback(
    (url: string) => {
      setText(url)
      void analyze(url)
    },
    [analyze],
  )
  const visible = useMemo(() => {
    if (!playlist) return []
    const q = filter.trim().toLowerCase()
    if (!q) return playlist.entries
    return playlist.entries.filter((e) => e.title.toLowerCase().includes(q) || e.uploader.toLowerCase().includes(q))
  }, [playlist, filter])

  const toggle = useCallback((index: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }, [])
  const selectRange = () => {
    const from = parseInt(rangeFrom || '1', 10)
    const to = parseInt(rangeTo || String(playlist?.entries.length ?? 0), 10)
    if (!isFinite(from) || !isFinite(to) || from > to) return toast('error', 'Enter a valid range, like 5 to 20')
    setSelected(new Set(available.filter((e) => e.index >= from && e.index <= to).map((e) => e.index)))
  }
  const selectedEntries = useMemo(() => available.filter((e) => selected.has(e.index)), [available, selected])
  const selectedSeconds = selectedEntries.reduce((n, e) => n + (e.duration || 0), 0)

  // ---- enqueue ----
  const problem = opts ? optionsProblem(opts) : ''
  const download = async () => {
    if (!opts || !result) return
    if (problem) return toast('error', problem)
    let items: NewJob[] = []
    let options = opts
    if (video) {
      items = [{url: video.url || probedUrl, title: video.title, uploader: video.uploader, thumbnail: video.thumbnailSmall || video.thumbnail, duration: video.duration, playlistTitle: '', playlistIndex: 0, playlistCount: 0}]
    } else if (playlist) {
      options = {...opts, sectionStart: '', sectionEnd: ''}
      items = selectedEntries.map((e) => ({
        url: e.url,
        title: e.title,
        uploader: e.uploader,
        thumbnail: e.thumbnail,
        duration: e.duration,
        playlistTitle: playlist.title,
        playlistIndex: e.index,
        playlistCount: playlist.entries.length,
      }))
    }
    setAdding(true)
    try {
      const res = await api.enqueue(items, options)
      if (res.error) return toast('error', res.error)
      if (res.added === 0 && res.skipped > 0) return toast('info', 'Already in the queue')
      if (res.skipped > 0) toast('info', `${plural(res.skipped, 'download')} already in the queue`)
      // The form is cleared for next time; the Queue page shows the download starting.
      setText('')
      reset()
      go('queue')
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setAdding(false)
    }
  }

  const saveAsDefaults = async () => {
    if (!settings || !opts) return
    const res = await attempt(api.saveSettings({...settings, defaults: {...forNewVideo(opts, settings.defaults), outputDir: ''}}))
    if (!res) return
    if (res.error) return toast('error', res.error)
    setSettings(res.settings)
    toast('success', 'Saved as your default options')
  }

  const rows = playlist ? visible : []
  let lastGroup = ''

  return (
    <div className="content">
      <div className="page-head">
        <h1>Download</h1>
      </div>

      <Card>
        <div className="urlbox">
          <textarea
            ref={inputRef}
            value={text}
            rows={Math.min(5, Math.max(1, text.split('\n').length))}
            placeholder="Paste a video, playlist or channel link, or several, one per line"
            aria-label="Video, playlist or channel links"
            spellCheck={false}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void submit()
              }
            }}
            onPaste={(e) => {
              const pasted = e.clipboardData.getData('text').trim()
              if (text.trim() === '' && looksLikeUrl(pasted)) {
                e.preventDefault()
                setText(pasted)
                void analyze(pasted)
              }
            }}
          />
          <Button icon="clipboard" onClick={() => void paste()} style={{height: 'auto'}}>
            Paste
          </Button>
          <Button variant="primary" icon={bulk ? 'plus' : 'search'} loading={phase === 'analyzing' || adding} disabled={links.length === 0 || !opts} onClick={() => void submit()} style={{height: 'auto'}}>
            {bulk ? `Add ${links.length} links` : 'Analyze'}
          </Button>
        </div>
        {clip && !text.trim() && phase === 'idle' && (
          <div className="clip-suggest">
            <Button
              size="sm"
              variant="ghost"
              icon="link"
              onClick={() => {
                setText(clip)
                void analyze(clip)
              }}>
              <span className="truncate" style={{maxWidth: 520}}>
                Use copied link: {hostOf(clip)}
                <span className="muted"> {clip.replace(/^https?:\/\/(www\.)?[^/]+/, '').slice(0, 60)}</span>
              </span>
            </Button>
          </div>
        )}
        {!bulk && links.length === 1 && phase === 'idle' && <div className="hint" style={{marginTop: 8}}>Press Enter to analyze · Shift+Enter adds another link</div>}
      </Card>

      {bulkProgress && (
        <div className="row" style={{padding: '28px 4px', color: 'var(--muted)'}}>
          <Spinner />
          <span>
            Checking links: {bulkProgress.done} of {bulkProgress.total} done. Each one is queued as soon as it's checked.
          </span>
          <Button size="sm" variant="ghost" onClick={() => void api.cancelBulk()}>
            Cancel
          </Button>
        </div>
      )}
      {phase === 'analyzing' && (
        <div className="row" style={{padding: '28px 4px', color: 'var(--muted)'}}>
          <Spinner /> <span>Reading {hostOf(probedUrl)}… large playlists and channels can take a minute.</span>
          <Button size="sm" variant="ghost" onClick={reset}>
            Cancel
          </Button>
        </div>
      )}

      {phase === 'error' && error && (
        <div style={{marginTop: 16}}>
          <ErrorPanel error={error} onRetry={() => void analyze(probedUrl, noPlaylist)} />
        </div>
      )}

      {phase === 'idle' && !bulk && (
        <Empty title="Paste a link to get started">
          Works with YouTube and the 1,800+ other sites yt-dlp supports. Playlists and channels show a checklist so you can pick what to download.
        </Empty>
      )}

      {bulk && opts && phase !== 'analyzing' && (
        <Card title={`${links.length} links`} sub="Each link is checked and queued. Playlists are expanded and queued in full. Use the options below for all of them.">
          <OptionsPanel value={opts} onChange={setOpts} playlist mode="download" archiveOn={settings?.useArchive} />
          <div className="divider" />
          <Button variant="ghost" icon="save" onClick={() => void saveAsDefaults()}>
            Save these options as my defaults
          </Button>
        </Card>
      )}

      {phase === 'ready' && result && opts && (
        <div className="stack" style={{marginTop: 16}}>
          {video && (
            <Card>
              <div className="media">
                <Thumb src={video.thumbnail} seconds={video.duration} />
                <div className="media-info">
                  <h2 title={video.title}>{video.title}</h2>
                  <div className="meta">
                    {video.uploader && (
                      <span>
                        <Icon name="globe" size={13} /> {video.uploader}
                      </span>
                    )}
                    {video.uploadDate && <span>{uploadDate(video.uploadDate)}</span>}
                    {video.viewCount > 0 && <span>{compactCount(video.viewCount)} views</span>}
                    {video.heights.length > 0 && <span>up to {video.heights[0]}p</span>}
                    <span>{hostOf(video.url || probedUrl)}</span>
                  </div>
                  {video.isLive && (
                    <div className="badge warn" style={{marginTop: 8}}>
                      <Icon name="warning" size={13} /> Live stream. It records until the stream ends.
                    </div>
                  )}
                  {isVideoInPlaylist(probedUrl) && noPlaylist && (
                    <div style={{marginTop: 10}}>
                      <Button size="sm" onClick={() => void analyze(probedUrl, false)}>
                        This link is part of a playlist. Show the whole playlist
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            </Card>
          )}

          {playlist && (
            <Card
              title={playlist.title || 'Playlist'}
              sub={`${plural(available.length, 'video')}${nestedCount ? ` · ${plural(nestedCount, 'playlist')} (open one to pick its videos)` : ''}${playlist.uploader ? ` · ${playlist.uploader}` : ''}${available.length + nestedCount < playlist.entries.length ? ` · ${playlist.entries.length - available.length - nestedCount} unavailable` : ''}`}
             >
              {isVideoInPlaylist(probedUrl) && (
                <div className="banner info" style={{marginBottom: 12}}>
                  <span className="ic">
                    <Icon name="info" size={18} />
                  </span>
                  <div className="body">
                    <div className="title">This link is a video inside a playlist</div>
                    <div className="text">Choose whether you want the whole playlist or only that video.</div>
                  </div>
                  <div className="actions">
                    <Button size="sm" onClick={() => void analyze(probedUrl, true)}>
                      Just this video
                    </Button>
                  </div>
                </div>
              )}
              <div className="pl-tools">
                <Button size="sm" onClick={() => setSelected(new Set(available.map((e) => e.index)))}>
                  Select all
                </Button>
                <Button size="sm" onClick={() => setSelected(new Set())}>
                  None
                </Button>
                <Button size="sm" onClick={() => setSelected(new Set(available.filter((e) => !selected.has(e.index)).map((e) => e.index)))}>
                  Invert
                </Button>
                <span className="muted" style={{margin: '0 2px 0 8px'}}>
                  Range
                </span>
                <input type="text" className="range-in" inputMode="numeric" placeholder="1" value={rangeFrom} onChange={(e) => setRangeFrom(e.target.value.replace(/\D/g, ''))} aria-label="Range from" />
                <span className="muted">to</span>
                <input type="text" className="range-in" inputMode="numeric" placeholder={String(playlist.entries.length)} value={rangeTo} onChange={(e) => setRangeTo(e.target.value.replace(/\D/g, ''))} aria-label="Range to" />
                <Button size="sm" onClick={selectRange}>
                  Select
                </Button>
                <div style={{flex: 1}} />
                <input type="text" placeholder="Filter by title…" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter playlist" />
              </div>
              <div className="pl-list" role="group" aria-label="Playlist videos">
                {rows.length === 0 && <div className="empty" style={{padding: 24}}>{filter.trim() ? `No videos match “${filter.trim()}”.` : 'This playlist is empty.'}</div>}
                {rows.map((e) => {
                  const header = e.group && e.group !== lastGroup ? <div key={`g-${e.index}`} className="pl-group">{e.group}</div> : null
                  lastGroup = e.group
                  return (
                    <div key={e.index} style={{display: 'contents'}}>
                      {header}
                      <PlaylistRow entry={e} checked={selected.has(e.index)} onToggle={toggle} onOpen={openNested} />
                    </div>
                  )
                })}
              </div>
              <div className="hint" style={{marginTop: 8}}>
                {selectedEntries.length} of {available.length} selected{selectedSeconds > 0 ? ` · ${duration(selectedSeconds)} total` : ''}
              </div>
            </Card>
          )}

          <Card title="Options">
            <OptionsPanel value={opts} onChange={setOpts} video={video} playlist={!!playlist} mode="download" archiveOn={settings?.useArchive} />
            <div className="divider" />
            <Button variant="ghost" icon="save" onClick={() => void saveAsDefaults()}>
              Save these options as my defaults
            </Button>
          </Card>

          <div className="actionbar">
            <div className="info truncate">
              {problem ? (
                <span style={{color: 'var(--danger)'}}>{problem}</span>
              ) : playlist ? (
                `${plural(selectedEntries.length, 'video')} will be added to the queue`
              ) : video && opts && plannedName(video, opts) ? (
                <span title={`${opts.outputDir || settings?.downloadDir || ''}\\${plannedName(video, opts)}`}>
                  Saves as <b>{plannedName(video, opts)}</b>
                </span>
              ) : (
                'Ready to download'
              )}
            </div>
            <Button variant="ghost" onClick={reset}>
              Clear
            </Button>
            <Button variant="primary" size="lg" icon="download" loading={adding} disabled={!!problem || (!!playlist && selectedEntries.length === 0)} onClick={() => void download()}>
              {playlist ? `Download ${selectedEntries.length}` : 'Download'}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
