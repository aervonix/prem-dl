import {memo, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {CSSProperties, Dispatch, MouseEvent, ReactNode, SetStateAction} from 'react'
import {ago, compactCount, duration} from '../format'
import {Icon} from '../icons'
import {useJobs} from '../store'
import type {JobStatus, PlaylistEntry} from '../types'
import {Button} from './ui'

/** Whether an entry can be ticked: not private or deleted, not a playlist, not a stream yet to start. */
export function selectable(e: PlaylistEntry): boolean {
  return !e.unavailable && !e.isPlaylist && e.liveStatus !== 'is_upcoming'
}

// What a video's jobs say about it; when one link has several jobs, the busiest wins.
const STATUS: Partial<Record<JobStatus, [rank: number, text: string]>> = {
  completed: [1, 'Downloaded'],
  queued: [2, 'In queue'],
  paused: [2, 'In queue'],
  starting: [3, 'Downloading'],
  downloading: [3, 'Downloading'],
  processing: [3, 'Downloading'],
}

/** A VideoGrid status for each video from the queue: "Downloaded", "In queue"... or ''. */
export function useJobStatus(): (e: PlaylistEntry) => string {
  const jobs = useJobs()
  const last = useRef(new Map<string, [number, string]>())
  const statusByUrl = useMemo(() => {
    const m = new Map<string, [number, string]>()
    for (const j of jobs) {
      const s = STATUS[j.status]
      if (s && s[0] > (m.get(j.url)?.[0] ?? 0)) m.set(j.url, s)
    }
    // Jobs change several times a second while downloading, the words rarely: keep the same
    // map (and so the same function) until one does, or every card would be looked at again.
    const prev = last.current
    if (m.size === prev.size && [...m].every(([url, s]) => prev.get(url) === s)) return prev
    return (last.current = m)
  }, [jobs])
  return useCallback((e: PlaylistEntry) => statusByUrl.get(e.url)?.[1] ?? '', [statusByUrl])
}

/** Whether a link is a YouTube channel rather than a playlist (search results list both). */
function isChannelLink(url: string): boolean {
  return /youtube\.com\/(channel\/|@|c\/|user\/)/.test(url)
}

/** "110K views · 3 days ago", from what the listing says. */
function metaLine(e: PlaylistEntry, uploader: boolean): string {
  return [uploader ? e.uploader : '', e.viewCount ? `${compactCount(e.viewCount)} views` : '', ago(e.timestamp)].filter(Boolean).join(' · ')
}

// Cards past this many in a batch fade in with the last of these; they are below the fold.
const REVEAL_MAX = 15

// A big channel lists thousands of videos. Cards are made this many at a time, the next lot as
// the end of the last comes near: 8,000 cards at once took 1.5 s to show and 0.7 s to Select all.
const STEP = 240

/** The nearest ancestor that scrolls, which an IntersectionObserver has to watch from. */
function scroller(el: HTMLElement | null): HTMLElement | null {
  for (let p = el?.parentElement; p; p = p.parentElement) {
    const y = getComputedStyle(p).overflowY
    if (y === 'auto' || y === 'scroll') return p
  }
  return null
}

const Card = memo(function Card({
  entry,
  checked,
  status,
  uploader,
  onPick,
  onOpen,
  onOpenVideo,
  reveal,
}: {
  entry: PlaylistEntry
  checked: boolean
  status: string
  uploader: boolean
  onPick: (index: number, range: boolean) => void
  onOpen?: (url: string) => void
  onOpenVideo?: (url: string) => void
  /** Its place in the batch that brought it, which delays its fade-in; -1 shows it at once. */
  reveal: number
}) {
  const [failedSrc, setFailedSrc] = useState('')
  const e = entry
  const playlist = e.isPlaylist && !e.unavailable
  const can = selectable(e)
  const pick = (ev: MouseEvent) => {
    if (playlist) return onOpen?.(e.url)
    if (can) onPick(e.index, ev.shiftKey)
  }
  const title = e.title || e.id

  return (
    <div
      className={`vcard${checked ? ' on' : ''}${!can && !playlist ? ' off' : ''}${reveal >= 0 ? ' reveal' : ''}`}
      style={reveal >= 0 ? ({'--reveal': Math.min(reveal, REVEAL_MAX)} as CSSProperties) : undefined}
      onClick={pick}
      // Two clicks tick and untick it again, so the ticks stay as they were.
      onDoubleClick={can && onOpenVideo ? () => onOpenVideo(e.url) : undefined}
      title={onOpenVideo && can ? `${title}\nDouble-click to choose its options` : title}>
      <div className="vthumb">
        {e.thumbnail && failedSrc !== e.thumbnail ? (
          <img src={e.thumbnail} alt="" loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailedSrc(e.thumbnail)} />
        ) : (
          <Icon name={playlist ? 'list' : 'film'} size={26} />
        )}
        {e.liveStatus === 'is_live' && <span className="vtag live">LIVE</span>}
        {e.liveStatus === 'is_upcoming' && <span className="vtag">Upcoming</span>}
        {playlist && <span className="vtag">{isChannelLink(e.url) ? 'Channel' : 'Playlist'}</span>}
        {e.duration > 0 && <span className="dur">{duration(e.duration)}</span>}
        {can && (
          // The control assistive tech meets; the card around it is a bigger target for the mouse.
          <input
            type="checkbox"
            className="vcheck"
            checked={checked}
            aria-label={`Select ${title}`}
            onClick={(ev) => {
              ev.stopPropagation()
              onPick(e.index, ev.shiftKey)
            }}
            onChange={() => {}}
          />
        )}
      </div>
      <div className="vtitle">{title}</div>
      <div className="vmeta truncate">
        {e.unavailable ? 'Unavailable (private or deleted)' : metaLine(e, uploader)}
        {status && <span className="vstatus"> · {status}</span>}
      </div>
      {playlist && (
        <Button
          size="sm"
          style={{marginTop: 6}}
          onClick={(ev) => {
            ev.stopPropagation()
            onOpen?.(e.url)
          }}>
          Open
        </Button>
      )}
    </div>
  )
})

