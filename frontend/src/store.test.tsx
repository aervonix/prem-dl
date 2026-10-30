// @vitest-environment jsdom
import {act} from 'react'
import {createRoot} from 'react-dom/client'
import type {Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {AppProvider, initial, jobsReducer, reducer, useApp, useJobs} from './store'
import type {Action, State} from './store'
import type {AppState, AppUpdate, Job, Settings, ToolStatus, UpdateResult} from './types'

// The backend: every call is a mock, and `emit` plays a Go event to whatever subscribed with `on`.
const handlers = vi.hoisted(() => new Map<string, (data: unknown) => void>())
vi.mock('./api', async (importActual) => {
  const actual = await importActual<typeof import('./api')>()
  return {
    errMessage: actual.errMessage,
    on: vi.fn((event: string, fn: (data: unknown) => void) => {
      handlers.set(event, fn)
      return () => handlers.delete(event)
    }),
    api: {
      getState: vi.fn(),
      copyText: vi.fn(),
      repairTools: vi.fn(),
      installAppUpdate: vi.fn(),
      checkAppUpdate: vi.fn(),
      setWindowFocused: vi.fn(),
    },
  }
})
const {api} = await import('./api')
const mocked = vi.mocked(api)
const emit = (event: string, data: unknown) => act(() => handlers.get(event)?.(data))

const job = (id: string, seq: number, extra: Partial<Job> = {}) => ({id, seq, title: id, status: 'queued', ...extra}) as Job
const settings = (extra: Partial<Settings> = {}) => ({theme: 'dark', ...extra}) as Settings
const tools = {checking: false, ytdlp: {path: 'yt-dlp.exe', source: 'app', version: '2026.10.01'}} as ToolStatus
const appState = (extra: Partial<AppState> = {}): AppState =>
  ({version: '0.1.2', dataDir: 'C:\\data', settings: settings(), jobs: [], feeds: [], tools, openLink: '', error: '', ...extra}) as AppState
const update = (extra: Partial<AppUpdate> = {}) => ({current: '0.1.2', latest: '0.1.3', available: true, canInstall: true, error: '', ...extra}) as AppUpdate
const result = (extra: Partial<UpdateResult> = {}): UpdateResult => ({ok: true, previous: '', version: '', path: '', message: 'Updated', error: null, ...extra})

const run = (actions: Action[], from: State = initial) => actions.reduce(reducer, from)

describe('reducer', () => {
  it('fills the state on init', () => {
    const s = run([{type: 'init', state: appState({appUpdate: update()})}])
    expect(s.ready).toBe(true)
    expect(s.version).toBe('0.1.2')
    expect(s.settings?.theme).toBe('dark')
    expect(s.tools).toBe(tools)
    expect(s.appUpdate?.latest).toBe('0.1.3')
    expect(s.page).toBe('download')
    expect(s.incoming).toBeNull()
  })

  it('keeps an app update that arrived before init', () => {
    const early = update({latest: '0.2.0'})
    const s = run([{type: 'appUpdate', info: early}, {type: 'init', state: appState({appUpdate: null})}])
    expect(s.appUpdate).toBe(early)
  })

  it('puts a start-up link on the Download page', () => {
    const s = run([{type: 'page', page: 'settings'}, {type: 'init', state: appState({openLink: 'https://youtu.be/x'})}])
    expect(s.page).toBe('download')
    expect(s.incoming).toEqual({links: ['https://youtu.be/x'], n: 1})
  })

  it('marks a failed load as ready with the error', () => {
    const s = run([{type: 'initFailed', error: 'boom'}])
    expect(s).toMatchObject({ready: true, loadError: 'boom'})
  })

  it('leaves job events to the jobs reducer', () => {
    expect(reducer(initial, {type: 'jobs', jobs: [job('a', 1)]})).toBe(initial)
    expect(reducer(initial, {type: 'jobsRemoved', ids: ['a']})).toBe(initial)
  })

  it('counts every incoming batch so repeats still fire', () => {
    const s = run([
      {type: 'page', page: 'queue'},
      {type: 'incoming', links: ['a']},
      {type: 'incoming', links: ['a']},
    ])
    expect(s.page).toBe('download')
    expect(s.incoming).toEqual({links: ['a'], n: 2})
  })

  it('counts section requests and keeps the last one on plain page changes', () => {
    const s = run([
      {type: 'page', page: 'settings', section: 'cookies'},
      {type: 'page', page: 'settings', section: 'cookies'},
    ])
    expect(s.section).toEqual({id: 'cookies', n: 2})
    const later = reducer(s, {type: 'page', page: 'queue'})
    expect(later.page).toBe('queue')
    expect(later.section).toBe(s.section)
  })

  it('opens Settings → Cookies for a dropped cookies.txt', () => {
    const s = run([
      {type: 'page', page: 'settings', section: 'network'},
      {type: 'droppedCookies', text: '# Netscape HTTP Cookie File'},
    ])
    expect(s.page).toBe('settings')
    expect(s.section).toEqual({id: 'cookies', n: 2})
    expect(s.droppedCookies).toEqual({text: '# Netscape HTTP Cookie File', n: 1})
  })

  it('keeps the last four toasts and removes one by id', () => {
    const toasts = [1, 2, 3, 4, 5].map((id): Action => ({type: 'toast', toast: {id, kind: 'info', text: `t${id}`}}))
    const s = run(toasts)
    expect(s.toasts.map((t) => t.id)).toEqual([2, 3, 4, 5])
    expect(reducer(s, {type: 'toastDone', id: 3}).toasts.map((t) => t.id)).toEqual([2, 4, 5])
  })

  it('remembers dismissed banners', () => {
    const s = run([{type: 'dismiss', key: 'a'}, {type: 'dismiss', key: 'b'}])
    expect(s.dismissed).toEqual({a: true, b: true})
  })

})

describe('jobsReducer', () => {
  it('replaces the jobs on init', () => {
    const s = jobsReducer({old: job('old', 0)}, {type: 'init', state: appState({jobs: [job('a', 1), job('b', 2)]})})
    expect(Object.keys(s)).toEqual(['a', 'b'])
  })

  it('adds and updates jobs without touching the input', () => {
    const before = {a: job('a', 1)}
    const s = jobsReducer(before, {type: 'jobs', jobs: [job('a', 1, {percent: 50}), job('b', 2)]})
    expect(s.a.percent).toBe(50)
    expect(s.b).toBeDefined()
    expect(before.a.percent).toBeUndefined()
  })

  it('removes jobs by id, ignoring unknown ones', () => {
    const s = jobsReducer({a: job('a', 1), b: job('b', 2)}, {type: 'jobsRemoved', ids: ['a', 'zzz']})
    expect(Object.keys(s)).toEqual(['b'])
  })

  it('returns the same object for other actions so React skips the render', () => {
    const before = {a: job('a', 1)}
    expect(jobsReducer(before, {type: 'dismiss', key: 'x'})).toBe(before)
  })
})

describe('AppProvider', () => {
  let root: Root
  let host: HTMLDivElement
  let ctx: ReturnType<typeof useApp>
  let jobs: Job[]

  function Probe() {
    ctx = useApp()
    jobs = useJobs()
    return null
  }

  beforeEach(async () => {
    ;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
    handlers.clear()
    vi.clearAllMocks()
    mocked.getState.mockResolvedValue(appState())
    mocked.setWindowFocused.mockResolvedValue()
    host = document.createElement('div')
    root = createRoot(host)
    await act(async () =>
      root.render(
        <AppProvider>
          <Probe />
        </AppProvider>,
      ),
    )
  })

  afterEach(() => {
    act(() => root.unmount())
    delete document.documentElement.dataset.theme
  })

  it('loads the state and follows the theme setting', async () => {
    expect(ctx.state.ready).toBe(true)
    expect(document.documentElement.dataset.theme).toBe('dark')
    await emit('settings:changed', settings({theme: 'light'}))
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('shows a failed load', async () => {
    act(() => root.unmount())
    mocked.getState.mockRejectedValue(new Error('no backend'))
    root = createRoot(host)
    await act(async () =>
      root.render(
        <AppProvider>
          <Probe />
        </AppProvider>,
      ),
    )
    expect(ctx.state.loadError).toBe('no backend')
  })

  it('lists jobs in queue order as events arrive', async () => {
    await emit('jobs:update', [job('b', 2), job('a', 1)])
    expect(jobs.map((j) => j.id)).toEqual(['a', 'b'])
    await emit('jobs:update', [job('c', 0)])
    expect(jobs.map((j) => j.id)).toEqual(['c', 'a', 'b'])
    await emit('jobs:remove', ['a'])
    expect(jobs.map((j) => j.id)).toEqual(['c', 'b'])
  })

  it('tells the backend when the window gains or loses the focus', () => {
    expect(mocked.setWindowFocused).toHaveBeenCalledTimes(1) // how it started
    act(() => void window.dispatchEvent(new Event('blur')))
    expect(mocked.setWindowFocused).toHaveBeenLastCalledWith(false)
    act(() => void window.dispatchEvent(new Event('focus')))
    expect(mocked.setWindowFocused).toHaveBeenLastCalledWith(true)
  })

  it('only navigates to pages it knows', async () => {
    await emit('app:navigate', 'queue')
    expect(ctx.state.page).toBe('queue')
    await emit('app:navigate', 'nowhere')
    expect(ctx.state.page).toBe('queue')
  })

  it('toasts found videos with a way to the queue', async () => {
    await emit('feeds:found', {count: 3, title: 'Some Channel'})
    const t = ctx.state.toasts.at(-1)!
    expect(t.text).toBe('3 new videos from Some Channel added to the queue')
    act(() => t.action!.run())
    expect(ctx.state.page).toBe('queue')
  })

  it('keeps action identities across state changes', async () => {
    const {go, toast, attempt, repairTools} = ctx
    await emit('settings:changed', settings({theme: 'light'}))
    await act(async () => ctx.toast('info', 'hi'))
    expect(ctx.go).toBe(go)
    expect(ctx.toast).toBe(toast)
    expect(ctx.attempt).toBe(attempt)
    expect(ctx.repairTools).toBe(repairTools)
  })

  it('attempt returns the value, or toasts the error and returns undefined', async () => {
    let ok: number | undefined
    await act(async () => {
      ok = await ctx.attempt(Promise.resolve(7))
    })
    expect(ok).toBe(7)
    let bad: unknown = 'unset'
    await act(async () => {
      bad = await ctx.attempt(Promise.reject('Go said no'))
    })
    expect(bad).toBeUndefined()
    expect(ctx.state.toasts.at(-1)).toMatchObject({kind: 'error', text: 'Go said no'})
  })

  it('copy reports success and failure', async () => {
    mocked.copyText.mockResolvedValueOnce(undefined as never)
    await act(() => ctx.copy('x', 'Copied'))
    expect(ctx.state.toasts.at(-1)).toMatchObject({kind: 'success', text: 'Copied'})
    mocked.copyText.mockRejectedValueOnce(new Error('busy'))
    await act(() => ctx.copy('x', 'Copied'))
    expect(ctx.state.toasts.at(-1)).toMatchObject({kind: 'error', text: "Couldn't copy to the clipboard: busy"})
  })

  describe('repairTools', () => {
    it('marks the repair while it runs, and ignores a second click', async () => {
      let finish!: (r: UpdateResult) => void
      mocked.repairTools.mockReturnValueOnce(new Promise((r) => (finish = r)))
      let p!: Promise<void>
      act(() => {
        p = ctx.repairTools()
      })
      expect(ctx.state.repairing).toBe(true)
      await act(() => ctx.repairTools())
      expect(mocked.repairTools).toHaveBeenCalledTimes(1)
      await act(async () => {
        finish(result({message: 'Ready'}))
        await p
      })
      expect(ctx.state.repairing).toBe(false)
      expect(ctx.state.toasts.at(-1)).toMatchObject({kind: 'success', text: 'Ready'})
    })

    it('toasts a failure', async () => {
      mocked.repairTools.mockResolvedValueOnce(result({ok: false, error: {title: 'No', message: 'locked', hint: 'Retry.'} as UpdateResult['error']}))
      await act(() => ctx.repairTools())
      expect(ctx.state.repairing).toBe(false)
      expect(ctx.state.toasts.at(-1)).toMatchObject({kind: 'error', text: 'No: locked Retry.'})
    })
  })

  describe('installAppUpdate', () => {
    it('keeps the bar and marks the restart once the installer runs', async () => {
      mocked.installAppUpdate.mockResolvedValueOnce(result())
      await act(() => ctx.installAppUpdate())
      expect(ctx.state.appRestarting).toBe(true)
      expect(ctx.state.appUpdateProgress).not.toBeNull()
      await emit('app:update-progress', {done: 9, total: 10})
      expect(ctx.state.appUpdateProgress).toEqual({done: 9, total: 10})
    })

    it('clears the bar and says why when it fails', async () => {
      mocked.installAppUpdate.mockResolvedValueOnce(
        result({ok: false, error: {code: 'x', title: 'Download failed', message: 'Timed out.', hint: 'Try again later.'} as UpdateResult['error']}),
      )
      await act(() => ctx.installAppUpdate())
      expect(ctx.state.appRestarting).toBe(false)
      expect(ctx.state.appUpdateProgress).toBeNull()
      expect(ctx.state.toasts.at(-1)).toMatchObject({kind: 'error', text: 'Download failed: Timed out. Try again later.'})
      await emit('app:update-progress', {done: 1, total: 10})
      expect(ctx.state.appUpdateProgress).toBeNull()
    })
  })

  describe('checkAppUpdate', () => {
    it('stores a successful check', async () => {
      mocked.checkAppUpdate.mockResolvedValueOnce(update())
      await act(() => ctx.checkAppUpdate())
      expect(ctx.state.appUpdate?.latest).toBe('0.1.3')
    })

    it('returns a failed check without storing it', async () => {
      mocked.checkAppUpdate.mockResolvedValueOnce(update({error: 'GitHub said 404'}))
      let res: AppUpdate | undefined
      await act(async () => {
        res = await ctx.checkAppUpdate()
      })
      expect(res?.error).toBe('GitHub said 404')
      expect(ctx.state.appUpdate).toBeNull()
    })
  })
})
