import {memo, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {api, errMessage} from '../api'
import {ErrorPanel} from '../components/ErrorPanel'
import {editable} from '../components/DropLinks'
import {useSteps} from '../components/VideoGrid'
import {RetryDialog} from '../components/RetryDialog'
import {Thumb} from '../components/Thumb'
import {Button, Empty, IconButton, Modal, Progress, Segmented, useConfirm} from '../components/ui'
import {basename, bytes, eta, goTime, hoursOpen, plural, speed} from '../format'
import {Icon} from '../icons'
import {useApp, useJobs} from '../store'
import type {Job, JobStatus} from '../types'

type Filter = 'all' | 'active' | 'waiting' | 'done' | 'failed'

const ACTIVE: JobStatus[] = ['starting', 'downloading', 'processing']

function matches(j: Job, f: Filter): boolean {
  switch (f) {
    case 'all':
      return true
    case 'active':
      return ACTIVE.includes(j.status)
    case 'waiting':
      return j.status === 'queued' || j.status === 'paused'
    case 'done':
      return j.status === 'completed'
    case 'failed':
      return j.status === 'failed' || j.status === 'cancelled'
  }
}

/** When an automatic retry is due, or null. Go sends its zero time (year 1) when there is none. */
function retryDue(j: Job): number | null {
  const t = Date.parse(j.retryAt)
  return j.status === 'queued' && t > Date.UTC(2000, 0) ? t : null
}

/** "1:45" (or "77:19:39") until the given time, ticking every second. */
function Countdown({at}: {at: number}) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(t)
  }, [])
  const s = Math.max(0, Math.ceil((at - now) / 1000))
  const pad = (n: number) => String(n).padStart(2, '0')
  if (s >= 3600) return <>{`${Math.floor(s / 3600)}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`}</>
  return <>{s >= 60 ? `${Math.floor(s / 60)}:${pad(s % 60)}` : `${s}s`}</>
}

/** While it waits for a scheduled stream to start: when it looks again, or null. */
function waitingUntil(j: Job): number | null {
  return j.status === 'starting' ? goTime(j.waitUntil) : null
}

function statusText(j: Job): string {
  switch (j.status) {
    case 'queued':
      return retryDue(j) ? 'Will retry automatically' : 'Waiting in queue'
    case 'starting':
      return waitingUntil(j) ? 'Waiting for the stream to start' : 'Starting…'
    case 'downloading':
    case 'processing':
      return j.stage
    case 'paused':
      return j.stage === 'Interrupted' ? 'Paused (app was closed)' : 'Paused'
    case 'completed':
      return j.note || 'Done'
    case 'failed':
      return 'Failed'
    case 'cancelled':
      return 'Cancelled'
  }
}

function progressState(j: Job): 'busy' | 'done' | 'paused' | 'failed' | 'active' {
  if (j.status === 'completed') return 'done'
  if (j.status === 'failed') return 'failed'
  if (j.status === 'paused' || j.status === 'queued' || j.status === 'cancelled') return 'paused'
  if (j.status === 'processing' || j.status === 'starting') return 'busy'
  return 'active'
}

