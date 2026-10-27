import {useState} from 'react'
import {api} from '../../api'
import {EJS_DOCS} from '../../components/ErrorPanel'
import {Button, Card, Switch} from '../../components/ui'
import {useApp} from '../../store'
import type {TabProps} from './tab'

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

/** Prem-dl's version and updates, the tools built into it, and the JavaScript runtime. */
export function UpdatesTab({draft, upd}: TabProps) {
  const {state, attempt, repairTools, setTools} = useApp()
  const tools = state.tools

  const refreshTools = async () => {
    const t = await attempt(api.refreshTools())
    if (t) setTools(t)
  }

  return (
    <Card id="updates" title="Updates" sub="Fixes for sites that changed come with new versions of Prem-dl.">
      <div className="stack">
        <dl className="kv">
          <AppVersionRow />
          <dt>Download tools</dt>
          <dd>
            {tools.checking ? (
              'Checking…'
            ) : tools.ytdlpError ? (
              <>
                <span className="badge bad">Not working</span> <span className="muted">{tools.ytdlpError}</span>
              </>
            ) : (
              <>
                {tools.builtIn ? 'Built in' : 'Found on this computer'}
                {!tools.ffmpeg && <span className="muted"> · ffmpeg is missing, so merging and converting won't work</span>}
                {/* For bug reports: which tools this release carries. */}
                <div className="hint">
                  {[tools.ytdlp?.version && `yt-dlp ${tools.ytdlp.version}`, tools.ffmpeg?.version && `ffmpeg ${tools.ffmpeg.version}`].filter(Boolean).join(' · ')}
                </div>
              </>
            )}
            <div className="row wrap" style={{marginTop: 6}}>
              {tools.builtIn ? (
                <Button size="sm" variant={tools.ytdlpError || !tools.ffmpeg ? 'primary' : 'secondary'} icon="refresh" loading={state.repairing} onClick={() => void repairTools()}>
                  Repair
                </Button>
              ) : (
                <Button size="sm" variant="secondary" icon="retry" onClick={() => void refreshTools()}>
                  Look again
                </Button>
              )}
            </div>
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

        <div className="divider" style={{margin: '4px 0'}} />
        <Switch
          checked={draft.checkAppUpdates}
          onChange={(v) => upd({checkAppUpdates: v})}
          label="Check for Prem-dl updates"
          description="Looks for a new release shortly after launch and twice a day, and says so at the top of the window. Nothing installs without you."
        />
      </div>
    </Card>
  )
}
