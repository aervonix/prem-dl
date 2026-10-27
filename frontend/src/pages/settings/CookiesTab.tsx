import {useEffect, useState} from 'react'
import {api} from '../../api'
import {ErrorPanel} from '../../components/ErrorPanel'
import {Button, Card, Field, Modal} from '../../components/ui'
import {plural} from '../../format'
import {Icon} from '../../icons'
import {useApp} from '../../store'
import type {CookieInfo, CookieTest, Settings} from '../../types'
import type {TabProps} from './tab'

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

const COOKIE_FAQ = 'https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp'

function PasteCookiesModal({initial = '', onClose, onImport}: {initial?: string; onClose: () => void; onImport: (text: string) => Promise<boolean>}) {
  const [text, setText] = useState(initial)
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

/** Where cookies come from: off, a browser, or a cookies.txt (imported, pasted or from Chrome). */
export function CookiesTab({draft, saved, upd}: TabProps) {
  const {state, setSettings, toast, attempt} = useApp()
  const [cookieInfo, setCookieInfo] = useState<CookieInfo | null>(null)
  const [cookieTest, setCookieTest] = useState<CookieTest | null>(null)
  const [testing, setTesting] = useState(false)
  const [pasteOpen, setPasteOpen] = useState(false)
  // A cookies.txt dropped on the window: shown in the paste dialog to be checked and imported.
  const [pasteText, setPasteText] = useState('')
  const [chromeBusy, setChromeBusy] = useState(false)
  const [helperPath, setHelperPath] = useState('')

  useEffect(() => {
    if (!state.droppedCookies) return
    setPasteText(state.droppedCookies.text)
    setPasteOpen(true)
  }, [state.droppedCookies])

  // Summarise the cookie file whenever it changes. The file stays saved when cookies are turned
  // off, so picking "cookies.txt file" again shows it before the change is saved.
  const cookieFile = draft.cookies.mode === 'file' ? saved.cookies.file : ''
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

  const applyCookieResponse = (res: {settings: Settings; error: string; info: CookieInfo}): boolean => {
    if (res.error) {
      toast('error', res.error)
      return false
    }
    // Cookie changes are saved by the backend straight away. Take its whole copy, not `saved`:
    // the Chrome flow can wait minutes, and settings saved meanwhile would be rolled back. The
    // Settings page keeps unsaved edits in the draft.
    setSettings(res.settings)
    upd({cookies: res.settings.cookies})
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

  const browser = BROWSERS.find((b) => b.id === draft.cookies.browser)

  return (
    <>
      <Card
        id="cookies"
        title="Cookies"
        sub="Lets Prem-dl act as you: for age-restricted, members-only or private videos, and to get past YouTube's “confirm you're not a bot” check.">
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
              The helper also adds Download with Prem-dl to Chrome's toolbar and to the menu you get when you right-click a page or a link. Already added it? After Prem-dl
              updates, click the reload arrow on the helper in chrome://extensions to get its new features.
            </p>
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
                    Recent Chrome-based browsers encrypt cookies in a way Prem-dl can't read, which fails with “Failed to decrypt with DPAPI”. Use <b>Firefox</b>, or export a <b>cookies.txt file</b>
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
                <button type="button" className="link" onClick={() => void api.openExternal(COOKIE_FAQ)}>cookie FAQ</button>.
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

      {pasteOpen && (
        <PasteCookiesModal
          // A new drop replaces what's in the dialog.
          key={state.droppedCookies?.n ?? 0}
          initial={pasteText}
          onClose={() => {
            setPasteOpen(false)
            setPasteText('')
          }}
          onImport={async (text) => {
            const res = await attempt(api.importCookieText(text))
            if (!res) return false
            const ok = applyCookieResponse(res)
            if (ok) toast('success', `Imported ${plural(res.info.count, 'cookie')} from ${plural(res.info.domainCount, 'site')}`)
            return ok
          }}
        />
      )}
    </>
  )
}
