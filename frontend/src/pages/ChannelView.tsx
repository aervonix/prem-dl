import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {SetStateAction} from 'react'
import {api, errMessage} from '../api'
import {ErrorPanel} from '../components/ErrorPanel'
import {FeedDialog, TAB_LABELS} from '../components/FeedDialog'
import {VideoGrid, selectable, useJobStatus} from '../components/VideoGrid'
import {Button, Empty, IconButton, Spinner} from '../components/ui'
import {hostOf, plural} from '../format'
import {useApp} from '../store'
import type {AppError, ChannelTab, Feed, NewJob, PlaylistEntry} from '../types'

const FIRST = 60 // videos listed when a tab opens
const MORE = 120 // and added by "Show more"
const TABS: ChannelTab[] = ['videos', 'shorts', 'streams']

interface Listing {
  entries: PlaylistEntry[]
  /** How many were asked for; 0 = all. Fewer came back when the list ended. */
  limit: number
  loading: boolean
  error: AppError | null
  /**
   * Read page by page (without yt-dlp): the backend's listing, which reads on as far as `limit`
   * when asked, so "Show more" carries on rather than reading the tab again from the top.
   * 0 when the whole answer came at once.
   */
  listing: number
  /** The listing has reached the end of the tab. */
  done: boolean
}

/**
 * A followed channel or playlist, laid out like YouTube: its videos as a grid, a tab each for a
 * channel's Videos, Shorts and Live, and a checkbox on every video to download several at once.
 * Ticks are kept by video ID, across tabs and reloads.
 */
