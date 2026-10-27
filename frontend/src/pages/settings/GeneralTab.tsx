import {useEffect, useRef, useState} from 'react'
import {api, errMessage} from '../../api'
import {Button, Card, Field, Switch} from '../../components/ui'
import {plural} from '../../format'
import {useApp} from '../../store'
import type {ArchiveInfo, Settings} from '../../types'
import type {TabProps} from './tab'

const TEMPLATES = [
  {value: '%(title)s [%(id)s].%(ext)s', label: 'Title [id]  (default, avoids name clashes)'},
  {value: '%(title)s.%(ext)s', label: 'Title'},
  {value: '%(uploader)s - %(title)s.%(ext)s', label: 'Uploader - Title'},
  {value: '%(upload_date>%Y-%m-%d)s %(title)s.%(ext)s', label: 'Date Title'},
  {value: '%(channel)s/%(title)s.%(ext)s', label: 'Channel folder / Title'},
]

/** How many videos "Remember downloaded videos" knows, with the file and a way to forget them. */
function ArchiveRow() {
  const {state, toast} = useApp()
  const [info, setInfo] = useState<ArchiveInfo | null>(null)
  const [confirming, setConfirming] = useState(false)
  // Downloads add to it, so it's read again whenever Settings is opened.
  const visible = state.page === 'settings'
  useEffect(() => {
    if (visible) api.getArchiveInfo().then(setInfo).catch(() => setInfo(null))
  }, [visible])
  useEffect(() => {
    if (!confirming) return
    const t = window.setTimeout(() => setConfirming(false), 4000)
    return () => window.clearTimeout(t)
  }, [confirming])
  if (!info) return null

  const clear = async () => {
    if (!confirming) return setConfirming(true)
    setConfirming(false)
    try {
      const res = await api.clearArchive()
      setInfo(res)
      if (res.error) toast('error', res.error)
      else toast('success', 'Forgot every downloaded video. They can all be downloaded again.')
    } catch (e) {
      toast('error', errMessage(e))
    }
  }
  return (
    <div className="hint" style={{marginTop: 10}}>
      {info.count === 0 ? 'No videos remembered yet.' : `${plural(info.count, 'video')} remembered.`}
      {info.count > 0 && (
        <>
          {' '}
          <button type="button" className="link" onClick={() => api.revealArchive().catch((e) => toast('error', errMessage(e)))}>
            Show file
          </button>
          {' · '}
          <button type="button" className="link" onClick={() => void clear()}>
            {confirming ? 'Click again to forget them all' : 'Clear'}
          </button>
        </>
      )}
    </div>
  )
}

/** Download folder, file names, theme, notifications and running in the background. */
export function GeneralTab({draft, upd}: TabProps) {
  const {attempt} = useApp()
  // "Custom template…" was picked but the template still matches a preset (not edited yet).
  // Discarding changes remounts this tab, which clears it.
  const [customPicked, setCustomPicked] = useState(false)
  const templateInput = useRef<HTMLInputElement>(null)
  const templatePreset = !customPicked && TEMPLATES.some((t) => t.value === draft.filenameTemplate) ? draft.filenameTemplate : 'custom'

  return (
    <>
      <Card id="general" title="General">
        <div className="stack">
          <Field label="Download folder">
            <div className="row">
              <input type="text" className="grow" value={draft.downloadDir} onChange={(e) => upd({downloadDir: e.target.value})} />
              <Button
                onClick={async () => {
                  const d = await api.pickDirectory('Choose a download folder', draft.downloadDir)
                  if (d) upd({downloadDir: d})
                }}>
                Browse…
              </Button>
              <Button variant="ghost" icon="external" onClick={() => void attempt(api.openDownloadFolder())}>
                Open
              </Button>
            </div>
          </Field>

          <div className="grid2">
            <Field label="File names" hint="How downloaded files are named.">
              <select
                value={templatePreset}
                onChange={(e) => {
                  setCustomPicked(e.target.value === 'custom')
                  if (e.target.value !== 'custom') return upd({filenameTemplate: e.target.value})
                  // The template field below is where a custom one is written.
                  templateInput.current?.focus()
                  templateInput.current?.select()
                }}>
                {TEMPLATES.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
                <option value="custom">Custom template…</option>
              </select>
            </Field>
            <Field label="Simultaneous downloads" hint="More is faster, but sites may throttle you.">
              <input type="number" min={1} max={10} value={draft.maxConcurrent} onChange={(e) => upd({maxConcurrent: Number(e.target.value)})} />
            </Field>
          </div>
          <Field
            label="File name template"
            hint={
              <>
                Uses the <button type="button" className="link" onClick={() => void api.openExternal('https://github.com/yt-dlp/yt-dlp#output-template')}>output template</button> syntax and must end with <code>.%(ext)s</code>.
              </>
            }>
            <input ref={templateInput} type="text" className="mono" value={draft.filenameTemplate} onChange={(e) => upd({filenameTemplate: e.target.value})} spellCheck={false} />
          </Field>

          <div className="grid2">
            <Field label="Theme">
              <select value={draft.theme} onChange={(e) => upd({theme: e.target.value as Settings['theme']})}>
                <option value="system">Match Windows</option>
                <option value="light">Light</option>
                <option value="dark">Dark</option>
              </select>
            </Field>
            <Switch
              checked={draft.useArchive}
              onChange={(v) => upd({useArchive: v})}
              label="Remember downloaded videos"
              description="Skips videos you've already downloaded, even if you moved or deleted the file. Handy for re-running a playlist."
            />
          </div>
          {draft.useArchive && <ArchiveRow />}
          <div className="grid2">
            <Switch
              checked={draft.notifyPopup}
              onChange={(v) => upd({notifyPopup: v})}
              label="Show finished downloads"
              description="A card in the corner with Open and Show in folder when a download finishes or fails."
            />
            <Switch
              checked={draft.notifySound}
              onChange={(v) => upd({notifySound: v})}
              label="Play a sound when downloads finish"
              description="A short click. A playlist finishing at once clicks once."
            />
            <Switch
              checked={draft.notifySystem}
              onChange={(v) => upd({notifySystem: v})}
              label="Windows notifications"
              description="When a download finishes while you're in another app, with Open and Show in folder buttons."
            />
          </div>
        </div>
      </Card>
      <Card id="background" title="In the background">
        <div className="stack">
          <div className="grid2">
            <Switch
              checked={draft.closeToTray}
              onChange={(v) => upd({closeToTray: v})}
              label="Keep running when the window is closed"
              description="Downloads carry on and Prem-dl waits in the notification area for links from Chrome. Right-click its icon to quit."
            />
            <Switch
              checked={draft.startWithWindows}
              onChange={(v) => upd({startWithWindows: v})}
              label="Start with Windows"
              description="Starts in the notification area when you sign in, without opening the window."
            />
          </div>
        </div>
      </Card>
    </>
  )
}
