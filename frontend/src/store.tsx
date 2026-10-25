import {createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef} from 'react'
import type {ReactNode} from 'react'
import {api, errMessage, on} from './api'
import type {AppState, Job, Settings, ToolStatus, UpdateInfo, UpdateResult} from './types'

export type Page = 'download' | 'queue' | 'settings'
export type ToastKind = 'info' | 'success' | 'error'

export interface Toast {
  id: number
  kind: ToastKind
  text: string
  action?: {label: string; run: () => void}
}

interface State {
  ready: boolean
  loadError: string
  version: string
  dataDir: string
  settings: Settings | null
  tools: ToolStatus
  toolProgress: {done: number; total: number} | null
  /** Set while ffmpeg is being installed. */
  ffmpegProgress: {done: number; total: number} | null
  updateAvailable: UpdateInfo | null
  autoUpdated: UpdateResult | null
  dismissed: Record<string, boolean>
  toasts: Toast[]
  page: Page
  /** Settings section to scroll to; `n` changes on every request so repeats still fire. */
  section: {id: string; n: number} | null
  incomingUrl: {url: string; n: number} | null
}

const emptyTools: ToolStatus = {
  checking: true,
  ytdlp: null,
  ytdlpError: '',
  ffmpeg: null,
  jsRuntimes: [],
  managedPath: '',
  ffmpegManaged: false,
  canInstallFfmpeg: false,
}

const initial: State = {
  ready: false,
  loadError: '',
  version: '',
  dataDir: '',
  settings: null,
  tools: emptyTools,
  toolProgress: null,
  ffmpegProgress: null,
  updateAvailable: null,
  autoUpdated: null,
  dismissed: {},
  toasts: [],
  page: 'download',
  section: null,
  incomingUrl: null,
}

type Action =
  | {type: 'init'; state: AppState}
  | {type: 'initFailed'; error: string}
  | {type: 'jobs'; jobs: Job[]}
  | {type: 'jobsRemoved'; ids: string[]}
  | {type: 'tools'; tools: ToolStatus}
  | {type: 'toolProgress'; p: State['toolProgress']}
  | {type: 'ffmpegProgress'; p: State['ffmpegProgress']}
  | {type: 'settings'; settings: Settings}
  | {type: 'updateAvailable'; info: UpdateInfo | null}
  | {type: 'autoUpdated'; res: UpdateResult | null}
  | {type: 'dismiss'; key: string}
  | {type: 'toast'; toast: Toast}
  | {type: 'toastDone'; id: number}
  | {type: 'page'; page: Page; section?: string}
  | {type: 'incomingUrl'; url: string}

// Jobs live in their own reducer and context: they change several times a second while
// downloading, and only the queue, sidebar and banners need them.
function jobsReducer(s: Record<string, Job>, a: Action): Record<string, Job> {
  switch (a.type) {
    case 'init': {
      const jobs: Record<string, Job> = {}
      for (const j of a.state.jobs) jobs[j.id] = j
      return jobs
    }
    case 'jobs': {
      const jobs = {...s}
      for (const j of a.jobs) jobs[j.id] = j
      return jobs
    }
    case 'jobsRemoved': {
      const jobs = {...s}
      for (const id of a.ids) delete jobs[id]
      return jobs
    }
    default:
      return s // same object: React skips the update
  }
}

function reducer(s: State, a: Action): State {
  switch (a.type) {
    case 'init':
      return {
        ...s,
        ready: true,
        version: a.state.version,
        dataDir: a.state.dataDir,
        settings: a.state.settings,
        tools: a.state.tools,
      }
    case 'initFailed':
      return {...s, ready: true, loadError: a.error}
    case 'jobs':
    case 'jobsRemoved':
      return s
    case 'tools':
      return {...s, tools: a.tools}
    case 'toolProgress':
      return {...s, toolProgress: a.p}
    case 'ffmpegProgress':
      return {...s, ffmpegProgress: a.p}
    case 'settings':
      return {...s, settings: a.settings}
    case 'updateAvailable':
      return {...s, updateAvailable: a.info}
    case 'autoUpdated':
      // The background update sent progress events too; nothing else clears them.
      return {...s, autoUpdated: a.res, toolProgress: null, updateAvailable: a.res?.ok ? null : s.updateAvailable}
    case 'dismiss':
      return {...s, dismissed: {...s.dismissed, [a.key]: true}}
    case 'toast':
      return {...s, toasts: [...s.toasts.slice(-3), a.toast]}
    case 'toastDone':
      return {...s, toasts: s.toasts.filter((t) => t.id !== a.id)}
    case 'page':
      return {
        ...s,
        page: a.page,
        section: a.section ? {id: a.section, n: (s.section?.n ?? 0) + 1} : s.section,
      }
    case 'incomingUrl':
      return {...s, page: 'download', incomingUrl: {url: a.url, n: (s.incomingUrl?.n ?? 0) + 1}}
  }
}

interface Ctx {
  state: State
  go: (p: Page, section?: string) => void
  toast: (kind: ToastKind, text: string, action?: Toast['action']) => void
  dismissToast: (id: number) => void
  dismiss: (key: string) => void
  setSettings: (s: Settings) => void
  setTools: (t: ToolStatus) => void
  /** Run a backend call that may reject; shows an error toast and returns undefined on failure. */
  attempt: <T>(p: Promise<T>) => Promise<T | undefined>
  /** Copy text to the clipboard; the toast says whether it worked. */
  copy: (text: string, done: string) => Promise<void>
  /** Install or update yt-dlp, reporting the outcome with a toast. */
  updateYtdlp: () => Promise<UpdateResult>
  /** Install (or update) the app's own ffmpeg, reporting the outcome with a toast. */
  installFfmpeg: () => Promise<void>
  setUpdateAvailable: (i: UpdateInfo | null) => void
}

