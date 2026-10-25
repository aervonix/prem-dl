import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {api, errMessage} from '../api'
import {ErrorPanel, EJS_DOCS, FFMPEG_INSTALL_CMD} from '../components/ErrorPanel'
import {OptionsPanel} from '../components/OptionsPanel'
import {Button, Card, Field, Modal, Progress, Switch} from '../components/ui'
import {bytes, plural} from '../format'
import {Icon} from '../icons'
import {useApp} from '../store'
import type {CookieInfo, CookieTest, Settings, UpdateInfo} from '../types'

const SECTIONS = [
  {id: 'general', label: 'General'},
  {id: 'defaults', label: 'Default options'},
  {id: 'cookies', label: 'Cookies'},
  {id: 'network', label: 'Network'},
  {id: 'updates', label: 'Updates & tools'},
  {id: 'advanced', label: 'Advanced'},
] as const
type SectionId = (typeof SECTIONS)[number]['id']
const isSection = (id: string): id is SectionId => SECTIONS.some((s) => s.id === id)

const TEMPLATES = [
  {value: '%(title)s [%(id)s].%(ext)s', label: 'Title [id]  (default, avoids name clashes)'},
  {value: '%(title)s.%(ext)s', label: 'Title'},
  {value: '%(uploader)s - %(title)s.%(ext)s', label: 'Uploader - Title'},
  {value: '%(upload_date>%Y-%m-%d)s %(title)s.%(ext)s', label: 'Date Title'},
  {value: '%(channel)s/%(title)s.%(ext)s', label: 'Channel folder / Title'},
]

const BROWSERS = [
  {id: 'firefox', label: 'Firefox', chromium: false},
  {id: 'chrome', label: 'Chrome', chromium: true},
  {id: 'edge', label: 'Edge', chromium: true},
  {id: 'brave', label: 'Brave', chromium: true},
  {id: 'chromium', label: 'Chromium', chromium: true},
  {id: 'opera', label: 'Opera', chromium: true},
  {id: 'vivaldi', label: 'Vivaldi', chromium: true},
  {id: 'whale', label: 'Whale', chromium: true},
]

const SOURCE_LABEL: Record<string, string> = {
  setting: 'Custom path from Advanced settings',
  app: 'Next to this app',
  cwd: 'In the working folder',
  managed: 'Managed by this app',
  path: 'Found on your PATH',
}

const COOKIE_FAQ = 'https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp'

function PasteCookiesModal({onClose, onImport}: {onClose: () => void; onImport: (text: string) => Promise<boolean>}) {
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <Modal
      title="Paste cookies"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!text.trim()}
            onClick={async () => {
              setBusy(true)
              const ok = await onImport(text)
              setBusy(false)
              if (ok) onClose()
            }}>
            Import
          </Button>
        </>
      }>
      <p className="muted" style={{marginBottom: 10}}>
        Paste the contents of a cookies.txt file (Netscape format, starting with <code># Netscape HTTP Cookie File</code>).
      </p>
      <textarea rows={12} className="mono" value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} placeholder="# Netscape HTTP Cookie File&#10;.youtube.com&#9;TRUE&#9;/&#9;TRUE&#9;…" aria-label="Cookies text" />
    </Modal>
  )
}

