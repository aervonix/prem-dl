import type {ReactNode} from 'react'
import {api} from '../api'
import {Icon} from '../icons'
import type {IconName} from '../icons'
import {bytes} from '../format'
import {useApp, useJobs} from '../store'
import {Button, IconButton, Progress} from './ui'
import {EJS_DOCS} from './ErrorPanel'

export function Banner({tone, icon, title, children, actions}: {tone: 'warn' | 'bad' | 'info' | 'ok'; icon: IconName; title: string; children?: ReactNode; actions?: ReactNode}) {
  return (
    <div className={`banner ${tone}`} role={tone === 'bad' ? 'alert' : 'status'}>
      <span className="ic">
        <Icon name={icon} size={19} />
      </span>
      <div className="body">
        <div className="title">{title}</div>
        {children && <div className="text">{children}</div>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  )
}

/** Status notices that sit above every page: missing tools, updates, interrupted downloads. */
export function Banners() {
  const {state, dismiss, repairTools, go, attempt, installAppUpdate} = useApp()
  const jobs = useJobs()
  const {tools, dismissed} = state
  const ready = !tools.checking
  const interrupted = jobs.filter((j) => j.status === 'paused' && j.stage === 'Interrupted').length
  const running = jobs.filter((j) => ['starting', 'downloading', 'processing'].includes(j.status)).length

  const out: ReactNode[] = []

  if (ready && tools.ytdlpError) {
    out.push(
      <Banner
        key="tools"
        tone="bad"
        icon="alert"
        title="Prem-dl can't download right now"
        actions={
          tools.builtIn ? (
            <Button variant="primary" size="sm" icon="refresh" loading={state.repairing} onClick={() => void repairTools()}>
              Repair
            </Button>
          ) : (
            <Button variant="primary" size="sm" icon="settings" onClick={() => go('settings', 'updates')}>
              Details
            </Button>
          )
        }>
        {tools.ytdlpError} {tools.builtIn ? 'Repair puts them back. If that doesn’t help, install Prem-dl again.' : 'This build has no tools built in: put yt-dlp.exe next to it.'}
      </Banner>,
    )
  }

  const app = state.appUpdate
  const appProgress = state.appUpdateProgress
  if (app?.available && (appProgress || !dismissed[`app-${app.latest}`])) {
    const fetching = appProgress !== null && !state.appRestarting
    out.push(
      <Banner
        key="appupdate"
        tone="info"
        icon={state.appRestarting ? 'refresh' : 'download'}
        title={state.appRestarting ? `Installing Prem-dl ${app.latest}…` : `Prem-dl ${app.latest} is available`}
        actions={
          appProgress ? null : (
            <>
              {app.canInstall ? (
                <>
                  <Button
                    variant="primary"
                    size="sm"
                    icon="download"
                    disabled={running > 0 || state.repairing}
                    title={running > 0 ? 'Pause or finish the active downloads first' : state.repairing ? 'Wait for the repair to finish' : undefined}
                    onClick={() => void installAppUpdate()}>
                    Update now
                  </Button>
                  <Button size="sm" variant="ghost" icon="external" onClick={() => void api.openExternal(app.page)}>
                    What's new
                  </Button>
                </>
              ) : (
                <Button variant="primary" size="sm" icon="external" onClick={() => void api.openExternal(app.page)}>
                  Download
                </Button>
              )}
              <IconButton icon="x" label="Dismiss" onClick={() => dismiss(`app-${app.latest}`)} />
            </>
          )
        }>
        {state.appRestarting ? (
          'Prem-dl closes now and starts again when the update is installed.'
        ) : fetching ? (
          <div style={{marginTop: 6, maxWidth: 360}}>
            <Progress percent={appProgress.total > 0 ? (appProgress.done / appProgress.total) * 100 : -1} label={`Downloading Prem-dl ${app.latest}`} />
            <div style={{marginTop: 4}}>{appProgress.total > 0 ? `${bytes(appProgress.done)} of ${bytes(appProgress.total)}` : 'Starting…'}</div>
          </div>
        ) : app.canInstall ? (
          <>
            You have {app.current}. Update now downloads it{app.size > 0 && ` (${bytes(app.size)})`}, checks it against Prem-dl's release signature and restarts Prem-dl with the new
            version. Your settings, queue and followed channels stay.
          </>
        ) : (
          <>You have {app.current}. Download the new version from its release page.</>
        )}
      </Banner>,
    )
  }

  if (interrupted > 0 && !dismissed['interrupted']) {
    out.push(
      <Banner
        key="interrupted"
        tone="info"
        icon="pause"
        title={`${interrupted} download${interrupted === 1 ? ' was' : 's were'} paused when the app last closed`}
        actions={
          <>
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                void attempt(api.resumeAll())
                dismiss('interrupted')
                go('queue')
              }}>
              Resume all
            </Button>
            <IconButton icon="x" label="Dismiss" onClick={() => dismiss('interrupted')} />
          </>
        }>
        Partial files were kept, so they continue where they left off.
      </Banner>,
    )
  }

  if (ready && tools.ytdlp && !tools.ytdlpError && !tools.ffmpeg && !dismissed['ffmpeg']) {
    out.push(
      <Banner
        key="ffmpeg"
        tone="warn"
        icon="warning"
        title="Part of Prem-dl's tools is missing"
        actions={
          <>
            {tools.builtIn && (
              <Button variant="primary" size="sm" icon="refresh" loading={state.repairing} onClick={() => void repairTools()}>
                Repair
              </Button>
            )}
            <IconButton icon="x" label="Dismiss" onClick={() => dismiss('ffmpeg')} />
          </>
        }>
        Without ffmpeg, video and audio can't be merged, so you'd get lower-quality single files. Audio extraction, subtitle embedding and SponsorBlock also won't work.
      </Banner>,
    )
  }

  if (ready && tools.ytdlp && tools.jsRuntimes.length === 0 && !dismissed['js']) {
    out.push(
      <Banner
        key="js"
        tone="warn"
        icon="warning"
        title="No JavaScript runtime found"
        actions={
          <>
            <Button size="sm" icon="external" onClick={() => void api.openExternal(EJS_DOCS)}>
              Learn more
            </Button>
            <IconButton icon="x" label="Dismiss" onClick={() => dismiss('js')} />
          </>
        }>
        YouTube now needs Deno or Node.js (20+) to solve its challenges. Without one, some formats may be missing or downloads may fail. Install one and restart the app.
      </Banner>,
    )
  }

  if (out.length === 0) return null
  return <div className="banners">{out}</div>
}