const AppCtx = createContext<Ctx | null>(null)
const JobsCtx = createContext<Job[] | null>(null)

export function useApp(): Ctx {
  const c = useContext(AppCtx)
  if (!c) throw new Error('useApp outside provider')
  return c
}

/** Every job in queue order. Components using this re-render on each progress update. */
export function useJobs(): Job[] {
  const c = useContext(JobsCtx)
  if (!c) throw new Error('useJobs outside provider')
  return c
}

export function AppProvider({children}: {children: ReactNode}) {
  const [state, dispatchState] = useReducer(reducer, initial)
  const [jobsById, dispatchJobs] = useReducer(jobsReducer, {})
  const dispatch = useCallback((a: Action) => {
    dispatchState(a)
    dispatchJobs(a)
  }, [])
  const toastId = useRef(0)

  const toast = useCallback((kind: ToastKind, text: string, action?: Toast['action']) => {
    const id = ++toastId.current
    dispatch({type: 'toast', toast: {id, kind, text, action}})
    // Toasts dismiss themselves (see Toasts.tsx), pausing while hovered or focused.
  }, [dispatch])

  // Initial load + backend events.
  useEffect(() => {
    let cancelled = false
    api
      .getState()
      .then((st) => !cancelled && dispatch({type: 'init', state: st}))
      .catch((e) => !cancelled && dispatch({type: 'initFailed', error: errMessage(e)}))

    const offs = [
      on('jobs:update', (jobs) => dispatch({type: 'jobs', jobs})),
      on('jobs:remove', (ids) => dispatch({type: 'jobsRemoved', ids})),
      on('tools:status', (tools) => dispatch({type: 'tools', tools})),
      on('tools:progress', (p) => dispatch({type: 'toolProgress', p})),
      // Progress only while an install is running: a late event must not bring the bar back.
      on('ffmpeg:progress', (p) => installing.current && dispatch({type: 'ffmpegProgress', p})),
      on('tools:update-available', (info) => dispatch({type: 'updateAvailable', info})),
      on('tools:auto-updated', (res) => dispatch({type: 'autoUpdated', res})),
      on('settings:changed', (settings) => dispatch({type: 'settings', settings})),
      on('app:open-url', (url) => dispatch({type: 'incomingUrl', url})),
    ]
    return () => {
      cancelled = true
      offs.forEach((off) => off())
    }
  }, [dispatch])

  // Keep the theme attribute in sync with the setting.
  const theme = state.settings?.theme ?? 'system'
  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])

  const jobs = useMemo(() => Object.values(jobsById).sort((a, b) => a.seq - b.seq), [jobsById])

  const attempt = useCallback(
    async <T,>(p: Promise<T>): Promise<T | undefined> => {
      try {
        return await p
      } catch (e) {
        toast('error', errMessage(e))
        return undefined
      }
    },
    [toast],
  )

  const copy = useCallback(
    async (text: string, done: string) => {
      try {
        await api.copyText(text)
        toast('success', done)
      } catch (e) {
        toast('error', `Couldn't copy to the clipboard: ${errMessage(e)}`)
      }
    },
    [toast],
  )

  const updateYtdlp = useCallback(async (): Promise<UpdateResult> => {
    dispatch({type: 'toolProgress', p: {done: 0, total: 0}})
    try {
      const res = await api.updateYtdlp()
      if (res.ok) {
        toast('success', res.message)
        dispatch({type: 'updateAvailable', info: null})
      } else if (res.error) {
        toast('error', `${res.error.title}: ${res.error.message}`)
      }
      return res
    } catch (e) {
      const msg = errMessage(e)
      toast('error', msg)
      return {ok: false, previous: '', version: '', path: '', message: '', error: null}
    } finally {
      dispatch({type: 'toolProgress', p: null})
    }
  }, [dispatch, toast])

  const installing = useRef(false)
  const installFfmpeg = useCallback(async () => {
    if (installing.current) return
    installing.current = true
    dispatch({type: 'ffmpegProgress', p: {done: 0, total: 0}})
    try {
      const res = await api.installFfmpeg()
      if (res.ok) toast('success', res.message)
      else if (res.error) toast('error', `${res.error.title}: ${res.error.message}`)
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      installing.current = false
      dispatch({type: 'ffmpegProgress', p: null})
    }
  }, [dispatch, toast])

  // Actions never change identity, so effects and memoised children that take them don't rerun.
  const actions = useMemo<Omit<Ctx, 'state'>>(
    () => ({
      go: (page, section) => dispatch({type: 'page', page, section}),
      toast,
      dismissToast: (id) => dispatch({type: 'toastDone', id}),
      dismiss: (key) => dispatch({type: 'dismiss', key}),
      setSettings: (settings) => dispatch({type: 'settings', settings}),
      setTools: (tools) => dispatch({type: 'tools', tools}),
      attempt,
      copy,
      updateYtdlp,
      installFfmpeg,
      setUpdateAvailable: (info) => dispatch({type: 'updateAvailable', info}),
    }),
    [dispatch, toast, attempt, copy, updateYtdlp, installFfmpeg],
  )
  const ctx = useMemo<Ctx>(() => ({state, ...actions}), [state, actions])

  return (
    <AppCtx.Provider value={ctx}>
      <JobsCtx.Provider value={jobs}>{children}</JobsCtx.Provider>
    </AppCtx.Provider>
  )
}