const byIndex = (e: PlaylistEntry) => e.index

/**
 * The first entries of a long list, and an element to put after them that brings the next lot
 * when it scrolls near. A new list (fewer entries than before, or another first entry) starts
 * again; one that grows keeps what's shown.
 */
export function useSteps<T>(entries: T[], key: (t: T) => string | number): {shown: T[]; sentinel: ReactNode} {
  const [limit, setLimit] = useState(STEP)
  // By key, not object: a job's object is replaced on every progress tick.
  const first = entries.length > 0 ? key(entries[0]) : undefined
  const [was, setWas] = useState({first, n: entries.length})
  if (first !== was.first || entries.length < was.n) {
    setWas({first, n: entries.length})
    setLimit(STEP)
  } else if (entries.length !== was.n) {
    setWas({first: was.first, n: entries.length})
  }
  const shown = entries.length > limit ? entries.slice(0, limit) : entries
  const more = useRef<HTMLDivElement>(null)
  const hasMore = shown.length < entries.length
  useEffect(() => {
    const el = more.current
    if (!el || !hasMore) return
    const io = new IntersectionObserver((seen) => seen.some((s) => s.isIntersecting) && setLimit((l) => l + STEP), {
      root: scroller(el),
      rootMargin: '1200px 0px',
    })
    io.observe(el)
    return () => io.disconnect()
  }, [hasMore, limit])
  return {shown, sentinel: hasMore ? <div ref={more} className="vgrid-more" aria-hidden="true" /> : null}
}

/**
 * Videos as a grid of thumbnails, the way YouTube shows a channel, each with a checkbox. Shift+click
 * ticks or clears everything since the last one clicked. Entries are keyed by their index.
 */
export const VideoGrid = memo(function VideoGrid({
  entries,
  selected,
  setSelected,
  onOpen,
  onOpenVideo,
  tall,
  uploader,
  status,
  reveal,
}: {
  entries: PlaylistEntry[]
  selected: Set<number>
  setSelected: Dispatch<SetStateAction<Set<number>>>
  /** Open a playlist listed among the videos (a channel's Playlists tab). */
  onOpen?: (url: string) => void
  /** Double-clicking a video opens it on its own (search results: to choose its options). */
  onOpenVideo?: (url: string) => void
  /** Portrait thumbnails, for Shorts. */
  tall?: boolean
  /** Show each video's uploader (a playlist mixes channels; a channel page doesn't need it). */
  uploader?: boolean
  /** A word about each video, such as "Downloaded", or ''. */
  status?: (e: PlaylistEntry) => string
  /** Fade each batch of new entries in from the top, rather than showing them at once. */
  reveal?: boolean
}) {
  const anchor = useRef<number | null>(null)
  // Each entry's place in the batch it came in, set when it first shows, so a card already on
  // screen keeps its number while "Show more" adds the next batch from 0.
  const order = useRef(new Map<number, number>())
  if (reveal) {
    if (entries.length < order.current.size) order.current = new Map() // another list
    let n = 0
    for (const e of entries) if (!order.current.has(e.index)) order.current.set(e.index, n++)
  }
  const list = useRef(entries)
  list.current = entries

  const {shown, sentinel} = useSteps(entries, byIndex)

  const onPick = useCallback(
    (index: number, range: boolean) => {
      // Read now: the updater may run later, after the anchor has moved on.
      const from = range && anchor.current !== null ? list.current.findIndex((e) => e.index === anchor.current) : -1
      anchor.current = index
      setSelected((prev) => {
        const next = new Set(prev)
        const on = !prev.has(index)
        const to = list.current.findIndex((e) => e.index === index)
        if (from >= 0 && to >= 0) {
          for (const e of list.current.slice(Math.min(from, to), Math.max(from, to) + 1)) {
            if (!selectable(e)) continue
            if (on) next.add(e.index)
            else next.delete(e.index)
          }
        } else if (on) {
          next.add(index)
        } else {
          next.delete(index)
        }
        return next
      })
    },
    [setSelected],
  )

  let lastGroup = ''
  return (
    <div className={`vgrid${tall ? ' tall' : ''}${selected.size > 0 ? ' selecting' : ''}`} role="group" aria-label="Videos">
      {shown.map((e) => {
        const header = e.group && e.group !== lastGroup ? (
          <div key={`g-${e.index}`} className="vgroup">
            {e.group}
          </div>
        ) : null
        lastGroup = e.group
        const card = (
          <Card
            key={e.index}
            entry={e}
            checked={selected.has(e.index)}
            status={status?.(e) ?? ''}
            uploader={!!uploader}
            onPick={onPick}
            onOpen={onOpen}
            onOpenVideo={onOpenVideo}
            reveal={reveal ? (order.current.get(e.index) ?? -1) : -1}
          />
        )
        return header ? [header, card] : card
      })}
      {sentinel}
    </div>
  )
})
