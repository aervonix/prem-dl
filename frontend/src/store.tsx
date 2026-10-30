import {createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef} from 'react'
import type {ReactNode} from 'react'
import {api, errMessage, on} from './api'
import {plural} from './format'
import type {AppState, AppUpdate, Feed, Job, Settings, ToolStatus} from './types'

export type Page = 'download' | 'search' | 'queue' | 'following' | 'settings'
const PAGES: Page[] = ['download', 'queue', 'following', 'settings']
export type ToastKind = 'info' | 'success' | 'error'

export interface Toast {
  id: number
  kind: ToastKind
  text: string
  action?: {label: string; run: () => void}
}

export interface State {
  ready: boolean
  loadError: string
  version: string
  dataDir: string
  settings: Settings | null
  /** Followed channels and playlists. */
  feeds: Feed[]
  tools: ToolStatus
  /** Set while the built-in tools are unpacked again (repairTools). */
  repairing: boolean
  /** The last check for a new Prem-dl. */
  appUpdate: AppUpdate | null
  /** Set while the new Prem-dl's installer downloads. */
  appUpdateProgress: {done: number; total: number} | null
  /** The installer is running and the app is about to quit. */
  appRestarting: boolean
  dismissed: Record<string, boolean>
  toasts: Toast[]
  page: Page
  /** Settings section to scroll to; `n` changes on every request so repeats still fire. */
  section: {id: string; n: number} | null
  /** Links for the Download page: from the Chrome helper, a second launch, or dropped on the window. */
  incoming: {links: string[]; n: number} | null
  /** A cookies.txt dropped on the window, for Settings to offer importing. */
  droppedCookies: {text: string; n: number} | null
}

const emptyTools: ToolStatus = {
  checking: true,
  ytdlp: null,
  ytdlpError: '',
  ffmpeg: null,
  jsRuntimes: [],
  builtIn: false,
}

export const initial: State = {
  ready: false,
  loadError: '',
  version: '',
  dataDir: '',
  settings: null,
  feeds: [],
  tools: emptyTools,
  repairing: false,
  appUpdate: null,
  appUpdateProgress: null,
  appRestarting: false,
  dismissed: {},
  toasts: [],
  page: 'download',
  section: null,
  incoming: null,
  droppedCookies: null,
}

export type Action =
  | {type: 'init'; state: AppState}
  | {type: 'initFailed'; error: string}
  | {type: 'jobs'; jobs: Job[]}
  | {type: 'jobsRemoved'; ids: string[]}
  | {type: 'tools'; tools: ToolStatus}
  | {type: 'repairing'; on: boolean}
  | {type: 'settings'; settings: Settings}
  | {type: 'feeds'; feeds: Feed[]}
  | {type: 'appUpdate'; info: AppUpdate}
  | {type: 'appUpdateProgress'; p: State['appUpdateProgress']}
  | {type: 'appRestarting'}
  | {type: 'dismiss'; key: string}
  | {type: 'toast'; toast: Toast}
  | {type: 'toastDone'; id: number}
  | {type: 'page'; page: Page; section?: string}
  | {type: 'incoming'; links: string[]}
  | {type: 'droppedCookies'; text: string}