export function ChannelView({feed, onBack}: {feed: Feed; onBack: () => void}) {
  const {toast, go} = useApp()
  const channel = feed.tabs.length > 0
  const [tab, setTab] = useState<ChannelTab | ''>(channel ? feed.tabs[0] : '')
  const [lists, setLists] = useState<Partial<Record<string, Listing>>>({})
  const [picked, setPicked] = useState<Map<string, PlaylistEntry>>(new Map())
  const [filter, setFilter] = useState('')
  const [editing, setEditing] = useState(false)
  const [adding, setAdding] = useState(false)
  const request = useRef(0)
  const latest = useRef<Record<string, number>>({}) // the newest request for each tab

  const load = useCallback(
    async (t: ChannelTab | '', limit: number) => {
      const id = ++request.current
      latest.current[t] = id
      setLists((l) => ({...l, [t]: {entries: l[t]?.entries ?? [], limit, loading: true, error: null, listing: 0, done: false}}))
      let next: Listing | null
      try {
        const res = await api.browseFeed(feed.id, t, limit)
        if (latest.current[t] !== id) return // a newer request for this tab answers instead
        const got = res.result?.playlist?.entries ?? []
        next = res.cancelled
          ? null // another tab started listing: this one lists again when it's opened
          : {
              entries: got,
              limit,
              // A listing read page by page goes on loading (below) until it has `limit`.
              loading: !!res.listing && (limit === 0 || got.length < limit),
              error: res.error ?? (res.result?.kind === 'video' ? notAList : null),
              listing: res.listing,
              done: !res.listing && (limit === 0 || got.length < limit),
            }
      } catch (e) {
        if (latest.current[t] !== id) return
        next = {entries: [], limit, loading: false, error: {...notAList, title: 'Couldn’t list the videos', message: errMessage(e)}, listing: 0, done: true}
      }
      setLists((l) => {
        const cur = l[t]
        if (next) return {...l, [t]: next.error && cur?.entries.length ? {...next, entries: cur.entries} : next}
        if (cur && cur.entries.length > 0) return {...l, [t]: {...cur, loading: false}}
        const rest = {...l}
        delete rest[t]
        return rest
      })
    },
    [feed.id],
  )

  // A listing read page by page is pulled on until it has what was asked for.
  const pulling = useRef(new Set<number>())
  useEffect(() => {
    for (const [t, cur] of Object.entries(lists)) {
      if (!cur?.listing || cur.done || !cur.loading || pulling.current.has(cur.listing)) continue
      const {listing, limit} = cur
      pulling.current.add(listing)
      api
        .moreFeedEntries(listing, limit)
        .then((part) =>
          setLists((l) => {
            const c = l[t]
            if (!c || c.listing !== listing) return l // read again meanwhile
            const entries = part.entries.length ? [...c.entries, ...part.entries] : c.entries
            const done = part.done
            const loading = !done && (c.limit === 0 || entries.length < c.limit)
            const error = done && part.error && entries.length === 0 ? {...notAList, title: 'Couldn’t list the videos', message: part.error} : c.error
            return {...l, [t]: {...c, entries, done, loading, error}}
          }),
        )
        .catch(() => setLists((l) => (l[t]?.listing === listing ? {...l, [t]: {...l[t]!, loading: false, done: true}} : l)))
        .finally(() => pulling.current.delete(listing))
    }
  }, [lists])

  /** "Show more" (n more) or "Show all" (n = 0). */
  const showMore = (n: number) => {
    const cur = lists[tab]
    if (!cur?.listing || cur.done) return void load(tab, n === 0 ? 0 : (cur?.limit ?? FIRST) + n)
    const limit = n === 0 ? 0 : cur.entries.length + n
    setLists((l) => ({...l, [tab]: {...cur, limit, loading: true}}))
  }

  // Each tab is listed the first time it opens; switching back shows what was already read.
  const current = lists[tab]
  useEffect(() => {
    if (!lists[tab]) void load(tab, FIRST)
  }, [tab, lists, load])
  // Leaving the page stops a slow listing.
  // (After a tick: StrictMode unmounts and mounts again at once, which must not stop it.)
  const mounted = useRef(false)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      setTimeout(() => !mounted.current && void api.cancelBrowse(), 0)
    }
  }, [])

  const entries = useMemo(() => {
    const all = current?.entries ?? []
    const q = filter.trim().toLowerCase()
    return q ? all.filter((e) => e.title.toLowerCase().includes(q)) : all
  }, [current, filter])
  const more = !!current && !current.loading && !current.done && current.limit > 0 && current.entries.length >= current.limit

  // The grid works in indices of the current tab; the ticks themselves are kept by video ID.
  const selected = useMemo(() => new Set(entries.filter((e) => picked.has(e.id)).map((e) => e.index)), [entries, picked])
  const setSelected = useCallback(
    (action: SetStateAction<Set<number>>) => {
      setPicked((prev) => {
        const shown = entries.filter((e) => prev.has(e.id))
        const before = new Set(shown.map((e) => e.index))
        const after = typeof action === 'function' ? action(before) : action
        const next = new Map(prev)
        for (const e of entries) {
          if (after.has(e.index)) next.set(e.id, e)
          else next.delete(e.id)
        }
        return next
      })
    },
    [entries],
  )

  const status = useJobStatus()

  const tickable = useMemo(() => entries.filter(selectable), [entries])
  const pickedHere = tickable.filter((e) => picked.has(e.id)).length

  const download = async () => {
    if (picked.size === 0) return
    const items: NewJob[] = [...picked.values()].map((e) => ({
      url: e.url,
      title: e.title,
      uploader: e.uploader || feed.uploader || feed.title,
      thumbnail: e.thumbnail,
      duration: e.duration,
      playlistTitle: feed.title,
      playlistIndex: 0,
      playlistCount: 0,
    }))
    setAdding(true)
    try {
      const res = await api.enqueue(items, feed.options)
      if (res.error) return toast('error', res.error)
      setPicked(new Map())
      if (res.added === 0) return toast('info', 'Already in the queue')
      toast('success', `Added ${plural(res.added, 'download')}${res.skipped ? ` (${res.skipped} already queued)` : ''}`, {label: 'View queue', run: () => go('queue')})
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setAdding(false)
    }
  }

  return (
    <div className="content">
      <div className="page-head">
        <IconButton icon="back" label="Back to Following" onClick={onBack} />
        <div style={{minWidth: 0}}>
          <h1 className="truncate">{feed.title}</h1>
          <div className="muted truncate" style={{fontSize: 12.5}}>
            {channel ? 'YouTube channel' : hostOf(feed.url)} · {feed.paused ? 'following, paused' : 'following'}
          </div>
        </div>
        <div className="spacer" />
        <Button icon="sliders" onClick={() => setEditing(true)}>
          Options
        </Button>
        <IconButton icon="external" label="Open in browser" onClick={() => void api.openExternal(channel && tab ? `${feed.url}/${tab}` : feed.url)} />
      </div>

      {channel && (
        <div className="ch-tabs" role="tablist" aria-label="Channel tabs">
          {TABS.map((t) => (
            <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>
              {TAB_LABELS[t]}
              {!feed.tabs.includes(t) && <span className="muted"> (not followed)</span>}
            </button>
          ))}
        </div>
      )}

      <div className="pl-tools">
        <Button size="sm" disabled={tickable.length === 0} onClick={() => setSelected(new Set(tickable.map((e) => e.index)))}>
          Select all
        </Button>
        <Button size="sm" disabled={pickedHere === 0} onClick={() => setSelected(new Set())}>
          None
        </Button>
        <span className="muted" style={{fontSize: 12.5}}>
          Shift+click ticks a run of videos
        </span>
        <div style={{flex: 1}} />
        <input type="text" placeholder="Filter by title…" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter videos" />
        <IconButton icon="refresh" label="Reload" disabled={current?.loading} onClick={() => void load(tab, current?.limit || FIRST)} />
      </div>

      {current?.error ? (
        <ErrorPanel error={current.error} onRetry={() => void load(tab, current.limit || FIRST)} />
      ) : !current || (current.loading && current.entries.length === 0) ? (
        <div className="row" style={{padding: '28px 4px', color: 'var(--muted)'}}>
          <Spinner /> <span>Reading {channel && tab ? TAB_LABELS[tab] : 'the videos'}…</span>
        </div>
      ) : entries.length === 0 ? (
        <Empty title={filter.trim() ? `No videos match “${filter.trim()}”` : 'Nothing here'}>
          {filter.trim() ? 'Only the videos listed so far are searched; show more to search further.' : 'This tab has no videos.'}
        </Empty>
      ) : (
        <VideoGrid entries={entries} selected={selected} setSelected={setSelected} tall={tab === 'shorts'} uploader={!channel} status={status} />
      )}

      {(more || (current?.loading && current.entries.length > 0)) && (
        <div className="row" style={{justifyContent: 'center', marginTop: 16}}>
          {current?.loading ? (
            <>
              <Spinner /> <span className="muted">Reading more…</span>
            </>
          ) : (
            <>
              <Button onClick={() => showMore(MORE)}>Show more</Button>
              <Button variant="ghost" onClick={() => showMore(0)} title="Large channels can take a minute">
                Show all
              </Button>
            </>
          )}
        </div>
      )}

      {picked.size > 0 && (
        <div className="actionbar">
          <div className="info truncate">
            {plural(picked.size, 'video')} ticked{picked.size > pickedHere ? ` (${picked.size - pickedHere} on other tabs)` : ''}. Downloads with this {channel ? 'channel' : 'playlist'}’s options.
          </div>
          <Button variant="ghost" onClick={() => setPicked(new Map())}>
            Clear
          </Button>
          <Button variant="primary" size="lg" icon="download" loading={adding} onClick={() => void download()}>
            Download {picked.size}
          </Button>
        </div>
      )}

      {editing && <FeedDialog feed={feed} options={feed.options} onClose={() => setEditing(false)} />}
    </div>
  )
}

const notAList: AppError = {
  code: 'not_a_feed',
  title: 'This isn’t a list of videos',
  message: 'The link leads to a single video.',
  hint: '',
  action: '',
  detail: '',
  retryable: false,
}
