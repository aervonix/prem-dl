import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {SetStateAction} from 'react'
import {api, errMessage} from '../api'
import {ErrorPanel} from '../components/ErrorPanel'
import {VideoGrid, selectable, useJobStatus} from '../components/VideoGrid'
import {Button, Card, Empty, Spinner} from '../components/ui'
import {plural} from '../format'
import {useApp} from '../store'
import type {AppError, NewJob, PlaylistEntry, SearchFilter} from '../types'

const PAGE = 24 // results asked for at a time

const ANY: SearchFilter = {type: '', uploaded: '', length: ''}

function looksLikeUrl(s: string): boolean {
  return /^https?:\/\/\S+$/i.test(s.trim())
}

/**
 * YouTube search: results as the same grid of thumbnails as a channel, ticked to download with
 * the default options, or opened on the Download page to choose. Ticks are kept by video ID
 * across searches, so videos from several searches can be downloaded together.
 */
export function SearchPage() {
  const {state, toast, go, openLinks} = useApp()
  const [text, setText] = useState('')
  const [filter, setFilter] = useState<SearchFilter>(ANY)
  // The search the results are for.
  const [shown, setShown] = useState<{query: string; filter: SearchFilter} | null>(null)
  const [entries, setEntries] = useState<PlaylistEntry[]>([])
  const [loading, setLoading] = useState<'new' | 'more' | null>(null)
  const [error, setError] = useState<AppError | null>(null)
  const [ended, setEnded] = useState(false)
  const [picked, setPicked] = useState<Map<string, PlaylistEntry>>(new Map())
  const [adding, setAdding] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const request = useRef(0)
  const fetched = useRef(0) // results read so far, duplicates included; where "Show more" goes on
  const status = useJobStatus()

  useEffect(() => {
    if (state.page === 'search') inputRef.current?.focus()
  }, [state.page])

  const run = useCallback(async (query: string, f: SearchFilter, first: number) => {
    const id = ++request.current
    setLoading(first === 1 ? 'new' : 'more')
    setError(null)
    if (first === 1) {
      setShown({query, filter: f})
      setEntries([])
      setEnded(false)
      fetched.current = 0
    }
    try {
      const res = await api.search(query, f, first, PAGE)
      if (request.current !== id || res.cancelled) return
      if (res.error) return setError(res.error)
      const got = res.result?.playlist?.entries ?? []
      fetched.current = first - 1 + got.length
      setEnded(got.length < PAGE)
      // Pages can overlap when YouTube reshuffles between requests.
      setEntries((prev) => {
        const seen = new Set(prev.map((e) => e.id))
        return [...prev, ...got.filter((e) => !seen.has(e.id))]
      })
    } catch (e) {
      if (request.current === id) setError({...searchFailed, message: errMessage(e)})
    } finally {
      if (request.current === id) setLoading(null)
    }
  }, [])

  const submit = () => {
    const q = text.trim()
    if (!q) return
    // A link pasted here belongs on the Download page.
    if (looksLikeUrl(q)) return openLinks([q])
    void run(q, filter, 1)
  }
  const changeFilter = (patch: Partial<SearchFilter>) => {
    const next = {...filter, ...patch}
    setFilter(next)
    if (shown) void run(shown.query, next, 1)
  }

  // The grid works in indices; the ticks themselves are kept by video ID.
  const selected = useMemo(() => new Set(entries.filter((e) => picked.has(e.id)).map((e) => e.index)), [entries, picked])
  const setSelected = useCallback(
    (action: SetStateAction<Set<number>>) => {
      setPicked((prev) => {
        const before = new Set(entries.filter((e) => prev.has(e.id)).map((e) => e.index))
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
  const tickable = useMemo(() => entries.filter(selectable), [entries])
  const pickedHere = tickable.filter((e) => picked.has(e.id)).length

  const download = async () => {
    const options = state.settings?.defaults
    if (picked.size === 0 || !options) return
    const items: NewJob[] = [...picked.values()].map((e) => ({
      url: e.url,
      title: e.title,
      uploader: e.uploader,
      thumbnail: e.thumbnail,
      duration: e.duration,
      playlistTitle: '',
      playlistIndex: 0,
      playlistCount: 0,
    }))
    setAdding(true)
    try {
      const res = await api.enqueue(items, options)
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
  const chooseOptions = () => {
    openLinks([...picked.values()].map((e) => e.url))
    setPicked(new Map())
  }

  return (
    <div className="content">
      <div className="page-head">
        <h1>Search</h1>
      </div>

      <Card>
        <form
          className="urlbox"
          role="search"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}>
          <input
            ref={inputRef}
            type="search"
            className="search-input"
            value={text}
            placeholder="Search YouTube"
            aria-label="Search YouTube"
            maxLength={200}
            spellCheck={false}
            onChange={(e) => setText(e.target.value)}
          />
          <Button type="submit" variant="primary" icon="search" loading={loading === 'new'} disabled={!text.trim()} style={{height: 'auto'}}>
            Search
          </Button>
        </form>
        <div className="search-filters">
          <select aria-label="Type of result" value={filter.type} onChange={(e) => changeFilter({type: e.target.value as SearchFilter['type']})}>
            <option value="">All results</option>
            <option value="video">Videos</option>
            <option value="channel">Channels</option>
            <option value="playlist">Playlists</option>
          </select>
          <select aria-label="Upload date" value={filter.uploaded} onChange={(e) => changeFilter({uploaded: e.target.value as SearchFilter['uploaded']})}>
            <option value="">Any time</option>
            <option value="hour">Last hour</option>
            <option value="today">Today</option>
            <option value="week">This week</option>
            <option value="month">This month</option>
            <option value="year">This year</option>
          </select>
          <select aria-label="Length" value={filter.length} onChange={(e) => changeFilter({length: e.target.value as SearchFilter['length']})}>
            <option value="">Any length</option>
            <option value="short">Under 4 minutes</option>
            <option value="medium">4–20 minutes</option>
            <option value="long">Over 20 minutes</option>
          </select>
          {(filter.type || filter.uploaded || filter.length) && (
            <button type="button" className="link" onClick={() => changeFilter(ANY)}>
              Clear filters
            </button>
          )}
        </div>
      </Card>

      {shown && entries.length > 0 && (
        <div className="pl-tools">
          <Button size="sm" disabled={tickable.length === 0} onClick={() => setSelected(new Set(tickable.map((e) => e.index)))}>
            Select all
          </Button>
          <Button size="sm" disabled={pickedHere === 0} onClick={() => setSelected(new Set())}>
            None
          </Button>
          <span className="muted" style={{fontSize: 12.5}}>
            Click videos to tick them; channels and playlists open on the Download page
          </span>
        </div>
      )}

      {error && entries.length === 0 ? (
        <ErrorPanel error={error} onRetry={shown ? () => void run(shown.query, shown.filter, 1) : undefined} />
      ) : loading === 'new' ? (
        <div className="row" style={{padding: '28px 4px', color: 'var(--muted)'}}>
          <Spinner /> <span>Searching…</span>
        </div>
      ) : !shown ? (
        <Empty title="Search YouTube">Find videos, channels and playlists, then tick the ones to download. Pasting a link here opens it on the Download page.</Empty>
      ) : entries.length === 0 ? (
        <Empty title={`No results for “${shown.query}”`}>{filter.type || filter.uploaded || filter.length ? 'Try fewer filters.' : 'Try other words.'}</Empty>
      ) : (
        <VideoGrid entries={entries} selected={selected} setSelected={setSelected} uploader status={status} onOpen={(url) => openLinks([url])} />
      )}

      {shown && entries.length > 0 && (
        <div className="row" style={{justifyContent: 'center', marginTop: 16}}>
          {loading === 'more' ? (
            <>
              <Spinner /> <span className="muted">Reading more…</span>
            </>
          ) : error ? (
            <span className="muted">
              {error.title}.{' '}
              <button type="button" className="link" onClick={() => void run(shown.query, shown.filter, fetched.current + 1)}>
                Try again
              </button>
            </span>
          ) : ended ? (
            <span className="muted">That's all YouTube found.</span>
          ) : (
            <Button onClick={() => void run(shown.query, shown.filter, fetched.current + 1)}>Show more</Button>
          )}
        </div>
      )}

      {picked.size > 0 && (
        <div className="actionbar">
          <div className="info truncate">
            {plural(picked.size, 'video')} ticked{picked.size > pickedHere ? ` (${picked.size - pickedHere} from other searches)` : ''}. Downloads with your default options.
          </div>
          <Button variant="ghost" onClick={() => setPicked(new Map())}>
            Clear
          </Button>
          <Button icon="sliders" onClick={chooseOptions} title="Open on the Download page to choose quality and other options">
            Choose options…
          </Button>
          <Button variant="primary" size="lg" icon="download" loading={adding} onClick={() => void download()}>
            Download {picked.size}
          </Button>
        </div>
      )}
    </div>
  )
}

const searchFailed: AppError = {
  code: 'search_failed',
  title: 'Search didn’t work',
  message: '',
  hint: 'Check your connection and try again.',
  action: '',
  detail: '',
  retryable: true,
}
