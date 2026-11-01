import {memo, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {api, errMessage, on} from '../api'
import {Banner} from '../components/Banners'
import {ErrorPanel} from '../components/ErrorPanel'
import {FeedDialog} from '../components/FeedDialog'
import {VideoGrid, selectable, useSteps} from '../components/VideoGrid'
import {OptionsPanel, optionsProblem} from '../components/OptionsPanel'
import {Thumb} from '../components/Thumb'
import {Button, Card, Empty, Field, Modal, Segmented, Spinner} from '../components/ui'
import {compactCount, duration, hostOf, plural, startsAt, uploadDate} from '../format'
import {Icon} from '../icons'
import {forPreset, parseSites, presetFor, presetOptions, sameOptions, switchTo} from '../presets'
import {useApp} from '../store'
import type {AppError, BulkLink, ChannelProfile, DownloadOptions, NamedPreset, NewJob, Peek, PlaylistEntry, ProbeResult, VideoInfo} from '../types'

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
  // The analysis names the file with Settings' template; a preset's own isn't known until then.
  if (!v.fileName || o.filenameTemplate) return ''
  const dot = v.fileName.lastIndexOf('.')
  const stem = dot > 0 ? v.fileName.slice(0, dot) : v.fileName
  const ext = o.preset === 'audio' ? (o.audioFormat === 'best' ? '' : o.audioFormat) : o.preset === 'custom' && o.formatAudioOnly ? '' : o.container
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
  const fallback = defaults.preset === 'custom' ? {preset: 'best' as const, formatId: '', formatAudioOnly: false} : {preset: defaults.preset, formatId: defaults.formatId, formatAudioOnly: false}
  return {...o, sectionStart: '', sectionEnd: '', chapters: [], ignoreArchive: false, liveFromStart: false, waitForVideo: false, audioLang: '', ...(o.preset === 'custom' ? fallback : {})}
}

type View = 'grid' | 'list'
const VIEW_KEY = 'prem-dl.playlistView'

// Remembered per computer; storage can be unavailable, and then the grid is the default.
function savedView(): View {
  try {
    return localStorage.getItem(VIEW_KEY) === 'list' ? 'list' : 'grid'
  } catch {
    return 'grid'
  }
}

const NONE: PlaylistEntry[] = []
const entryIndex = (e: PlaylistEntry) => e.index

const PlaylistRow = memo(function PlaylistRow({entry, checked, onToggle, onOpen}: {entry: PlaylistEntry; checked: boolean; onToggle: (index: number) => void; onOpen: (url: string) => void}) {
  if (entry.isPlaylist && !entry.unavailable) {
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
  const off = !selectable(entry)
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
        <div className="psub truncate">{entry.unavailable ? 'Unavailable (private or deleted)' : entry.liveStatus === 'is_upcoming' ? 'Upcoming stream' : entry.uploader}</div>
      </div>
      <span className="psub">{duration(entry.duration)}</span>
    </label>
  )
})

// A thumbnail that failed to load shows the empty placeholder instead of a broken image. Rows are
// keyed by position, so the failure is remembered per URL: the next playlist's image in this row
// still loads.
function PlaylistThumb({src}: {src: string}) {
  const [failedSrc, setFailedSrc] = useState('')
  if (!src || failedSrc === src) return <span className="pthumb" />
  return <img className="pthumb" src={src} alt="" loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailedSrc(src)} />
}

/** Picks the preset the form starts from; '' is the default options. */
function PresetSelect({presets, value, onChange}: {presets: NamedPreset[]; value: string; onChange: (name: string) => void}) {
  if (presets.length === 0) return null
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} aria-label="Preset" title="Preset" style={{width: 'auto', minWidth: 170}}>
      <option value="">Default options</option>
      {presets.map((p) => (
        <option key={p.name} value={p.name}>
          {p.name}
        </option>
      ))}
    </select>
  )
}