// Jobs live in their own reducer and context: they change several times a second while
// downloading, and only the queue, sidebar and banners need them.
export function jobsReducer(s: Record<string, Job>, a: Action): Record<string, Job> {
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

export function reducer(s: State, a: Action): State {
  switch (a.type) {
    case 'init':
      return {
        ...s,
        ready: true,
        version: a.state.version,
        dataDir: a.state.dataDir,
        settings: a.state.settings,
        feeds: a.state.feeds,
        tools: a.state.tools,
        appUpdate: a.state.appUpdate ?? s.appUpdate,
        ...(a.state.openLink ? {page: 'download' as const, incoming: {links: [a.state.openLink], n: (s.incoming?.n ?? 0) + 1}} : {}),
      }
    case 'initFailed':
      return {...s, ready: true, loadError: a.error}
    case 'jobs':
    case 'jobsRemoved':
      return s
    case 'tools':
      return {...s, tools: a.tools}
    case 'repairing':
      return {...s, repairing: a.on}
    case 'settings':
      return {...s, settings: a.settings}
    case 'feeds':
      return {...s, feeds: a.feeds}
    case 'appUpdate':
      return {...s, appUpdate: a.info}
    case 'appUpdateProgress':
      return {...s, appUpdateProgress: a.p}
    case 'appRestarting':
      return {...s, appRestarting: true}
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
    case 'incoming':
      return {...s, page: 'download', incoming: {links: a.links, n: (s.incoming?.n ?? 0) + 1}}
    case 'droppedCookies':
      return {...s, page: 'settings', section: {id: 'cookies', n: (s.section?.n ?? 0) + 1}, droppedCookies: {text: a.text, n: (s.droppedCookies?.n ?? 0) + 1}}
  }
}

interface Ctx {
  state: State
  go: (p: Page, section?: string) => void
  /** Put links on the Download page; one is analysed straight away. */
  openLinks: (links: string[]) => void
  /** Show Settings → Cookies, offering to import a dropped cookies.txt. */
  offerCookies: (text: string) => void
  toast: (kind: ToastKind, text: string, action?: Toast['action']) => void
  dismissToast: (id: number) => void
  dismiss: (key: string) => void
  setSettings: (s: Settings) => void
  setTools: (t: ToolStatus) => void
  /** Run a backend call that may reject; shows an error toast and returns undefined on failure. */
  attempt: <T>(p: Promise<T>) => Promise<T | undefined>
  /** Copy text to the clipboard; the toast says whether it worked. */
  copy: (text: string, done: string) => Promise<void>
  /** Unpack the tools built into Prem-dl again, reporting the outcome with a toast. */
  repairTools: () => Promise<void>
  /** Check for a new Prem-dl now; failures come back in the result's error. */
  checkAppUpdate: () => Promise<AppUpdate | undefined>
  /** Download and run the new Prem-dl's installer; the app restarts when it works. */
  installAppUpdate: () => Promise<void>
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
      on('app:update', (info) => dispatch({type: 'appUpdate', info})),
      on('app:update-progress', (p) => updatingApp.current && dispatch({type: 'appUpdateProgress', p})),
      on('settings:changed', (settings) => dispatch({type: 'settings', settings})),
      on('feeds:update', (feeds) => dispatch({type: 'feeds', feeds})),
      on('feeds:found', (f) =>
        toast('info', `${plural(f.count, 'new video')} from ${f.title} added to the queue`, {label: 'View queue', run: () => dispatch({type: 'page', page: 'queue'})}),
      ),
      on('app:open-url', (url) => dispatch({type: 'incoming', links: [url]})),
      on('app:navigate', (page) => {
        if ((PAGES as string[]).includes(page)) dispatch({type: 'page', page: page as Page})
      }),
      on('app:error', (msg) => toast('error', msg)),
    ]
    return () => {
      cancelled = true
      offs.forEach((off) => off())
    }
  }, [dispatch, toast])

  // Tell the backend when the window gains or loses the focus, which decides whether a finished
  // download gets a desktop notification. Linux can't be asked; Windows ignores it.
  useEffect(() => {
    const report = (focused: boolean) => void api.setWindowFocused(focused).catch(() => {})
    const onFocus = () => report(true)
    const onBlur = () => report(false)
    report(document.hasFocus())
    window.addEventListener('focus', onFocus)
    window.addEventListener('blur', onBlur)
    return () => {
      window.removeEventListener('focus', onFocus)
      window.removeEventListener('blur', onBlur)
    }
  }, [])

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

  const repairingRef = useRef(false)
  const repairTools = useCallback(async () => {
    if (repairingRef.current) return
    repairingRef.current = true
    dispatch({type: 'repairing', on: true})
    try {
      const res = await api.repairTools()
      if (res.ok) toast('success', res.message)
      else if (res.error) toast('error', `${res.error.title}: ${res.error.message} ${res.error.hint}`.trim())
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      repairingRef.current = false
      dispatch({type: 'repairing', on: false})
    }
  }, [dispatch, toast])

  const updatingApp = useRef(false)
  const installAppUpdate = useCallback(async () => {
    if (updatingApp.current) return
    updatingApp.current = true
    dispatch({type: 'appUpdateProgress', p: {done: 0, total: 0}})
    let ok = false
    try {
      const res = await api.installAppUpdate()
      ok = res.ok
      if (res.ok) dispatch({type: 'appRestarting'})
      else if (res.error) toast('error', `${res.error.title}: ${res.error.message} ${res.error.hint}`.trim())
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      // Once the installer runs the app is quitting; keep the bar until it's gone.
      if (!ok) {
        updatingApp.current = false
        dispatch({type: 'appUpdateProgress', p: null})
      }
    }
  }, [dispatch, toast])

  const checkAppUpdate = useCallback(async () => {
    try {
      const info = await api.checkAppUpdate()
      if (!info.error) dispatch({type: 'appUpdate', info})
      return info
    } catch (e) {
      toast('error', errMessage(e))
      return undefined
    }
  }, [dispatch, toast])

  // Actions never change identity, so effects and memoised children that take them don't rerun.
  const actions = useMemo<Omit<Ctx, 'state'>>(
    () => ({
      go: (page, section) => dispatch({type: 'page', page, section}),
      openLinks: (links) => dispatch({type: 'incoming', links}),
      offerCookies: (text) => dispatch({type: 'droppedCookies', text}),
      toast,
      dismissToast: (id) => dispatch({type: 'toastDone', id}),
      dismiss: (key) => dispatch({type: 'dismiss', key}),
      setSettings: (settings) => dispatch({type: 'settings', settings}),
      setTools: (tools) => dispatch({type: 'tools', tools}),
      attempt,
      copy,
      repairTools,
      checkAppUpdate,
      installAppUpdate,
    }),
    [dispatch, toast, attempt, copy, repairTools, checkAppUpdate, installAppUpdate],
  )
  const ctx = useMemo<Ctx>(() => ({state, ...actions}), [state, actions])

  return (
    <AppCtx.Provider value={ctx}>
      <JobsCtx.Provider value={jobs}>{children}</JobsCtx.Provider>
    </AppCtx.Provider>
  )
}
