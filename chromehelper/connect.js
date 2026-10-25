// Opened by Prem-dl as chrome-extension://<id>/connect.html#<port>.<token>. The app is
// listening on 127.0.0.1:<port> for this one request and rejects anything without the token.
const ORIGINS = ['https://*.youtube.com/*']
const [port, token] = location.hash.slice(1).split('.')
const valid = /^\d{2,5}$/.test(port ?? '') && /^[0-9a-f]{64}$/.test(token ?? '')
const base = `http://127.0.0.1:${port}`

const allow = document.getElementById('allow')
const deny = document.getElementById('deny')
const status = document.getElementById('status')

function show(text, bad = false) {
  status.textContent = text
  status.className = bad ? 'bad' : ''
}

function done(text, bad) {
  allow.disabled = true
  deny.disabled = true
  show(text, bad)
}

async function send(path, body) {
  const res = await fetch(base + path, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({token, ...body}),
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
}

if (!valid) {
  done('This page has to be opened from Prem-dl (Settings, Cookies, Get cookies from Chrome).', true)
} else {
  // Tells the app the helper is installed and the question is on screen.
  fetch(`${base}/hello?token=${token}`).catch(() => {})
}

allow.addEventListener('click', async () => {
  allow.disabled = true
  let granted = false
  try {
    granted = await chrome.permissions.request({origins: ORIGINS})
    if (!granted) {
      await send('/cookies', {cancelled: true}).catch(() => {})
      return done('Nothing was shared.')
    }
    const cookies = await chrome.cookies.getAll({domain: 'youtube.com'})
    if (cookies.length === 0) {
      await send('/cookies', {cancelled: true}).catch(() => {})
      return done('No YouTube cookies found. Sign in to YouTube in this Chrome profile and try again.', true)
    }
    await send('/cookies', {cookies})
    done('Done. The cookies were sent to Prem-dl. You can close this tab.')
  } catch (e) {
    done(`Couldn't reach Prem-dl (${e.message}). Start it again from the app.`, true)
  } finally {
    if (granted) await chrome.permissions.remove({origins: ORIGINS}).catch(() => {})
  }
})

deny.addEventListener('click', async () => {
  await send('/cookies', {cancelled: true}).catch(() => {})
  done('Nothing was shared.')
})