/** Names the form's options and saves them as a new preset, or over the one with that name. */
function SavePresetDialog({presets, initialName, host, onClose, onSave}: {presets: NamedPreset[]; initialName: string; host: string; onClose: () => void; onSave: (name: string, sites: string[]) => Promise<boolean>}) {
  const [name, setName] = useState(initialName)
  // Until edited, the sites shown are those of the preset the name matches.
  const [sitesText, setSitesText] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const match = presets.find((p) => p.name.toLowerCase() === name.trim().toLowerCase())
  const sites = sitesText ?? match?.sites.join(', ') ?? ''
  const save = async () => {
    if (!name.trim() || busy) return
    setBusy(true)
    const ok = await onSave(name.trim(), parseSites(sites))
    setBusy(false)
    if (ok) onClose()
  }
  return (
    <Modal
      dismissable={false}
      title="Save as preset"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" loading={busy} disabled={!name.trim()} onClick={() => void save()}>
            {match ? `Replace ${match.name}` : 'Save preset'}
          </Button>
        </>
      }>
      <form
        className="stack"
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}>
        <Field label="Name" hint={match ? `This replaces the options saved in ${match.name}.` : 'Pick it from the preset list on the Download page.'}>
          <input type="text" autoFocus maxLength={40} value={name} placeholder="Music, Archive, Podcasts…" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Use it for links from" hint="Optional. Links from these sites (and their subdomains) pick this preset by themselves. Separate several with commas.">
          <input type="text" value={sites} placeholder={host ? `e.g. ${host}` : 'e.g. music.youtube.com'} spellCheck={false} onChange={(e) => setSitesText(e.target.value)} />
        </Field>
        {/* With two fields, Enter only submits when the form has a submit button. */}
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}

