import {useEffect, useMemo} from 'react'
import {Banners} from './components/Banners'
import {Toasts} from './components/Toasts'
import {Spinner} from './components/ui'
import {Icon} from './icons'
import type {IconName} from './icons'
import {DownloadPage} from './pages/DownloadPage'
import {QueuePage} from './pages/QueuePage'
import {SettingsPage} from './pages/SettingsPage'
import {AppProvider, useApp, useJobs} from './store'
import type {Page} from './store'

const NAV: {page: Page; label: string; icon: IconName}[] = [
  {page: 'download', label: 'Download', icon: 'download'},
  {page: 'queue', label: 'Queue', icon: 'list'},
  {page: 'settings', label: 'Settings', icon: 'settings'},
]

function Sidebar() {
  const {state, go} = useApp()
  const jobs = useJobs()
  const busy = useMemo(() => jobs.filter((j) => ['starting', 'downloading', 'processing', 'queued'].includes(j.status)).length, [jobs])
  const {tools, updateAvailable} = state

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark">
          <Icon name="download" size={16} />
        </span>
        yt-dlp GUI
      </div>
      {NAV.map((n) => (
        <button key={n.page} type="button" className="nav-item" aria-current={state.page === n.page ? 'page' : undefined} onClick={() => go(n.page)}>
          <Icon name={n.icon} />
          {n.label}
          {n.page === 'queue' && busy > 0 && <span className="nav-badge">{busy}</span>}
        </button>
      ))}
      <div className="sidebar-foot">
        <div className="row">
          <span className={`dot ${tools.checking ? '' : tools.ytdlp && !tools.ytdlpError ? 'ok' : 'bad'}`} />
          <span className="truncate">{tools.checking ? 'Checking yt-dlp…' : tools.ytdlp ? `yt-dlp ${tools.ytdlp.version}` : 'yt-dlp missing'}</span>
        </div>
        {updateAvailable && (
          <button type="button" className="chip" style={{alignSelf: 'flex-start'}} onClick={() => go('settings', 'updates')}>
            <Icon name="refresh" size={12} /> Update available
          </button>
        )}
      </div>
    </aside>
  )
}

function Shell() {
  const {state, go} = useApp()

  // Ctrl+1/2/3 jump between pages.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.altKey || e.shiftKey) return
      const i = ['1', '2', '3'].indexOf(e.key)
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
        <div className="page" hidden={state.page !== 'queue'}>
          {state.page === 'queue' && <QueuePage />}
        </div>
        <div className="page" hidden={state.page !== 'settings'}>
          <SettingsPage />
        </div>
      </main>
      <Toasts />
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
