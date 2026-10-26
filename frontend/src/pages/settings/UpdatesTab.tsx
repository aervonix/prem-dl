import {useEffect, useState} from 'react'
import {api} from '../../api'
import {EJS_DOCS} from '../../components/ErrorPanel'
import {FfmpegProgress, InstallFfmpegButton} from '../../components/FfmpegInstall'
import {Button, Card, Field, Progress, Switch} from '../../components/ui'
import {bytes} from '../../format'
import {Icon} from '../../icons'
import {useApp} from '../../store'
import type {Settings, UpdateInfo} from '../../types'
import type {TabProps} from './tab'

const SOURCE_LABEL: Record<string, string> = {
  setting: 'Custom path from Advanced settings',
  app: 'Next to this app',
  cwd: 'In the working folder',
  managed: 'Managed by this app',
  path: 'Found on your PATH',
}

/** Prem-dl's own version, with a check for a newer release and a way to get it. */
function AppVersionRow() {
  const {state, checkAppUpdate, installAppUpdate} = useApp()
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState('')
  const installing = state.appUpdateProgress !== null
  const u = state.appUpdate

  const check = async () => {
    setChecking(true)
    setError('')
    const res = await checkAppUpdate()
    setError(res?.error ?? '')
    setChecking(false)
  }

  return (
    <>
      <dt>Prem-dl</dt>
      <dd>
        <b>{state.version}</b>
        {u && !u.available && <span className="muted"> · up to date</span>}
        {u?.available && <span className="muted"> · {u.latest} is available</span>}
        {error && <div className="muted">Couldn't check: {error}</div>}
        <div className="row wrap" style={{marginTop: 6}}>
          <Button size="sm" variant="secondary" icon="search" loading={checking} onClick={() => void check()}>
            Check for a new version
          </Button>
          {u?.available &&
            (u.canInstall ? (
              <Button size="sm" variant="primary" icon="download" loading={installing} onClick={() => void installAppUpdate()}>
                Update to {u.latest}
              </Button>
            ) : (
              <Button size="sm" variant="primary" icon="external" onClick={() => void api.openExternal(u.page)}>
                Download {u.latest}
              </Button>
            ))}
        </div>
      </dd>
    </>
  )
}

/** Versions of Prem-dl, yt-dlp, ffmpeg and the JS runtime, with checks and updates. */
export function UpdatesTab({draft, upd}: TabProps) {
  const {state, attempt, updateYtdlp, setTools, setUpdateAvailable} = useApp()
  const [checking, setChecking] = useState(false)
  const [updateInfo, setLocalUpdateInfo] = useState<UpdateInfo | null>(null)

  // A check's result describes the version it ran against; once yt-dlp changes (updated here, from
  // the banner or automatically) it would still say "available" and offer "Update now".
  const ytdlpVersion = state.tools.ytdlp?.version
  useEffect(() => setLocalUpdateInfo(null), [ytdlpVersion])

  const checkUpdate = async () => {
    setChecking(true)
    const res = await attempt(api.checkUpdate())
    setChecking(false)
    if (!res) return
    setLocalUpdateInfo(res)
    setUpdateAvailable(res.available && !res.error ? res : null)
  }

  const refreshTools = async () => {
    const t = await attempt(api.refreshTools())
    if (t) setTools(t)
  }

  const tools = state.tools
  const updating = state.toolProgress !== null
  const pct = state.toolProgress && state.toolProgress.total > 0 ? (state.toolProgress.done / state.toolProgress.total) * 100 : -1

  return (
    <Card id="updates" title="Updates & tools" sub="Sites change often, so keeping yt-dlp current is the best fix for “it stopped working”.">
      <div className="stack">
        <dl className="kv">
          <AppVersionRow />
          <dt>yt-dlp</dt>
          <dd>
            {tools.checking ? (
              'Checking…'
            ) : tools.ytdlp ? (
              <>
                <b>{tools.ytdlp.version || 'unknown version'}</b> <span className="muted">· {SOURCE_LABEL[tools.ytdlp.source] ?? tools.ytdlp.source}</span>
                <div className="mono muted">{tools.ytdlp.path}</div>
              </>
            ) : (
              <span className="badge bad">Not installed</span>
            )}
          </dd>
          <dt>ffmpeg</dt>
          <dd>
            {tools.checking ? (
              'Checking…'
            ) : tools.ffmpeg ? (
              <>
                <b>{tools.ffmpeg.version || 'found'}</b>
                <div className="mono muted">{tools.ffmpeg.path}</div>
                {tools.ffmpegManaged && (
                  <div style={{marginTop: 6}}>
                    <InstallFfmpegButton variant="secondary" label="Update ffmpeg" />
                  </div>
                )}
              </>
            ) : (
              <>
                <span className="badge warn">Not found</span>
                <div style={{marginTop: 6}}>
                  <InstallFfmpegButton variant="secondary" />
                </div>
              </>
            )}
            <FfmpegProgress />
          </dd>
          <dt>JavaScript runtime</dt>
          <dd>
            {tools.checking ? (
              'Checking…'
            ) : tools.jsRuntimes.length > 0 ? (
              tools.jsRuntimes.map((r) => (
                <div key={r.name}>
                  <b>{r.name}</b> <span className="mono muted">{r.path}</span>
                </div>
              ))
            ) : (
              <>
                <span className="badge warn">None found</span> YouTube needs Deno or Node.js 20+. <button type="button" className="link" onClick={() => void api.openExternal(EJS_DOCS)}>Why?</button>
              </>
            )}
          </dd>
        </dl>

        {updating && (
          <div>
            <Progress percent={pct} label="Downloading yt-dlp" />
            <div className="hint" style={{marginTop: 4}}>
              {state.toolProgress && state.toolProgress.total > 0 ? `${bytes(state.toolProgress.done)} of ${bytes(state.toolProgress.total)}` : 'Working…'}
            </div>
          </div>
        )}

        {updateInfo && !updateInfo.error && (
          <div className={`banner ${updateInfo.available ? 'info' : 'ok'}`}>
            <span className="ic">
              <Icon name={updateInfo.available ? 'refresh' : 'checkCircle'} size={18} />
            </span>
            <div className="body">
              <div className="title">{updateInfo.available ? `Version ${updateInfo.latest} is available` : 'yt-dlp is up to date'}</div>
              <div className="text">
                {updateInfo.current ? `Installed: ${updateInfo.current}` : 'Not installed yet'} · Channel: {updateInfo.channel}
              </div>
            </div>
          </div>
        )}
        {updateInfo?.error && (
          <div className="banner bad">
            <span className="ic">
              <Icon name="alert" size={18} />
            </span>
            <div className="body">
              <div className="title">Couldn't check for updates</div>
              <div className="text">{updateInfo.error}</div>
            </div>
          </div>
        )}

        <div className="row wrap">
          <Button icon="search" loading={checking} onClick={() => void checkUpdate()}>
            Check for updates
          </Button>
          <Button variant="primary" icon="refresh" loading={updating} onClick={() => void updateYtdlp()}>
            {tools.ytdlp ? (updateInfo?.available ? 'Update now' : 'Reinstall latest') : 'Download yt-dlp'}
          </Button>
          <Button variant="ghost" icon="retry" onClick={() => void refreshTools()}>
            Re-detect tools
          </Button>
        </div>

        <div className="divider" style={{margin: '4px 0'}} />
        <div className="grid2">
          <Switch checked={draft.autoUpdate} onChange={(v) => upd({autoUpdate: v})} label="Update yt-dlp automatically" description="Checks a few seconds after launch (at most every 5 hours) and installs when no downloads are running." />
          <Field label="Update channel" hint="Nightly gets site fixes days earlier; stable is the safest.">
            <select value={draft.updateChannel} onChange={(e) => upd({updateChannel: e.target.value as Settings['updateChannel']})}>
              <option value="stable">Stable</option>
              <option value="nightly">Nightly</option>
              <option value="master">Master (bleeding edge)</option>
            </select>
          </Field>
          <Switch
            checked={draft.checkAppUpdates}
            onChange={(v) => upd({checkAppUpdates: v})}
            label="Check for Prem-dl updates"
            description="Looks for a new release shortly after launch and twice a day, and says so at the top of the window. Nothing installs without you."
          />
        </div>
      </div>
    </Card>
  )
}
