import {useEffect, useMemo, useRef} from 'react'
import {api} from './api'
import {Banners} from './components/Banners'
import {CloseConfirm} from './components/CloseConfirm'
import {DropLinks, editable} from './components/DropLinks'
import {MAX_DROPPED, linksIn} from './drop'
import {Toasts} from './components/Toasts'
import {Spinner} from './components/ui'
import {Icon} from './icons'
import type {IconName} from './icons'
import {DownloadPage} from './pages/DownloadPage'
import {FollowingPage} from './pages/FollowingPage'
import {QueuePage} from './pages/QueuePage'
import {SearchPage} from './pages/SearchPage'
import {SettingsPage} from './pages/SettingsPage'
import {setTypingSound} from './sound'
import {AppProvider, useApp, useJobs} from './store'
import type {Page} from './store'

const NAV: {page: Page; label: string; icon: IconName}[] = [
  {page: 'search', label: 'Search', icon: 'search'},
  {page: 'download', label: 'Download', icon: 'download'},
  {page: 'queue', label: 'Queue', icon: 'list'},
  {page: 'following', label: 'Following', icon: 'bell'},
  {page: 'settings', label: 'Settings', icon: 'settings'},
]

function Sidebar() {
  const {state, go} = useApp()
  const jobs = useJobs()
  const busy = useMemo(() => jobs.filter((j) => ['starting', 'downloading', 'processing', 'queued'].includes(j.status)).length, [jobs])
  const {tools} = state

  return (
    <aside className="sidebar">
      <div className="brand">Prem-dl</div>
      {NAV.map((n, i) => (
        <button
          key={n.page}
          type="button"
          className="nav-item"
          title={`${n.label} (Ctrl+${i + 1})`}
          aria-label={n.label}
          aria-keyshortcuts={`Control+${i + 1}`}
          aria-current={state.page === n.page ? 'page' : undefined}
          onClick={() => go(n.page)}>
          <Icon name={n.icon} />
          <span className="nav-label">{n.label}</span>
          {n.page === 'queue' && busy > 0 && <span className="nav-badge">{busy}</span>}
          {n.page === 'settings' && state.settingsDirty && state.page !== 'settings' && (
            <span className="nav-dot" title="Unsaved changes" aria-label="Unsaved changes" />
          )}
        </button>
      ))}
      <div className="sidebar-foot">
        <span>Prem-dl {state.version}</span>
        {tools.checking ? (
          <span>Checking tools…</span>
        ) : tools.ytdlpError ? (
          <button type="button" className="foot-link bad" onClick={() => go('settings', 'updates')}>
            Needs repair
          </button>
        ) : state.appUpdate?.available ? (
          <button type="button" className="foot-link" onClick={() => go('settings', 'updates')}>
            Update available
          </button>
        ) : null}
      </div>
    </aside>
  )
}

function Shell() {
  const {state, go, openLinks} = useApp()

  const typingSound = state.settings?.typingSound ?? false
  useEffect(() => setTypingSound(typingSound), [typingSound])

  // Ctrl+1 to 5 jump between pages. Ctrl+V outside a text box pastes links to the Download page,
  // from wherever the focus is (read through the backend: WebKitGTK sends no paste event there).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return
      if (e.key.toLowerCase() === 'v') {
        if (editable(e.target) || document.querySelector('[aria-modal]')) return
        e.preventDefault()
        void api
          .readClipboard()
          .then((text) => {
            const links = linksIn(text)
            if (links.length > 0) openLinks(links.slice(0, MAX_DROPPED))
          })
          .catch(() => {})
        return
      }
      const i = NAV.findIndex((_, n) => e.key === String(n + 1))
      if (i >= 0) {
        e.preventDefault()
        go(NAV[i].page)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [go, openLinks])

  // A page switch moves the focus onto the new page (its heading, unless the page focuses a box
  // of its own), or it would stay on the hidden page's button and Tab would start over.
  const page = state.page
  const first = useRef(true)
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    const t = window.setTimeout(() => {
      const shown = document.querySelector<HTMLElement>('.page:not([hidden])')
      if (!shown || shown.contains(document.activeElement) || document.querySelector('[aria-modal]')) return
      const h1 = shown.querySelector<HTMLElement>('h1')
      if (!h1) return
      h1.tabIndex = -1
      h1.focus({preventScroll: true})
    }, 0)
    return () => window.clearTimeout(t)
  }, [page])

  if (!state.ready) {
    return (
      <div className="empty" style={{paddingTop: '30vh'}}>
        <Spinner />
      </div>
    )
  }
  if (state.loadError) {
    return (
      <div className="empty" style={{paddingTop: '20vh'}}>
        <h3>The app couldn't start</h3>
        <p>{state.loadError}</p>
      </div>
    )
  }

  return (
    <div className="app">
      <Sidebar />
      <main className="main">
        <Banners />
        {/* Download and Settings stay mounted so a half-reviewed link or unsaved edits survive a visit elsewhere. */}
        <div className="page" hidden={state.page !== 'download'}>
          <DownloadPage />
        </div>
        {/* Search stays mounted so results and ticks survive a look at the queue. */}
        <div className="page" hidden={state.page !== 'search'}>
          <SearchPage />
        </div>
        <div className="page" hidden={state.page !== 'queue'}>
          {state.page === 'queue' && <QueuePage />}
        </div>
        {/* Following stays mounted too: a channel open with videos ticked survives a look at the queue. */}
        <div className="page" hidden={state.page !== 'following'}>
          <FollowingPage />
        </div>
        <div className="page" hidden={state.page !== 'settings'}>
          <SettingsPage />
        </div>
      </main>
      <Toasts />
      <CloseConfirm />
      <DropLinks />
    </div>
  )
}

export default function App() {
  return (
    <AppProvider>
      <Shell />
    </AppProvider>
  )
}