const JobRow = memo(function JobRow({
  job,
  onLog,
  onEdit,
  notify,
  selected,
  onSelect,
}: {
  job: Job
  onLog: (j: Job) => void
  onEdit: (j: Job) => void
  notify: (msg: string) => void
  selected: boolean
  /** Tick or untick it; range ticks everything since the last one clicked (Shift+click). */
  onSelect: (id: string, range: boolean) => void
}) {
  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
    } catch (e) {
      notify(e instanceof Error ? e.message : String(e))
    }
  }
  const s = job.status
  const live = ACTIVE.includes(s)
  const percent = s === 'completed' ? 100 : s === 'starting' ? -1 : job.percent
  const due = retryDue(job)
  const waiting = waitingUntil(job)
  const sub = [job.uploader, job.playlistTitle ? `${job.playlistTitle} · ${job.playlistIndex}/${job.playlistCount}` : ''].filter(Boolean).join(' · ')

  return (
    <div className={`job${selected ? ' on' : ''}`}>
      <input
        type="checkbox"
        className="jsel"
        checked={selected}
        aria-label={`Select ${job.title}`}
        onClick={(e) => onSelect(job.id, e.shiftKey)}
        onChange={() => {}}
      />
      <Thumb src={job.thumbnail} seconds={job.duration} className="jthumb thumb" />
      <div style={{minWidth: 0}}>
        <div className="jtitle truncate" title={job.title}>
          {job.title}
        </div>
        <div className="jsub truncate">
          {sub && <>{sub} · </>}
          <span style={{color: s === 'failed' ? 'var(--danger)' : s === 'completed' ? 'var(--ok)' : undefined}}>{statusText(job)}</span>
          {s === 'completed' && job.outputFile && (
            <span className="muted" title={job.outputFile}>
              {' · '}
              {basename(job.outputFile)}
            </span>
          )}
        </div>
        {s !== 'failed' && s !== 'cancelled' && !waiting && <Progress percent={percent} state={progressState(job)} label={`${job.title}: ${job.stage}`} />}
        {waiting && (
          <div className="jretry">
            <span>
              <Icon name="clock" size={13} /> It downloads once the stream starts. Next look in <Countdown at={waiting} />. Keep Prem-dl running; it can wait in the tray.
            </span>
          </div>
        )}
        {due && (
          <div className="jretry">
            <span>
              {job.error?.title ?? 'It failed'}. Automatic retry {job.autoRetries} in <Countdown at={due} />.
            </span>
            <Button size="sm" variant="ghost" icon="retry" onClick={() => void run(() => api.retryJob(job.id))}>
              Retry now
            </Button>
          </div>
        )}
        {(live || s === 'paused') && !waiting && (
          <div className="jstats">
            <span>
              {[job.percent >= 0 ? `${job.percent.toFixed(1)}%` : '', job.total > 0 ? `${bytes(job.downloaded)} of ${bytes(job.total)}` : '']
                .filter(Boolean)
                .join(' · ')}
            </span>
            {s === 'downloading' && (
              <span>
                {[speed(job.speed), eta(job.eta)].filter(Boolean).join(' · ')}
              </span>
            )}
          </div>
        )}
        {s === 'completed' && job.total > 0 && (
          <div className="jstats">
            <span>{bytes(job.total)}</span>
          </div>
        )}
      </div>
      <div className="jactions">
        {(live || s === 'queued') && <IconButton icon="pause" label="Pause" onClick={() => void run(() => api.pauseJob(job.id))} />}
        {(s === 'queued' || s === 'paused') && <IconButton icon="toTop" label="Move to top" onClick={() => void run(() => api.moveJobToTop(job.id))} />}
        {s === 'paused' && <IconButton icon="play" label="Resume" onClick={() => void run(() => api.resumeJob(job.id))} />}
        {(s === 'failed' || s === 'cancelled') && <IconButton icon="retry" label="Retry" onClick={() => void run(() => api.retryJob(job.id))} />}
        {(s === 'failed' || s === 'cancelled') && <IconButton icon="sliders" label="Change options and retry" onClick={() => onEdit(job)} />}
        {s === 'completed' && job.outputFile && (
          <>
            <IconButton icon="file" label="Open file" onClick={() => void run(() => api.openFile(job.id))} />
            <IconButton icon="folder" label="Show in folder" onClick={() => void run(() => api.revealFile(job.id))} />
          </>
        )}
        <IconButton icon="terminal" label="Show log" onClick={() => onLog(job)} />
        {/* A paused job offers Remove; Cancel next to it did the same thing by another name. */}
        {(live || s === 'queued') && <IconButton icon="x" label="Cancel" danger onClick={() => void run(() => api.cancelJob(job.id))} />}
        {!live && s !== 'queued' && <IconButton icon="trash" label="Remove from list" danger onClick={() => void run(() => api.removeJob(job.id))} />}
      </div>
      {s === 'failed' && job.error && (
        <div className="jerr">
          <ErrorPanel
            error={job.error}
            onRetry={() => void run(() => api.retryJob(job.id))}
            onWait={() => void run(() => api.retryJobWith(job.id, {...job.options, waitForVideo: true}))}
            onEdit={() => onEdit(job)}
            onDetails={() => onLog(job)}
            compact
            live={false}
          />
        </div>
      )}
    </div>
  )
})

