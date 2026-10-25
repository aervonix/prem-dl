import {memo, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {api} from '../api'
import {ErrorPanel} from '../components/ErrorPanel'
import {Thumb} from '../components/Thumb'
import {Button, Empty, IconButton, Modal, Progress, Segmented} from '../components/ui'
import {basename, bytes, eta, plural, speed} from '../format'
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

function statusText(j: Job): string {
  switch (j.status) {
    case 'queued':
      return 'Waiting in queue'
    case 'starting':
      return 'Starting…'
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

const JobRow = memo(function JobRow({job, onLog, notify}: {job: Job; onLog: (j: Job) => void; notify: (msg: string) => void}) {
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
  const sub = [job.uploader, job.playlistTitle ? `${job.playlistTitle} · ${job.playlistIndex}/${job.playlistCount}` : ''].filter(Boolean).join(' · ')

  return (
    <div className="job">
      <Thumb src={job.thumbnail} seconds={job.duration} className="jthumb thumb" />
      <div style={{minWidth: 0}}>
        <div className="jtitle truncate" title={job.title}>
          {job.title}
        </div>
        <div className="jsub truncate">
          {sub && <>{sub} · </>}
          <span style={{color: s === 'failed' ? 'var(--danger)' : s === 'completed' ? 'var(--ok)' : undefined}}>{statusText(job)}</span>
          {s === 'completed' && job.outputFile && <span className="muted"> · {basename(job.outputFile)}</span>}
        </div>
        {s !== 'failed' && s !== 'cancelled' && <Progress percent={percent} state={progressState(job)} />}
        {(live || s === 'paused') && (
          <div className="jstats">
            <span>
              {job.percent >= 0 ? `${job.percent.toFixed(1)}%` : '—'}
              {job.total > 0 && ` · ${bytes(job.downloaded)} of ${bytes(job.total)}`}
            </span>
            {s === 'downloading' && (
              <span>
                {speed(job.speed)} · {eta(job.eta)}
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
        {s === 'paused' && <IconButton icon="play" label="Resume" onClick={() => void run(() => api.resumeJob(job.id))} />}
        {(s === 'failed' || s === 'cancelled') && <IconButton icon="retry" label="Retry" onClick={() => void run(() => api.retryJob(job.id))} />}
        {s === 'completed' && job.outputFile && (
          <>
            <IconButton icon="play" label="Open file" onClick={() => void run(() => api.openFile(job.id))} />
            <IconButton icon="folder" label="Show in folder" onClick={() => void run(() => api.revealFile(job.id))} />
          </>
        )}
        <IconButton icon="terminal" label="Show log" onClick={() => onLog(job)} />
        {(live || s === 'queued' || s === 'paused') && <IconButton icon="x" label="Cancel" danger onClick={() => void run(() => api.cancelJob(job.id))} />}
        {!live && s !== 'queued' && <IconButton icon="trash" label="Remove from list" danger onClick={() => void run(() => api.removeJob(job.id))} />}
      </div>
      {s === 'failed' && job.error && (
        <div className="jerr">
          <ErrorPanel error={job.error} onRetry={() => void run(() => api.retryJob(job.id))} onDetails={() => onLog(job)} compact />
        </div>
      )}
    </div>
  )
})

function LogModal({job, onClose}: {job: Job; onClose: () => void}) {
  const [lines, setLines] = useState<string[]>([])
  const box = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const {toast} = useApp()

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

  const copy = async () => {
    await api.copyText(`${job.url}\n\n${lines.join('\n')}`)
    toast('success', 'Log copied')
  }

  return (
    <Modal
      title={`Log — ${job.title}`}
      onClose={onClose}
      footer={
        <>
          <Button icon="copy" onClick={() => void copy()}>
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

export function QueuePage() {
  const {go, toast} = useApp()
  const jobs = useJobs()
  const [filter, setFilter] = useState<Filter>('all')
  const [logJob, setLogJob] = useState<Job | null>(null)

  const counts = useMemo(() => {
    const c = {all: jobs.length, active: 0, waiting: 0, done: 0, failed: 0}
    for (const j of jobs) {
      if (ACTIVE.includes(j.status)) c.active++
      else if (j.status === 'queued' || j.status === 'paused') c.waiting++
      else if (j.status === 'completed') c.done++
      else c.failed++
    }
    return c
  }, [jobs])

  const totalSpeed = useMemo(() => jobs.reduce((n, j) => (j.status === 'downloading' ? n + j.speed : n), 0), [jobs])
  const shown = useMemo(() => jobs.filter((j) => matches(j, filter)), [jobs, filter])
  const notify = useCallback((m: string) => toast('error', m), [toast])
  const canPause = counts.active > 0 || jobs.some((j) => j.status === 'queued')
  const canResume = jobs.some((j) => j.status === 'paused')

  // Keep the log's job fresh (title/URL can fill in after the plan arrives).
  const liveLogJob = logJob ? jobs.find((j) => j.id === logJob.id) ?? logJob : null

  return (
    <div className="content" style={{maxWidth: 980}}>
      <div className="page-head">
        <h1>Queue</h1>
      </div>

      {jobs.length === 0 ? (
        <Empty icon="list" title="Nothing in the queue">
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
            <Button size="sm" icon="pause" disabled={!canPause} onClick={() => void api.pauseAll()}>
              Pause all
            </Button>
            <Button size="sm" icon="play" disabled={!canResume} onClick={() => void api.resumeAll()}>
              Resume all
            </Button>
            <Button size="sm" icon="trash" disabled={counts.done === 0 && counts.failed === 0} onClick={() => void api.clearFinished(false)}>
              Clear finished
            </Button>
          </div>

          {shown.length === 0 ? (
            <div className="empty">Nothing here — {plural(counts.all, 'download')} in other tabs.</div>
          ) : (
            shown.map((j) => <JobRow key={j.id} job={j} onLog={setLogJob} notify={notify} />)
          )}
        </>
      )}

      {liveLogJob && <LogModal job={liveLogJob} onClose={() => setLogJob(null)} />}
    </div>
  )
}