export function DownloadPage() {
  const {state, go, toast, attempt, setSettings} = useApp()
  const settings = state.settings
  const [text, setText] = useState('')
  const root = useRef<HTMLDivElement>(null)
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
  // A channel or playlist shows its first page at once; its other entries are read meanwhile.
  const [reading, setReading] = useState(false)
  const [readError, setReadError] = useState('')
  const [clip, setClip] = useState('')
  const [following, setFollowing] = useState(false)
  const [view, setView] = useState<View>(savedView)
  /** The analysed playlist is already followed. */
  const [followed, setFollowed] = useState(false)
  const requestId = useRef(0)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const readyDownload = useRef<() => Promise<void>>(() => Promise.resolve())
  const [peek, setPeek] = useState<Peek | null>(null)
  const lastLink = useRef('')
  const defaults = settings?.defaults
  const defaultsRef = useRef(defaults)
  const seeded = useRef<DownloadOptions | null>(null)
  /** The preset the form started from; '' is the defaults. */
  const [presetName, setPresetName] = useState('')
  /** The site whose link picked the preset; '' when it was chosen by hand. */
  const [auto, setAuto] = useState('')
  const [savingPreset, setSavingPreset] = useState(false)
  const presets = useMemo(() => settings?.presets ?? [], [settings?.presets])
  const chosen = presetName ? presets.find((p) => p.name === presetName) : undefined
  const base = useMemo(() => (chosen ? presetOptions(chosen) : defaults ? forNewVideo(structuredClone(defaults), defaults) : null), [chosen, defaults])
  const changed = !!opts && !!base && !sameOptions(opts, base)
  // The latest of each, for callbacks that run later (analysing a link).
  const latest = useRef({presets, presetName, base, auto, opts})
  latest.current = {presets, presetName, base, auto, opts}

  // A preset deleted or renamed in Settings: back to the defaults.
  useEffect(() => {
    if (presetName && settings && !chosen) {
      setPresetName('')
      setAuto('')
    }
  }, [presetName, settings, chosen])

  // Seed the form from the chosen preset or the saved defaults, and follow later changes to them
  // unless the user has changed the form since.
  useEffect(() => {
    defaultsRef.current = defaults
    if (!base) return
    const prev = seeded.current
    setOpts((o) => (!o || (prev && JSON.stringify(o) === JSON.stringify(prev)) ? base : o))
    seeded.current = base
  }, [base, defaults])

  const choosePreset = (name: string) => {
    const p = presets.find((x) => x.name === name)
    const next = p ? presetOptions(p) : defaults ? forNewVideo(structuredClone(defaults), defaults) : null
    if (!next) return
    setPresetName(p ? p.name : '')
    setAuto('')
    setOpts((o) => switchTo(next, o))
  }

  // The link whose site picked the preset is gone: back to the defaults, unless the form was
  // changed since (then it is the user's choice). Reports whether the form was reset.
  const dropAuto = useCallback(() => {
    const {base, auto, opts} = latest.current
    const d = defaultsRef.current
    if (!auto) return false
    setAuto('')
    if (!d || !opts || !base || !sameOptions(opts, base)) return false
    setPresetName('')
    setOpts(forNewVideo(structuredClone(d), d))
    return true
  }, [])

  // A new link: its site's preset if it has one, unless that preset is already on the form.
  const pickFor = useCallback(
    (link: string) => {
      const d = defaultsRef.current
      const m = presetFor(link, latest.current.presets)
      if (m && m.preset.name !== latest.current.presetName) {
        setPresetName(m.preset.name)
        setAuto(m.site)
        setOpts(presetOptions(m.preset))
        return
      }
      if (m) setAuto(m.site)
      else if (dropAuto()) return
      if (d) setOpts((o) => (o ? forNewVideo(o, d) : o))
    },
    [dropAuto],
  )

  const links = useMemo(() => splitLinks(text), [text])
  const bulk = links.length > 1

  // Several links: site presets apply link by link, so one link's pick doesn't stay on the form
  // for the rest. Back at one link, it is picked again.
  useEffect(() => {
    if (!bulk) return
    lastLink.current = ''
    dropAuto()
  }, [bulk, dropAuto])

  /** Links in the box whose site picks a preset other than the form's, counted per preset. */
  const bySite = useMemo(() => {
    const counts = new Map<string, number>()
    if (!bulk) return counts
    for (const l of links) {
      const m = presetFor(l, presets)
      if (m && m.preset.name !== presetName) counts.set(m.preset.name, (counts.get(m.preset.name) ?? 0) + 1)
    }
    return counts
  }, [bulk, links, presets, presetName])
  const bySiteCount = [...bySite.values()].reduce((a, b) => a + b, 0)

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
  /**
   * Reads the rest of a listing the probe answered with its first page, adding entries at the end
   * as they come. A playlist opens with everything ticked, so its new entries are ticked too,
   * unless the ticks were changed meanwhile; a channel's stay unticked.
   */
  const readRest = useCallback(async (listing: number, id: number, tickNew: boolean, loaded: number) => {
    setReading(true)
    setReadError('')
    try {
      for (;;) {
        const part = await api.moreEntries(listing)
        if (id !== requestId.current) return
        if (part.entries.length > 0) {
          setResult((r) => (r?.playlist ? {...r, playlist: {...r.playlist, entries: [...r.playlist.entries, ...part.entries]}} : r))
          const fresh = part.entries.filter(selectable).map((e) => e.index)
          if (tickNew && fresh.length > 0) {
            const before = loaded
            setSelected((prev) => (prev.size === before ? new Set([...prev, ...fresh]) : prev))
          }
          loaded += fresh.length
        }
        if (part.done) {
          if (part.error) setReadError(part.error)
          return
        }
      }
    } catch (e) {
      if (id === requestId.current) setReadError(errMessage(e))
    } finally {
      if (id === requestId.current) setReading(false)
    }
  }, [])

  const analyze = useCallback(
    async (raw: string, noPl = false) => {
      const link = raw.trim()
      if (!link) return
      const id = ++requestId.current
      if (link !== lastLink.current) {
        lastLink.current = link
        pickFor(link)
      }
      setPhase('analyzing')
      setError(null)
      setResult(null)
      setPeek(null)
      // A YouTube video's title and thumbnail come back in a fraction of the analysis' time.
      void api
        .peek(link)
        .then((p) => id === requestId.current && p.ok && setPeek(p.peek))
        .catch(() => {})
      setReading(false)
      setReadError('')
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
          // A playlist opens with everything ticked; a channel with nothing, or Download would take
          // all it ever posted.
          const pl = res.result.playlist
          const ticked = pl.channel ? [] : pl.entries.filter(selectable).map((e) => e.index)
          setSelected(new Set(ticked))
          setFilter('')
          setRangeFrom('')
          setRangeTo('')
          if (res.listing) void readRest(res.listing, id, !pl.channel, ticked.length)
        }
        setPhase('ready')
      } catch (e) {
        if (id !== requestId.current) return
        setError({code: 'unknown', title: 'Analysis failed', message: errMessage(e), hint: '', action: '', detail: '', retryable: true})
        setPhase('error')
      }
    },
    [pickFor, readRest],
  )

  const reset = useCallback(() => {
    // Back to the top before the page shrinks to the link box. WebKitGTK can leave a page
    // scrolled with the wheel showing where it was, below everything, once its content is
    // gone: the window went blank after downloading from far down a long profile.
    root.current?.closest('.page')?.scrollTo(0, 0)
    requestId.current++
    void api.cancelProbe()
    setPhase('idle')
    setResult(null)
    setError(null)
    setReading(false)
    setReadError('')
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

  // Links handed over by the Chrome helper or a second launch, or dropped on the window. One is
  // analysed straight away; several fill the box, ready to add.
  // A playlist with videos ticked isn't replaced unasked: the new link waits in a notice.
  const [held, setHeld] = useState<string[] | null>(null)
  const take = (links: string[]) => {
    setHeld(null)
    setText(links.join('\n'))
    if (links.length === 1) void analyze(links[0])
  }
  useEffect(() => {
    const links = state.incoming?.links
    if (!links || links.length === 0) return
    if (phase === 'ready' && playlist && selected.size > 0 && !(links.length === 1 && links[0] === probedUrl)) return setHeld(links)
    take(links)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.incoming?.n])

  /** After queuing: stay here, ready for the next link, with a way to the queue. */
  const added = (text: string) => {
    toast('success', text, {label: 'View queue', run: () => go('queue')})
    inputRef.current?.focus({preventScroll: true})
  }

  // The link box has the focus whenever the page is shown with nothing analysed, so a paste lands.
  useEffect(() => {
    if (state.page === 'download' && phase === 'idle') inputRef.current?.focus({preventScroll: true})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.page])

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
      // A link whose site has a preset of its own is queued with that; the rest with the form.
      const rest = {...opts, sectionStart: '', sectionEnd: '', chapters: []}
      const items: BulkLink[] = links.map((url) => {
        const m = presetFor(url, presets)
        return {url, options: m && m.preset.name !== presetName ? presetOptions(m.preset) : rest}
      })
      const res = await api.enqueueLinks(items)
      if (res.error) return toast('error', res.error)
      if (res.added > 0 && res.failed.length === 0) {
        // Everything was queued: show it downloading. Otherwise stay, with the leftovers in the box.
        setText('')
        added(`Added ${plural(res.added, 'download')}${res.skipped ? ` (${res.skipped} already queued)` : ''}`)
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
  }, [adding, analyze, go, links, opts, presetName, presets, toast])

  const paste = useCallback(async () => {
    const read = await attempt(api.readClipboard())
    if (read === undefined) return
    const t = read.trim()
    if (!t) return toast('info', 'The clipboard is empty')
    const next = text.trim() ? `${text.trim()}\n${t}` : t
    setText(next)
    if (splitLinks(next).length === 1 && looksLikeUrl(t)) void analyze(t)
  }, [analyze, attempt, text, toast])

  // ---- playlist selection ----
  const playlist = result?.kind === 'playlist' ? result.playlist : null
  const video = result?.kind === 'video' ? result.video : null
  const available = useMemo(() => playlist?.entries.filter(selectable) ?? [], [playlist])
  // A playlist mixes channels, so its grid names each video's; a channel's doesn't need to.
  const mixed = useMemo(() => new Set(playlist?.entries.map((e) => e.uploader).filter(Boolean)).size > 1, [playlist])
  const changeView = (v: View) => {
    setView(v)
    try {
      localStorage.setItem(VIEW_KEY, v)
    } catch {
      /* not remembered */
    }
  }
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
  // Numbers are padded to the playlist's length ("001 - "), which is known once it is all read.
  const waitsForCount = !!playlist && reading && !!opts?.playlistNumbering
  const download = async () => {
    if (!opts || !result || waitsForCount) return
    if (problem) return toast('error', problem)
    let items: NewJob[] = []
    let options = opts
    if (video) {
      if (video.upcoming) options = {...opts, waitForVideo: true}
      items = [{url: video.url || probedUrl, title: video.title, uploader: video.uploader, thumbnail: video.thumbnailSmall || video.thumbnail, duration: video.duration, playlistTitle: '', playlistIndex: 0, playlistCount: 0}]
    } else if (playlist) {
      options = {...opts, sectionStart: '', sectionEnd: '', chapters: []}
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
      // The form is cleared for the next link; the toast leads to the queue.
      setText('')
      reset()
      added(
        video
          ? `Added ${video.title || 'the video'} to the queue`
          : `Added ${plural(res.added, 'video')} to the queue${res.skipped ? ` (${res.skipped} already there)` : ''}`,
      )
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setAdding(false)
    }
  }

  // Enter in the link box and Ctrl+Enter anywhere on the page press Download once it shows.
  const canDownload = phase === 'ready' && !!result && !!opts && !problem && !waitsForCount && !adding && (!playlist || selectedEntries.length > 0)
  readyDownload.current = () => (canDownload ? download() : Promise.resolve())
  useEffect(() => {
    if (state.page !== 'download') return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Enter' || !e.ctrlKey || e.altKey || e.shiftKey || document.querySelector('[aria-modal]')) return
      e.preventDefault()
      void readyDownload.current()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [state.page])

  // A scheduled stream whose details couldn't be read: queued under its link, which the title
  // replaces once it starts.
  const waitFor = async (url: string) => {
    if (!opts) return
    if (problem) return toast('error', problem)
    const res = await attempt(api.enqueue([{url, title: '', uploader: '', thumbnail: '', duration: 0, playlistTitle: '', playlistIndex: 0, playlistCount: 0}], {...opts, waitForVideo: true}))
    if (!res) return
    if (res.error) return toast('error', res.error)
    if (res.added === 0) return toast('info', 'Already in the queue')
    setText('')
    reset()
    added('Added to the queue. It waits for the stream to start.')
  }

  const saveAsDefaults = async () => {
    if (!settings || !opts) return
    const res = await attempt(api.saveSettings({...settings, defaults: {...forNewVideo(opts, settings.defaults), outputDir: '', filenameTemplate: ''}}))
    if (!res) return
    if (res.error) return toast('error', res.error)
    setSettings(res.settings)
    toast('success', 'Saved as your default options')
  }

  const savePreset = async (name: string, sites: string[]): Promise<boolean> => {
    if (!settings || !opts) return false
    const preset: NamedPreset = {name, sites, options: forPreset(opts)}
    const i = settings.presets.findIndex((p) => p.name.toLowerCase() === name.toLowerCase())
    const list = i >= 0 ? settings.presets.map((p, j) => (j === i ? preset : p)) : [...settings.presets, preset]
    const res = await attempt(api.saveSettings({...settings, presets: list}))
    if (!res) return false
    if (res.error) {
      toast('error', res.error)
      return false
    }
    setSettings(res.settings)
    setPresetName(name)
    setAuto('')
    toast('success', i >= 0 ? `Updated the ${name} preset` : `Saved the ${name} preset`)
    return true
  }

  const presetNote = changed ? (chosen ? `${chosen.name}, changed for this download` : '') : auto && chosen ? `${chosen.name}, picked for ${auto} links` : ''
  const saveButtons = (
    <div className="row wrap">
      <Button variant="ghost" icon="save" onClick={() => setSavingPreset(true)}>
        Save as preset…
      </Button>
      <Button variant="ghost" icon="save" onClick={() => void saveAsDefaults()}>
        Save these options as my defaults
      </Button>
    </div>
  )

  // Whether the analysed playlist or channel is followed, so the button can say so. Feeds changing
  // (followed from the dialog, or unfollowed) asks again.
  const playlistUrl = playlist ? probedUrl : ''
  useEffect(() => {
    setFollowed(false)
    if (!playlistUrl) return
    let alive = true
    api
      .inspectFeed(playlistUrl)
      .then((res) => alive && setFollowed(!!res.target.followed))
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [playlistUrl, state.feeds])

  const rows = playlist ? visible : []
  const listRows = useSteps(view === 'list' ? rows : NONE, entryIndex)
  let lastGroup = ''

  return (
    <div className="content" ref={root}>
      <div className="page-head">
        <h1>Download</h1>
      </div>

      <Card>
        <div className="urlbox">
          <textarea
            ref={inputRef}
            value={text}
            placeholder="Paste a video, playlist or channel link, or several, one per line"
            aria-label="Video, playlist or channel links"
            spellCheck={false}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey) {
                e.preventDefault()
                // Analysed already: Enter again downloads it.
                if (phase === 'ready' && links[0] === probedUrl && !bulk) void readyDownload.current()
                else void submit()
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
        {!bulk && links.length === 1 && phase === 'idle' && <div className="hint" style={{marginTop: 8}}>Press Enter to analyze, then Enter again to download · Shift+Enter adds another link</div>}
      </Card>

      {held && (
        <Banner
          tone="info"
          icon="link"
          title={held.length === 1 ? `A new link arrived: ${hostOf(held[0])}` : `${held.length} new links arrived`}
          actions={
            <>
              <Button size="sm" variant="primary" onClick={() => take(held)}>
                Open {held.length === 1 ? 'it' : 'them'}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setHeld(null)}>
                Keep this playlist
              </Button>
            </>
          }>
          Opening {held.length === 1 ? 'it' : 'them'} clears the {plural(selected.size, 'video')} ticked here.
        </Banner>
      )}

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
      {phase === 'analyzing' && peek && (
        <Card>
          <div className="peek">
            <Thumb src={peek.thumbnail} seconds={0} className="thumb" />
            <div style={{minWidth: 0}}>
              <div className="jtitle">{peek.title}</div>
              <div className="muted">{peek.uploader}</div>
              <div className="row" style={{marginTop: 10, color: 'var(--muted)'}}>
                <Spinner /> <span>Reading its formats…</span>
                <Button size="sm" variant="ghost" onClick={reset}>
                  Cancel
                </Button>
              </div>
            </div>
          </div>
        </Card>
      )}
      {phase === 'analyzing' && !peek && (
        <div className="row" style={{padding: '28px 4px', color: 'var(--muted)'}}>
          <Spinner /> <span>Reading {hostOf(probedUrl)}… large playlists and channels can take a minute.</span>
          <Button size="sm" variant="ghost" onClick={reset}>
            Cancel
          </Button>
        </div>
      )}

      {phase === 'error' && error && (
        <div style={{marginTop: 16}}>
          <ErrorPanel error={error} onRetry={() => void analyze(probedUrl, noPlaylist)} onWait={() => void waitFor(probedUrl)} />
        </div>
      )}

      {phase === 'idle' && !bulk && (
        <Empty title="Paste a link to get started">
          Works with YouTube and 1,800+ other sites. Playlists and channels show a checklist so you can pick what to download. You can also drop links, or a
          .txt file of them, anywhere on the window.
        </Empty>
      )}

      {bulk && opts && phase !== 'analyzing' && (
        <Card
          title={`${links.length} links`}
          sub={
            bySiteCount === 0
              ? 'Each link is checked and queued. Playlists are expanded and queued in full. Use the options below for all of them.'
              : `Each link is checked and queued, playlists in full. ${[...bySite].map(([name, n]) => `${plural(n, 'link')} ${n === 1 ? 'uses' : 'use'} the ${name} preset`).join(', ')} for ${bySiteCount === 1 ? 'its' : 'their'} site; ${bySiteCount === links.length ? 'none use the options below' : `the options below are for the other ${links.length - bySiteCount}`}.`
          }
          actions={<PresetSelect presets={presets} value={presetName} onChange={choosePreset} />}>
          <OptionsPanel value={opts} onChange={setOpts} playlist mode="download" archiveOn={settings?.useArchive} />
          <div className="divider" />
          {saveButtons}
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
                    {video.photos > 0 && <span>{plural(video.photos, 'picture')} and a sound</span>}
                    <span>{hostOf(video.url || probedUrl)}</span>
                  </div>
                  {video.isLive && (
                    <div className="badge warn" style={{marginTop: 8}}>
                      <Icon name="warning" size={13} /> Live stream. It records until the stream ends.
                    </div>
                  )}
                  {video.upcoming && (
                    <>
                      <div className="badge warn" style={{marginTop: 8}}>
                        <Icon name="clock" size={13} /> Not live yet{video.releaseTimestamp ? `. Starts ${startsAt(video.releaseTimestamp)}` : ''}
                      </div>
                      <div className="hint" style={{marginTop: 6}}>
                        Wait for it queues it now and downloads it when it starts, as long as Prem-dl is running (it can wait in the tray).
                      </div>
                    </>
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
              title={playlist.profile ? <ProfileName profile={playlist.profile} /> : playlist.title || 'Playlist'}
              sub={
                <>
                  {playlist.profile && `@${playlist.profile.handle} · `}
                  {`${playlist.profile ? plural(available.length, 'post') : plural(available.length, 'video')}${playlist.profile && playlist.profile.videos > available.length ? ` of ${compactCount(playlist.profile.videos)}` : ''}${nestedCount ? ` · ${plural(nestedCount, 'playlist')} (open one to pick its videos)` : ''}${playlist.uploader && !playlist.profile ? ` · ${playlist.uploader}` : ''}${available.length + nestedCount < playlist.entries.length ? ` · ${playlist.entries.length - available.length - nestedCount} unavailable` : ''}`}
                  {reading && (
                    <span className="reading-more">
                      {' · '}
                      <Spinner /> reading more…
                    </span>
                  )}
                  {readError && <span className="err-text">{` · couldn’t read the rest: ${readError}`}</span>}
                </>
              }
              actions={
                available.length === 0 && nestedCount > 0 ? null : followed ? (
                  <Button size="sm" variant="ghost" icon="check" onClick={() => go('following')} title="Open the Following page">
                    Following
                  </Button>
                ) : (
                  <Button size="sm" icon="bell" onClick={() => setFollowing(true)} title="Download new uploads automatically">
                    Follow…
                  </Button>
                )
              }>
              {playlist.profile && <ProfileHeader profile={playlist.profile} />}
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
                <Segmented
                  label="Show as"
                  value={view}
                  onChange={changeView}
                  options={[
                    {value: 'grid', label: <Icon name="grid" size={14} />, title: 'Grid of thumbnails', ariaLabel: 'Grid'},
                    {value: 'list', label: <Icon name="list" size={14} />, title: 'Compact list', ariaLabel: 'List'},
                  ]}
                />
              </div>
              {view === 'grid' ? (
                <div className="vgrid-scroll">
                  {rows.length === 0 ? (
                    <div className="empty" style={{padding: 24}}>{filter.trim() ? `No videos match “${filter.trim()}”.` : 'This playlist is empty.'}</div>
                  ) : (
                    <VideoGrid entries={rows} selected={selected} setSelected={setSelected} onOpen={openNested} uploader={mixed} square={playlist.portrait} />
                  )}
                </div>
              ) : (
                <div className="pl-list" role="group" aria-label="Playlist videos">
                  {rows.length === 0 && <div className="empty" style={{padding: 24}}>{filter.trim() ? `No videos match “${filter.trim()}”.` : 'This playlist is empty.'}</div>}
                  {listRows.shown.map((e) => {
                    const header = e.group && e.group !== lastGroup ? <div key={`g-${e.index}`} className="pl-group">{e.group}</div> : null
                    lastGroup = e.group
                    return (
                      <div key={e.index} style={{display: 'contents'}}>
                        {header}
                        <PlaylistRow entry={e} checked={selected.has(e.index)} onToggle={toggle} onOpen={openNested} />
                      </div>
                    )
                  })}
                  {listRows.sentinel}
                </div>
              )}
              <div className="hint" style={{marginTop: 8}}>
                {selectedEntries.length} of {available.length} selected{selectedSeconds > 0 ? ` · ${duration(selectedSeconds)} total` : ''}
              </div>
            </Card>
          )}

          <Card title="Options" sub={presetNote || undefined} actions={<PresetSelect presets={presets} value={presetName} onChange={choosePreset} />}>
            <OptionsPanel value={opts} onChange={setOpts} video={video} playlist={!!playlist} mode="download" archiveOn={settings?.useArchive} />
            <div className="divider" />
            {saveButtons}
          </Card>

          {following && playlist && (
            <FeedDialog
              link={{url: probedUrl, title: playlist.title, uploader: playlist.uploader}}
              options={{...opts, sectionStart: '', sectionEnd: '', chapters: []}}
              onClose={() => setFollowing(false)}
            />
          )}

          <div className="actionbar">
            <div className="info truncate">
              {problem ? (
                <span style={{color: 'var(--danger)'}}>{problem}</span>
              ) : waitsForCount && selectedEntries.length > 0 ? (
                'Numbered files wait until the whole list is read…'
              ) : playlist ? (
                selectedEntries.length === 0 ? (
                  'Tick the videos you want, or Select all'
                ) : (
                  `${plural(selectedEntries.length, 'video')} will be added to the queue`
                )
              ) : video && video.photos > 0 ? (
                opts.preset === 'audio' || (opts.preset === 'custom' && opts.formatAudioOnly) ? (
                  'Saves the post’s sound'
                ) : (
                  `${plural(video.photos, 'picture')} and the sound, in a folder named after the post`
                )
              ) : video && opts.chapters.length > 0 ? (
                `${plural(opts.chapters.length, 'chapter')}, each saved as its own file`
              ) : video && opts && plannedName(video, opts) ? (
                <span title={`${opts.outputDir || settings?.downloadDir || ''}${state.platform === 'windows' ? '\\' : '/'}${plannedName(video, opts)}`}>
                  Saves as <b>{plannedName(video, opts)}</b>
                </span>
              ) : (
                'Ready to download'
              )}
            </div>
            <Button
              variant="ghost"
              onClick={() => {
                setText('')
                reset()
              }}>
              Clear
            </Button>
            <Button variant="primary" size="lg" icon={video?.upcoming ? 'clock' : 'download'} loading={adding} disabled={!!problem || waitsForCount || (!!playlist && selectedEntries.length === 0)} title="Ctrl+Enter" aria-keyshortcuts="Control+Enter" onClick={() => void download()}>
              {playlist ? `Download ${selectedEntries.length}` : video?.upcoming ? 'Wait for it' : 'Download'}
            </Button>
          </div>
        </div>
      )}

      {savingPreset && opts && (
        <SavePresetDialog presets={presets} initialName={presetName} host={probedUrl && !bulk ? hostOf(probedUrl) : ''} onClose={() => setSavingPreset(false)} onSave={savePreset} />
      )}
    </div>
  )
}

/** An account's name as its page shows it, with the verified mark. */
function ProfileName({profile}: {profile: ChannelProfile}) {
  return (
    <span className="profile-name">
      {profile.name || `@${profile.handle}`}
      {profile.verified && (
        <span className="profile-verified" title="Verified account" aria-label="Verified account">
          <Icon name="check" size={10} />
        </span>
      )}
    </span>
  )
}

/** What an account's page shows above its videos: picture, counts and bio. */
function ProfileHeader({profile}: {profile: ChannelProfile}) {
  const [broken, setBroken] = useState(false)
  return (
    <div className="profile-head">
      {profile.avatar && !broken ? (
        <img className="profile-avatar" src={profile.avatar} alt="" referrerPolicy="no-referrer" onError={() => setBroken(true)} />
      ) : (
        <div className="profile-avatar" aria-hidden="true" />
      )}
      <div className="profile-about">
        <dl className="profile-stats">
          {(
            [
              [profile.followers, 'Followers'],
              [profile.following, 'Following'],
              [profile.likes, 'Likes'],
            ] as const
          ).map(([n, label]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{compactCount(n)}</dd>
            </div>
          ))}
        </dl>
        {profile.bio && <p className="profile-bio">{profile.bio}</p>}
      </div>
    </div>
  )
}