function LogModal({job, onClose}: {job: Job; onClose: () => void}) {
  const [lines, setLines] = useState<string[]>([])
  const box = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const {copy} = useApp()

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const l = await api.jobLog(job.id)
        if (alive) setLines(l ?? [])
      } catch {
        /* the job may have been removed */
      }
    }
    void load()
    const t = window.setInterval(load, 1000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [job.id])

  useEffect(() => {
    const el = box.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [lines])

  return (
    <Modal
      title={`Log: ${job.title}`}
      onClose={onClose}
      footer={
        <>
          <Button icon="copy" onClick={() => void copy(`${job.url}\n\n${lines.join('\n')}`, 'Log copied')}>
            Copy log
          </Button>
          <Button variant="primary" onClick={onClose}>
            Close
          </Button>
        </>
      }>
      <div className="hint" style={{marginBottom: 8, wordBreak: 'break-all'}}>
        {job.url}
      </div>
      <div
        ref={box}
        className="logbox"
        onScroll={(e) => {
          const el = e.currentTarget
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}>
        {lines.length === 0 ? (
          <span className="muted">Nothing logged yet.</span>
        ) : (
          lines.map((l, i) => (
            <div key={i} className={l.startsWith('ERROR') ? 'err' : l.startsWith('WARNING') ? 'warn' : undefined}>
              {l}
            </div>
          ))
        )}
      </div>
    </Modal>
  )
}

// The page unmounts while hidden (it would otherwise re-render on every progress tick), so the
// chosen filter is kept here for the next visit.
let lastFilter: Filter = 'all'
let lastSearch = ''

const jobId = (j: Job) => j.id

export function QueuePage() {
  const {state, go, toast, copy, attempt} = useApp()
  const jobs = useJobs()
  const [filter, setFilterState] = useState<Filter>(lastFilter)
  const setFilter = useCallback((f: Filter) => {
    lastFilter = f
    setFilterState(f)
  }, [])
  const [search, setSearchState] = useState(lastSearch)
  const setSearch = (q: string) => {
    lastSearch = q
    setSearchState(q)
  }
  const [logJob, setLogJob] = useState<Job | null>(null)
  const [editJob, setEditJob] = useState<Job | null>(null)

  const counts = useMemo(() => {
    // failed counts failed and cancelled (the Failed tab); failedOnly is what "Retry failed" retries.
    const c = {all: jobs.length, active: 0, waiting: 0, done: 0, failed: 0, failedOnly: 0}
    for (const j of jobs) {
      if (ACTIVE.includes(j.status)) c.active++
      else if (j.status === 'queued' || j.status === 'paused') c.waiting++
      else if (j.status === 'completed') c.done++
      else c.failed++
      if (j.status === 'failed') c.failedOnly++
    }
    return c
  }, [jobs])

  // Failed downloads can't be retried once cleared: that takes a second click.
  const [clearArmed, confirmClear] = useConfirm()

  const totalSpeed = useMemo(() => jobs.reduce((n, j) => (j.status === 'downloading' ? n + j.speed : n), 0), [jobs])
  const shown = useMemo(() => {
    const q = search.trim().toLowerCase()
    return jobs.filter((j) => matches(j, filter) && (!q || [j.title, j.uploader, j.url, j.playlistTitle].some((t) => t.toLowerCase().includes(q))))
  }, [jobs, filter, search])
  const retryFailed = async () => {
    try {
      const n = await api.retryFailed()
      if (n < counts.failedOnly) toast('info', `Retried ${n} of ${counts.failedOnly}; the others are already queued again.`)
    } catch (e) {
      toast('error', errMessage(e))
    }
  }
  const notify = useCallback((m: string) => toast('error', m), [toast])
  const canPause = counts.active > 0 || jobs.some((j) => j.status === 'queued')
  const canResume = jobs.some((j) => j.status === 'paused')

  // Keep the log's job fresh (title/URL can fill in after the plan arrives).
  const liveLogJob = logJob ? jobs.find((j) => j.id === logJob.id) ?? logJob : null

  // Outside the download hours queued downloads wait; checked again every half minute.
  const [minute, setMinute] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setMinute(Date.now()), 30_000)
    return () => window.clearInterval(t)
  }, [])
  const hours = useMemo(() => hoursOpen(state.settings?.activeHours, new Date(minute)), [state.settings?.activeHours, minute])

  // ---- selection: tick rows (Shift+click for a run), then act on them all ----
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const anchor = useRef<string | null>(null)
  const shownRef = useRef(shown)
  shownRef.current = shown
  const onSelect = useCallback((id: string, range: boolean) => {
    const list = shownRef.current
    const from = range && anchor.current ? list.findIndex((j) => j.id === anchor.current) : -1
    anchor.current = id
    setPicked((prev) => {
      const next = new Set(prev)
      const on = !prev.has(id)
      const to = list.findIndex((j) => j.id === id)
      const ids = from >= 0 && to >= 0 ? list.slice(Math.min(from, to), Math.max(from, to) + 1).map((j) => j.id) : [id]
      for (const x of ids) {
        if (on) next.add(x)
        else next.delete(x)
      }
      return next
    })
  }, [])
  // Ticks on downloads that left the list (cleared, or out of the filter) are dropped.
  const sel = useMemo(() => shown.filter((j) => picked.has(j.id)), [shown, picked])
  const [removeArmed, confirmRemove] = useConfirm()
  const each = async (fn: (id: string) => Promise<unknown>, which: Job[]) => {
    let failed = 0
    for (const j of which) await fn(j.id).catch(() => failed++)
    if (failed) toast('error', `${plural(failed, 'download')} couldn't be changed`)
  }
  const selPausable = sel.filter((j) => ACTIVE.includes(j.status) || j.status === 'queued')
  const selResumable = sel.filter((j) => j.status === 'paused')
  const selRetryable = sel.filter((j) => j.status === 'failed' || j.status === 'cancelled')
  const selWaiting = sel.filter((j) => j.status === 'queued' || j.status === 'paused')
  const removeSelected = () =>
    // Only finished downloads leave without a second click: the rest would be lost.
    confirmRemove(() => {
      void each(api.removeJob, sel)
      setPicked(new Set())
    }, sel.some((j) => j.status !== 'completed'))

  // Ctrl+F finds; Ctrl+A ticks what's shown, Delete removes the ticked, Esc unticks.
  const searchRef = useRef<HTMLInputElement>(null)
  const keys = useRef<(e: KeyboardEvent) => void>(() => {})
  keys.current = (e) => {
    if (document.querySelector('[aria-modal]')) return
    const ctrl = e.ctrlKey || e.metaKey
    if (ctrl && e.key.toLowerCase() === 'f') {
      e.preventDefault()
      searchRef.current?.focus()
      searchRef.current?.select()
      return
    }
    if (editable(e.target)) return
    if (ctrl && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      setPicked(new Set(shownRef.current.map((j) => j.id)))
    } else if (e.key === 'Escape' && sel.length > 0) {
      setPicked(new Set())
    } else if (e.key === 'Delete' && sel.length > 0) {
      e.preventDefault()
      removeSelected()
    }
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => keys.current(e)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // A full queue (500) is made a few rows at a time as it scrolls into view.
  const rows = useSteps(shown, jobId)

  return (
    <div className="content" style={{maxWidth: 980}}>
      <div className="page-head">
        <h1>Queue</h1>
      </div>

      {jobs.length === 0 ? (
        <Empty title="Nothing in the queue">
          Downloads you add show up here with live progress.
          <div style={{marginTop: 14}}>
            <Button variant="primary" icon="download" onClick={() => go('download')}>
              Add a download
            </Button>
          </div>
        </Empty>
      ) : (
        <>
          <div className="q-toolbar">
            <Segmented
              label="Filter"
              value={filter}
              onChange={setFilter}
              options={[
                {value: 'all', label: `All ${counts.all}`},
                {value: 'active', label: `Active ${counts.active}`},
                {value: 'waiting', label: `Waiting ${counts.waiting}`},
                {value: 'done', label: `Done ${counts.done}`},
                {value: 'failed', label: `Failed ${counts.failed}`},
              ]}
            />
            <div className="summary">{totalSpeed > 0 ? `${speed(totalSpeed)} total` : counts.active > 0 ? 'Working…' : ''}</div>
            <div className="q-actions">
              <Button size="sm" icon="pause" disabled={!canPause} onClick={() => void attempt(api.pauseAll())}>
                Pause all
              </Button>
              <Button size="sm" icon="play" disabled={!canResume} onClick={() => void attempt(api.resumeAll())}>
                Resume all
              </Button>
              <Button size="sm" icon="trash" disabled={counts.done === 0} title="Remove completed downloads from the list. Files on disk are kept." onClick={() => void attempt(api.clearFinished(true))}>
                Clear completed
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={counts.done === 0 && counts.failed === 0}
                title="Also remove failed and cancelled downloads, which can then no longer be retried. Files on disk are kept."
                onClick={() => confirmClear(() => void attempt(api.clearFinished(false)), counts.failed > 0)}>
                {clearArmed ? `Click again to remove ${counts.failed} failed too` : 'Clear all finished'}
              </Button>
            </div>
          </div>
          <div className="q-toolbar">
            <input
              ref={searchRef}
              type="search"
              className="q-search"
              placeholder="Search titles, channels and links…"
              aria-label="Search downloads"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <div className="summary" />
            {counts.failedOnly > 0 && (
              <Button size="sm" icon="retry" onClick={() => void retryFailed()}>
                Retry {counts.failedOnly} failed
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              icon="copy"
              disabled={shown.length === 0}
              title="Copy the links of the downloads shown, one per line, to paste into the Download page later."
              onClick={() => void copy(shown.map((j) => j.url).join('\n'), `Copied ${plural(shown.length, 'link')}`)}>
              Copy links
            </Button>
          </div>

          {hours && !hours.open && counts.active === 0 && jobs.some((j) => j.status === 'queued') && (
            <div className="q-selbar" role="status">
              <Icon name="clock" size={16} />
              <span className="summary">Downloads start at {hours.opens}, the download hours set in Settings → Network.</span>
              <div style={{flex: 1}} />
              <Button size="sm" variant="primary" icon="play" onClick={() => void attempt(api.startNow())}>
                Start now
              </Button>
            </div>
          )}

          {sel.length > 0 && (
            <div className="q-selbar" role="region" aria-label="Selected downloads">
              <span className="summary">{plural(sel.length, 'download')} selected</span>
              <Button size="sm" icon="pause" disabled={selPausable.length === 0} onClick={() => void each(api.pauseJob, selPausable)}>
                Pause
              </Button>
              <Button size="sm" icon="play" disabled={selResumable.length === 0} onClick={() => void each(api.resumeJob, selResumable)}>
                Resume
              </Button>
              <Button size="sm" icon="retry" disabled={selRetryable.length === 0} onClick={() => void each(api.retryJob, selRetryable)}>
                Retry
              </Button>
              <Button size="sm" icon="toTop" disabled={selWaiting.length === 0} onClick={() => void each(api.moveJobToTop, [...selWaiting].reverse())}>
                Move to top
              </Button>
              <Button size="sm" variant="ghost" className="danger" icon="trash" onClick={removeSelected}>
                {removeArmed ? 'Click again to remove' : 'Remove'}
              </Button>
              <div style={{flex: 1}} />
              <Button size="sm" variant="ghost" onClick={() => setPicked(new Set())}>
                Clear selection
              </Button>
            </div>
          )}

          {shown.length === 0 ? (
            <div className="empty">{search.trim() ? `Nothing matches “${search.trim()}”.` : `Nothing here. ${plural(counts.all, 'download')} in other tabs.`}</div>
          ) : (
            <div className={picked.size > 0 ? 'jobs selecting' : 'jobs'}>
              {rows.shown.map((j) => (
                <JobRow key={j.id} job={j} onLog={setLogJob} onEdit={setEditJob} notify={notify} selected={picked.has(j.id)} onSelect={onSelect} />
              ))}
              {rows.sentinel}
            </div>
          )}
        </>
      )}

      {liveLogJob && <LogModal job={liveLogJob} onClose={() => setLogJob(null)} />}
      {editJob && <RetryDialog job={editJob} onClose={() => setEditJob(null)} />}
    </div>
  )
}
