import {useEffect, useMemo} from 'react'
import {Banners} from './components/Banners'
import {CloseConfirm} from './components/CloseConfirm'
import {DropLinks} from './components/DropLinks'
import {Toasts} from './components/Toasts'
import {Spinner} from './components/ui'
import {Icon} from './icons'
import type {IconName} from './icons'
import {DownloadPage} from './pages/DownloadPage'
import {FollowingPage} from './pages/FollowingPage'
import {QueuePage} from './pages/QueuePage'
import {SearchPage} from './pages/SearchPage'
import {SettingsPage} from './pages/SettingsPage'
import {AppProvider, useApp, useJobs} from './store'
import type {Page} from './store'

const NAV: {page: Page; label: string; icon: IconName}[] = [
  {page: 'download', label: 'Download', icon: 'download'},
  {page: 'search', label: 'Search', icon: 'search'},
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
          aria-keyshortcuts={`Control+${i + 1}`}
          aria-current={state.page === n.page ? 'page' : undefined}
          onClick={() => go(n.page)}>
          <Icon name={n.icon} />
          {n.label}
          {n.page === 'queue' && busy > 0 && <span className="nav-badge">{busy}</span>}
        </button>
      ))}
      <div className="sidebar-foot">
        {tools.checking ? (
          <span>PREM-DL - CHECKING</span>
        ) : tools.ytdlpError ? (
          <button type="button" className="foot-link bad" onClick={() => go('settings', 'updates')}>
            PREM-DL - NEEDS REPAIR
          </button>
        ) : state.appUpdate?.available ? (
          <button type="button" className="foot-link" onClick={() => go('settings', 'updates')}>
            PREM-DL - UPDATE AVAILABLE
          </button>
        ) : (
          <span>PREM-DL {state.version}</span>
        )}
      </div>
    </aside>
  )
}

function Shell() {
  const {state, go} = useApp()

  // Ctrl+1 to 5 jump between pages.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.altKey || e.shiftKey) return
      const i = NAV.findIndex((_, n) => e.key === String(n + 1))
      if (i >= 0) {
        e.preventDefault()
        go(NAV[i].page)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [go])

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