export function SettingsPage() {
  const {state, setSettings, toast, attempt, updateYtdlp, setTools, setUpdateAvailable} = useApp()
  const saved = state.settings
  const [draft, setDraft] = useState<Settings | null>(saved)
  const [saving, setSaving] = useState(false)
  const [cookieInfo, setCookieInfo] = useState<CookieInfo | null>(null)
  const [cookieTest, setCookieTest] = useState<CookieTest | null>(null)
  const [testing, setTesting] = useState(false)
  const [pasteOpen, setPasteOpen] = useState(false)
  const [checking, setChecking] = useState(false)
  const [updateInfo, setLocalUpdateInfo] = useState<UpdateInfo | null>(null)
  const [chromeBusy, setChromeBusy] = useState(false)
  const [helperPath, setHelperPath] = useState('')

  const dirty = useMemo(() => !!saved && !!draft && JSON.stringify(saved) !== JSON.stringify(draft), [saved, draft])

  // Follow saved changes (imports, "save as defaults", changes the backend makes): fields the user
  // hasn't touched take the new value, unsaved edits are kept.
  const prevSaved = useRef(saved)
  useEffect(() => {
    const prev = prevSaved.current
    prevSaved.current = saved
    if (!saved || prev === saved) return
    setDraft((d) => {
      if (!d || !prev) return saved
      const next: Record<string, unknown> = {...d}
      for (const k of Object.keys(saved) as (keyof Settings)[]) {
        if (JSON.stringify(d[k]) === JSON.stringify(prev[k])) next[k] = saved[k]
      }
      return next as unknown as Settings
    })
  }, [saved])

  // One section shows at a time. Switching starts at the top of the page.
  const [tab, setTab] = useState<SectionId>('general')
  const rootRef = useRef<HTMLDivElement>(null)
  const openTab = useCallback((id: SectionId) => {
    setTab(id)
    rootRef.current?.closest('.page')?.scrollTo({top: 0})
  }, [])

  // Open the section requested from elsewhere (error panels, banners), once per request: the
  // request stays in state, and opening Settings later must not jump there again.
  const handledFor = useRef(0)
  useEffect(() => {
    if (!state.section || state.page !== 'settings' || handledFor.current === state.section.n) return
    handledFor.current = state.section.n
    if (isSection(state.section.id)) openTab(state.section.id)
  }, [state.section, state.page, openTab])

  // Summarise the cookie file whenever it changes.
  const cookieFile = saved?.cookies.mode === 'file' ? saved.cookies.file : ''
  useEffect(() => {
    let alive = true
    setCookieTest(null)
    if (!cookieFile) {
      setCookieInfo(null)
      return
    }
    api.inspectCookieFile(cookieFile).then((i) => alive && setCookieInfo(i)).catch(() => alive && setCookieInfo(null))
    return () => {
      alive = false
    }
  }, [cookieFile])

  const upd = useCallback((patch: Partial<Settings>) => setDraft((d) => (d ? {...d, ...patch} : d)), [])

  if (!saved || !draft) return null

  const save = async () => {
    setSaving(true)
    try {
      const res = await api.saveSettings(draft)
      if (res.error) {
        toast('error', res.error)
        return
      }
      setSettings(res.settings)
      setDraft(res.settings)
      toast('success', 'Settings saved')
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setSaving(false)
    }
  }

  const applyCookieResponse = (res: {settings: Settings; error: string; info: CookieInfo}): boolean => {
    if (res.error) {
      toast('error', res.error)
      return false
    }
    // Cookie changes are saved by the backend straight away; fold them into both copies so any
    // other unsaved edits survive.
    setSettings({...saved, cookies: res.settings.cookies})
    setDraft((d) => (d ? {...d, cookies: res.settings.cookies} : d))
    setCookieInfo(res.info)
    setCookieTest(null)
    return true
  }

  const importCookieFile = async () => {
    const path = await api.pickFile('Choose a cookies.txt file', 'Cookies (*.txt)', '*.txt')
    if (!path) return
    const res = await attempt(api.importCookieFile(path))
    if (res && applyCookieResponse(res)) toast('success', `Imported ${plural(res.info.count, 'cookie')} from ${plural(res.info.domainCount, 'site')}`)
  }

  const getChromeCookies = async () => {
    setChromeBusy(true)
    const res = await attempt(api.getChromeCookies())
    setChromeBusy(false)
    if (res && applyCookieResponse(res)) toast('success', `Got ${plural(res.info.count, 'YouTube cookie')} from Chrome`)
  }

  const prepareHelper = async () => {
    const res = await attempt(api.chromeHelperSetup())
    if (!res) return
    if (res.error) return toast('error', `Couldn't write the helper: ${res.error}`)
    setHelperPath(res.path)
  }

  const cookiesDirty = JSON.stringify(saved.cookies) !== JSON.stringify(draft.cookies)
  const runCookieTest = async () => {
    setTesting(true)
    setCookieTest(null)
    const res = await attempt(api.testCookies())
    setTesting(false)
    if (res) setCookieTest(res)
  }

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

  const browser = BROWSERS.find((b) => b.id === draft.cookies.browser)
  const tools = state.tools
  const updating = state.toolProgress !== null
  const pct = state.toolProgress && state.toolProgress.total > 0 ? (state.toolProgress.done / state.toolProgress.total) * 100 : -1
  const templatePreset = TEMPLATES.some((t) => t.value === draft.filenameTemplate) ? draft.filenameTemplate : 'custom'

  return (
    <div className="settings-layout" ref={rootRef}>
      <nav className="settings-nav" aria-label="Settings sections">
        {SECTIONS.map((s) => (
          <button key={s.id} type="button" aria-current={tab === s.id ? 'page' : undefined} onClick={() => openTab(s.id)}>
            {s.label}
          </button>
        ))}
      </nav>

      <div>
        <div className="page-head">
          <h1>Settings</h1>
        </div>

        <div className="stack">
          {/* ---------------- general ---------------- */}
          {tab === 'general' && (
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
                      onChange={(e) => e.target.value !== 'custom' && upd({filenameTemplate: e.target.value})}>
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
                      Uses yt-dlp's <a onClick={() => void api.openExternal('https://github.com/yt-dlp/yt-dlp#output-template')}>output template</a> syntax and must end with <code>.%(ext)s</code>.
                    </>
                  }>
                  <input type="text" className="mono" value={draft.filenameTemplate} onChange={(e) => upd({filenameTemplate: e.target.value})} spellCheck={false} />
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
              </div>
            </Card>
          )}

          {/* ---------------- defaults ---------------- */}
          {tab === 'defaults' && (
            <Card id="defaults" title="Default options" sub="What new downloads start with. You can still change them for each download.">
              <OptionsPanel value={draft.defaults} onChange={(defaults) => upd({defaults})} mode="defaults" playlist />
            </Card>
          )}

          {/* ---------------- cookies ---------------- */}
          {tab === 'cookies' && (
            <Card
              id="cookies"
              title="Cookies"
              sub="Lets yt-dlp act as you: for age-restricted, members-only or private videos, and to get past YouTube's “confirm you're not a bot” check.">
              <div className="radio-cards" role="group" aria-label="Cookie source">
                {(
                  [
                    {v: 'none', t: 'Off', d: 'Download anonymously'},
                    {v: 'browser', t: 'From a browser', d: 'Read cookies directly'},
                    {v: 'file', t: 'cookies.txt file', d: 'Import an exported file'},
                  ] as const
                ).map((o) => (
                  <button key={o.v} type="button" className="radio-card" aria-pressed={draft.cookies.mode === o.v} onClick={() => upd({cookies: {...draft.cookies, mode: o.v}})}>
                    <div className="t">{o.t}</div>
                    <div className="d">{o.d}</div>
                  </button>
                ))}
              </div>
  
              <div className="stack" style={{marginTop: 16}}>
                <div>
                  <div style={{fontWeight: 600}}>Use your YouTube account from Chrome</div>
                  <div className="hint">
                    For YouTube Premium quality and videos that need you signed in. Chrome asks you to approve, then the app saves your YouTube cookies as its cookies.txt file. Do it again if
                    downloads start asking you to sign in.
                  </div>
                </div>
                <div className="row wrap">
                  <Button variant="primary" loading={chromeBusy} onClick={() => void getChromeCookies()}>
                    Get cookies from Chrome
                  </Button>
                  {chromeBusy && (
                    <>
                      <span className="hint">Waiting for you to allow it in Chrome…</span>
                      <Button variant="ghost" onClick={() => void api.cancelChromeCookies()}>
                        Cancel
                      </Button>
                    </>
                  )}
                </div>
                <details>
                  <summary style={{cursor: 'pointer', fontWeight: 580}}>Set up once: add the helper to Chrome</summary>
                  <ol className="muted" style={{margin: '8px 0 0', paddingLeft: 20, lineHeight: 1.8}}>
                    <li>
                      <Button size="sm" onClick={() => void prepareHelper()}>
                        Show the helper folder
                      </Button>{' '}
                      {helperPath && <code>{helperPath}</code>}
                    </li>
                    <li>
                      <Button size="sm" onClick={() => void attempt(api.openChromeExtensions())}>
                        Open chrome://extensions
                      </Button>{' '}
                      in the Chrome profile you use for YouTube.
                    </li>
                    <li>Turn on Developer mode (top right), click Load unpacked, and choose the helper folder.</li>
                    <li>Come back here and click Get cookies from Chrome.</li>
                  </ol>
                  <p className="hint" style={{marginTop: 8}}>
                    The helper only reads youtube.com cookies, only after you click Allow and approve Chrome's prompt, and only sends them to this app on this computer. Chrome's access is
                    removed again straight after. Anyone with these cookies can use your signed-in YouTube session, so the app keeps them in its own folder.
                  </p>
                </details>
              </div>
  
              {draft.cookies.mode === 'browser' && (
                <div className="stack" style={{marginTop: 16}}>
                  <div className="grid2">
                    <Field label="Browser">
                      <select value={draft.cookies.browser} onChange={(e) => upd({cookies: {...draft.cookies, browser: e.target.value}})}>
                        {BROWSERS.map((b) => (
                          <option key={b.id} value={b.id}>
                            {b.label}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label="Profile (optional)" hint="Leave empty for the default profile.">
                      <input type="text" value={draft.cookies.profile} placeholder="Default" onChange={(e) => upd({cookies: {...draft.cookies, profile: e.target.value}})} />
                    </Field>
                  </div>
                  {browser?.chromium && (
                    <div className="banner warn">
                      <span className="ic">
                        <Icon name="warning" size={18} />
                      </span>
                      <div className="body">
                        <div className="title">{browser.label} may not work on Windows</div>
                        <div className="text">
                          Recent Chrome-based browsers encrypt cookies in a way yt-dlp can't read, which fails with “Failed to decrypt with DPAPI”. Use <b>Firefox</b>, or export a <b>cookies.txt file</b>
                          and import it instead. Use “Test” to see whether yours works.
                        </div>
                      </div>
                    </div>
                  )}
                  <div className="row">
                    <Button icon="checkCircle" loading={testing} disabled={cookiesDirty} onClick={() => void runCookieTest()}>
                      Test cookies
                    </Button>
                    {cookiesDirty && <span className="hint">Save your changes to test them.</span>}
                  </div>
                </div>
              )}
  
              {draft.cookies.mode === 'file' && (
                <div className="stack" style={{marginTop: 16}}>
                  {cookieInfo && cookieInfo.valid && (
                    <div className="banner ok">
                      <span className="ic">
                        <Icon name="checkCircle" size={18} />
                      </span>
                      <div className="body">
                        <div className="title">
                          {plural(cookieInfo.count, 'cookie')} from {plural(cookieInfo.domainCount, 'site')}
                        </div>
                        <div className="text">
                          {cookieInfo.domains.slice(0, 6).join(', ')}
                          {cookieInfo.domainCount > 6 ? `, +${cookieInfo.domainCount - 6} more` : ''}
                          {cookieInfo.expired > 0 && <> · {cookieInfo.expired} expired</>}
                        </div>
                      </div>
                    </div>
                  )}
                  {cookieInfo && !cookieInfo.valid && (
                    <div className="banner bad">
                      <span className="ic">
                        <Icon name="alert" size={18} />
                      </span>
                      <div className="body">
                        <div className="title">This cookies file can't be used</div>
                        <div className="text">{cookieInfo.problem}</div>
                      </div>
                    </div>
                  )}
                  {!cookieFile && <p className="muted">No file imported yet.</p>}
                  <div className="row wrap">
                    <Button variant="primary" icon="download" onClick={() => void importCookieFile()}>
                      {cookieFile ? 'Replace with another file…' : 'Import cookies.txt…'}
                    </Button>
                    <Button icon="clipboard" onClick={() => setPasteOpen(true)}>
                      Paste text…
                    </Button>
                    {cookieFile && (
                      <>
                        <Button icon="checkCircle" loading={testing} disabled={cookiesDirty} onClick={() => void runCookieTest()}>
                          Test
                        </Button>
                        <Button
                          variant="ghost"
                          className="danger"
                          icon="trash"
                          onClick={async () => {
                            const res = await attempt(api.removeCookieFile())
                            if (res && applyCookieResponse(res)) toast('info', 'Cookies removed')
                          }}>
                          Remove
                        </Button>
                      </>
                    )}
                  </div>
                  <details>
                    <summary style={{cursor: 'pointer', fontWeight: 580}}>How do I get a cookies.txt file?</summary>
                    <ol className="muted" style={{margin: '8px 0 0', paddingLeft: 20, lineHeight: 1.7}}>
                      <li>Install a “cookies.txt” export extension in your browser (search the extension store for “Get cookies.txt LOCALLY”).</li>
                      <li>Open the site (e.g. youtube.com) while signed in.</li>
                      <li>Click the extension and export the cookies for that site in Netscape format.</li>
                      <li>Import the saved file here.</li>
                    </ol>
                    <p className="hint" style={{marginTop: 8}}>
                      Treat the file like a password: anyone with it can use your logged-in session. This app keeps its own private copy and gives each download a temporary one. See the{' '}
                      <a onClick={() => void api.openExternal(COOKIE_FAQ)}>yt-dlp FAQ</a>.
                    </p>
                  </details>
                </div>
              )}
  
              {cookieTest && (
                <div style={{marginTop: 14}}>
                  {cookieTest.ok ? (
                    <div className="banner ok">
                      <span className="ic">
                        <Icon name="checkCircle" size={18} />
                      </span>
                      <div className="body">
                        <div className="title">Cookies work</div>
                        <div className="text">
                          Loaded {plural(cookieTest.count, 'cookie')} from {cookieTest.browser}.
                        </div>
                      </div>
                    </div>
                  ) : (
                    cookieTest.error && <ErrorPanel error={cookieTest.error} />
                  )}
                </div>
              )}
            </Card>
          )}

          {/* ---------------- network ---------------- */}
          {tab === 'network' && (
            <Card id="network" title="Network">
              <div className="grid2">
                <Field label="Speed limit" hint="Per download, e.g. 500K or 2M. Empty = unlimited.">
                  <input type="text" value={draft.rateLimit} placeholder="Unlimited" onChange={(e) => upd({rateLimit: e.target.value})} />
                </Field>
                <Field label="Proxy" hint="e.g. http://127.0.0.1:8080 or socks5://127.0.0.1:1080. Also used for update checks.">
                  <input type="text" value={draft.proxy} placeholder="None" onChange={(e) => upd({proxy: e.target.value})} spellCheck={false} />
                </Field>
                <Field label="Parallel fragments" hint="Threads used per download for segmented (DASH/HLS) streams.">
                  <input type="number" min={1} max={16} value={draft.concurrentFragments} onChange={(e) => upd({concurrentFragments: Number(e.target.value)})} />
                </Field>
                <Field label="Retries" hint="How often to retry a failed connection or fragment.">
                  <input type="number" min={0} max={50} value={draft.retries} onChange={(e) => upd({retries: Number(e.target.value)})} />
                </Field>
              </div>
            </Card>
          )}

          {/* ---------------- updates & tools ---------------- */}
          {tab === 'updates' && (
            <Card id="updates" title="Updates & tools" sub="Sites change often, so keeping yt-dlp current is the best fix for “it stopped working”.">
              <div className="stack">
                <dl className="kv">
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
                      </>
                    ) : (
                      <>
                        <span className="badge warn">Not found</span>{' '}
                        <a
                          onClick={() => {
                            void api.copyText(FFMPEG_INSTALL_CMD)
                            toast('info', `Copied “${FFMPEG_INSTALL_CMD}”`)
                          }}>
                          copy install command
                        </a>
                      </>
                    )}
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
                        <span className="badge warn">None found</span> YouTube needs Deno or Node.js 20+. <a onClick={() => void api.openExternal(EJS_DOCS)}>Why?</a>
                      </>
                    )}
                  </dd>
                </dl>
  
                {updating && (
                  <div>
                    <Progress percent={pct} />
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
                </div>
              </div>
            </Card>
          )}

          {/* ---------------- advanced ---------------- */}
          {tab === 'advanced' && (
            <Card id="advanced" title="Advanced">
              <div className="stack">
                <Field label="yt-dlp location" hint="Leave empty to find it automatically (next to the app, in the working folder, the app's own copy, then PATH).">
                  <div className="row">
                    <input type="text" className="grow" value={draft.ytdlpPath} placeholder="Automatic" onChange={(e) => upd({ytdlpPath: e.target.value})} spellCheck={false} />
                    <Button
                      onClick={async () => {
                        const p = await api.pickFile('Choose yt-dlp', 'Programs (*.exe)', '*.exe')
                        if (p) upd({ytdlpPath: p})
                      }}>
                      Browse…
                    </Button>
                  </div>
                </Field>
                <Field label="ffmpeg location" hint="A folder containing ffmpeg.exe, or the file itself. Leave empty to find it automatically.">
                  <div className="row">
                    <input type="text" className="grow" value={draft.ffmpegPath} placeholder="Automatic" onChange={(e) => upd({ffmpegPath: e.target.value})} spellCheck={false} />
                    <Button
                      onClick={async () => {
                        const p = await api.pickDirectory('Choose the folder containing ffmpeg.exe', draft.ffmpegPath)
                        if (p) upd({ffmpegPath: p})
                      }}>
                      Browse…
                    </Button>
                  </div>
                </Field>
                <Field
                  label="Extra yt-dlp arguments"
                  hint="Added to every download and applied last, so they can override the options above. For example: --sleep-interval 3 --extractor-args &quot;youtube:player_client=web&quot;">
                  <textarea rows={2} className="mono" value={draft.extraArgs} onChange={(e) => upd({extraArgs: e.target.value})} spellCheck={false} />
                </Field>
                <div className="row wrap">
                  <Button icon="folder" onClick={() => void attempt(api.openDataFolder())}>
                    Open app data folder
                  </Button>
                  <span className="hint">Settings and the queue live in {state.dataDir}</span>
                </div>
                <div className="hint">Prem-dl {state.version}</div>
              </div>
            </Card>
          )}
        </div>

        {dirty && (
          <div className="savebar" role="region" aria-label="Unsaved changes">
            <span className="grow">You have unsaved changes</span>
            <Button variant="ghost" onClick={() => setDraft(saved)}>
              Discard
            </Button>
            <Button variant="primary" icon="save" loading={saving} onClick={() => void save()}>
              Save changes
            </Button>
          </div>
        )}
      </div>

      {pasteOpen && (
        <PasteCookiesModal
          onClose={() => setPasteOpen(false)}
          onImport={async (text) => {
            const res = await attempt(api.importCookieText(text))
            if (!res) return false
            const ok = applyCookieResponse(res)
            if (ok) toast('success', `Imported ${plural(res.info.count, 'cookie')} from ${plural(res.info.domainCount, 'site')}`)
            return ok
          }}
        />
      )}
    </div>
  )
}
