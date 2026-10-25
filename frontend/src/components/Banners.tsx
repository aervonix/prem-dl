import type {ReactNode} from 'react'
import {api} from '../api'
import {Icon} from '../icons'
import type {IconName} from '../icons'
import {bytes} from '../format'
import {useApp, useJobs} from '../store'
import {Button, IconButton, Progress} from './ui'
import {EJS_DOCS, FFMPEG_INSTALL_CMD} from './ErrorPanel'

function Banner({tone, icon, title, children, actions}: {tone: 'warn' | 'bad' | 'info' | 'ok'; icon: IconName; title: string; children?: ReactNode; actions?: ReactNode}) {
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
  const {state, dismiss, updateYtdlp, go, toast, setUpdateAvailable} = useApp()
  const jobs = useJobs()
  const {tools, dismissed, toolProgress, updateAvailable, autoUpdated} = state
  const ready = !tools.checking
  const updating = toolProgress !== null
  const interrupted = jobs.filter((j) => j.status === 'paused' && j.stage === 'Interrupted').length

  const out: ReactNode[] = []

  if (ready && !tools.ytdlp) {
    out.push(
      <Banner
        key="noytdlp"
        tone="bad"
        icon="alert"
        title="yt-dlp isn't installed"
        actions={
          <Button variant="primary" size="sm" icon="download" loading={updating} onClick={() => void updateYtdlp()}>
            Download yt-dlp
          </Button>
        }>
        {updating ? (
          <div style={{marginTop: 6, minWidth: 220}}>
            <Progress percent={toolProgress && toolProgress.total > 0 ? (toolProgress.done / toolProgress.total) * 100 : -1} />
            <div style={{marginTop: 4}}>{toolProgress && toolProgress.total > 0 ? `${bytes(toolProgress.done)} of ${bytes(toolProgress.total)}` : 'Starting…'}</div>
          </div>
        ) : (
          <>
            {tools.ytdlpError || 'The app needs yt-dlp to download anything.'} The download is checked against the official SHA-256 checksum before it is used.
          </>
        )}
      </Banner>,
    )
  } else if (ready && tools.ytdlpError) {
    out.push(
      <Banner
        key="badytdlp"
        tone="bad"
        icon="alert"
        title="yt-dlp won't start"
        actions={
          <Button variant="primary" size="sm" icon="refresh" loading={updating} onClick={() => void updateYtdlp()}>
            Reinstall
          </Button>
        }>
        {tools.ytdlpError}
      </Banner>,
    )
  }

  if (updating && tools.ytdlp) {
    out.push(
      <Banner key="updating" tone="info" icon="refresh" title="Updating yt-dlp…">
        <div style={{marginTop: 6, maxWidth: 360}}>
          <Progress percent={toolProgress && toolProgress.total > 0 ? (toolProgress.done / toolProgress.total) * 100 : -1} />
        </div>
      </Banner>,
    )
  }

  if (updateAvailable && !updating && !dismissed['update']) {
    out.push(
      <Banner
        key="update"
        tone="info"
        icon="refresh"
        title={`yt-dlp ${updateAvailable.latest} is available`}
        actions={
          <>
            <Button variant="primary" size="sm" onClick={() => void updateYtdlp()}>
              Update now
            </Button>
            <IconButton
              icon="x"
              label="Dismiss"
              onClick={() => {
                dismiss('update')
                setUpdateAvailable(null)
              }}
            />
          </>
        }>
        You have {updateAvailable.current || 'an older version'}. Updates need no active downloads.
      </Banner>,
    )
  }

  if (autoUpdated?.ok && !dismissed['autoupdated']) {
    out.push(
      <Banner key="autoupdated" tone="ok" icon="checkCircle" title="yt-dlp was updated automatically" actions={<IconButton icon="x" label="Dismiss" onClick={() => dismiss('autoupdated')} />}>
        {autoUpdated.message}
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
                void api.resumeAll()
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

  if (ready && tools.ytdlp && !tools.ffmpeg && !dismissed['ffmpeg']) {
    out.push(
      <Banner
        key="ffmpeg"
        tone="warn"
        icon="warning"
        title="ffmpeg wasn't found"
        actions={
          <>
            <Button
              size="sm"
              icon="copy"
              onClick={() => {
                void api.copyText(FFMPEG_INSTALL_CMD)
                toast('info', `Copied “${FFMPEG_INSTALL_CMD}”. Run it in a terminal, then restart the app.`)
              }}>
              Copy install command
            </Button>
            <Button size="sm" variant="ghost" onClick={() => go('settings', 'advanced')}>
              Locate…
            </Button>
            <IconButton icon="x" label="Dismiss" onClick={() => dismiss('ffmpeg')} />
          </>
        }>
        Without it, video and audio can't be merged, so you'd get lower-quality single files — and audio extraction, subtitle embedding and SponsorBlock won't work.
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
        YouTube now needs Deno or Node.js (20+) so yt-dlp can solve its challenges. Without one, some formats may be missing or downloads may fail. Install one and restart the app.
      </Banner>,
    )
  }

  if (out.length === 0) return null
  return <div className="banners">{out}</div>
}
