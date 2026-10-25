import {useCallback, useEffect, useRef, useState} from 'react'
import {api, errMessage} from '../api'
import {plural} from '../format'
import {useApp, useJobs} from '../store'
import type {Job} from '../types'
import {playClick} from '../sound'
import {Thumb} from './Thumb'
import {Button, IconButton} from './ui'

interface Notice {
  id: number
  jobs: Job[]
}

const FINISHED = ['completed', 'failed']
// Finishes this close together become one card and one click (a playlist finishing in a burst).
const GATHER_MS = 1200
const MAX_CARDS = 3

/**
 * A card in the corner, and a click, when downloads finish. Only finishes seen while the app runs
 * count: jobs that were already done when it started stay quiet, and so do cancelled ones.
 */
export function FinishNotices() {
  const {state} = useApp()
  const jobs = useJobs()
  const sound = state.settings?.notifySound ?? false
  const popup = state.settings?.notifyPopup ?? false
  const [notices, setNotices] = useState<Notice[]>([])
  const seen = useRef<Map<string, string> | null>(null)
  const pending = useRef<Job[]>([])
  const timer = useRef(0)
  const nextId = useRef(0)
  const prefs = useRef({sound, popup})
  prefs.current = {sound, popup}

  useEffect(() => {
    if (!state.ready) return
    const prev = seen.current
    seen.current = new Map(jobs.map((j) => [j.id, j.status]))
    if (!prev) return // first look: everything here was already known
    const fresh = jobs.filter((j) => FINISHED.includes(j.status) && !FINISHED.includes(prev.get(j.id) ?? ''))
    if (fresh.length === 0) return
    pending.current.push(...fresh)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      const batch = pending.current
      pending.current = []
      if (prefs.current.sound) playClick()
      if (prefs.current.popup) setNotices((n) => [...n, {id: ++nextId.current, jobs: batch}].slice(-MAX_CARDS))
    }, GATHER_MS)
  }, [jobs, state.ready])

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const dismiss = useCallback((id: number) => setNotices((n) => n.filter((x) => x.id !== id)), [])
  return (
    <>
      {notices.map((n) => (
        <NoticeCard key={n.id} notice={n} dismiss={dismiss} />
      ))}
    </>
  )
}

function NoticeCard({notice, dismiss}: {notice: Notice; dismiss: (id: number) => void}) {
  const {go, toast} = useApp()
  const done = useCallback(() => dismiss(notice.id), [dismiss, notice.id])
  // Counts down only while the window has focus and the pointer isn't on the card, so a download
  // that finishes while you're in another app is still there when you come back.
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(() => document.hasFocus())
  useEffect(() => {
    const on = () => setFocused(true)
    const off = () => setFocused(false)
    window.addEventListener('focus', on)
    window.addEventListener('blur', off)
    return () => {
      window.removeEventListener('focus', on)
      window.removeEventListener('blur', off)
    }
  }, [])
  useEffect(() => {
    if (hovered || !focused) return
    const t = window.setTimeout(done, 8000)
    return () => window.clearTimeout(t)
  }, [hovered, focused, done])

  const act = (fn: () => Promise<unknown>, close = true) => {
    fn().catch((e) => toast('error', errMessage(e)))
    if (close) done()
  }

  const failed = notice.jobs.filter((j) => j.status === 'failed')
  const single = notice.jobs.length === 1 ? notice.jobs[0] : null
  const ok = failed.length === 0

  let body
  if (single) {
    body = (
      <>
        <Thumb src={single.thumbnail} className="nthumb thumb" />
        <div className="ntext">
          <div className="nhead">{ok ? 'Download finished' : 'Download failed'}</div>
          <div className="ntitle" title={single.title}>
            {single.title}
          </div>
          {!ok && single.error && <div className="nsub">{single.error.title}</div>}
          <div className="nactions">
            {ok && single.outputFile ? (
              <>
                <Button size="sm" variant="primary" icon="play" onClick={() => act(() => api.openFile(single.id))}>
                  Open
                </Button>
                <Button size="sm" icon="folder" onClick={() => act(() => api.revealFile(single.id))}>
                  Show in folder
                </Button>
              </>
            ) : !ok ? (
              <Button size="sm" variant="primary" icon="retry" onClick={() => act(() => api.retryJob(single.id))}>
                Retry
              </Button>
            ) : null}
            <Button size="sm" variant="ghost" onClick={() => (go('queue'), done())}>
              View queue
            </Button>
          </div>
        </div>
      </>
    )
  } else {
    const completed = notice.jobs.length - failed.length
    body = (
      <div className="ntext">
        <div className="nhead">{ok ? 'Downloads finished' : 'Downloads finished with problems'}</div>
        <div className="ntitle">
          {completed > 0 && `${plural(completed, 'download')} done`}
          {completed > 0 && failed.length > 0 && ', '}
          {failed.length > 0 && `${failed.length} failed`}
        </div>
        <div className="nactions">
          <Button size="sm" variant="primary" onClick={() => (go('queue'), done())}>
            View queue
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div
      className={`notice${ok ? '' : ' bad'}`}
      role="status"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocus={() => setHovered(true)}
      onBlur={() => setHovered(false)}>
      {body}
      <IconButton icon="x" label="Dismiss" onClick={done} />
    </div>
  )
}
