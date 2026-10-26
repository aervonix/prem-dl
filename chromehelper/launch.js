// Opened by background.js as launch.html#<link> when Prem-dl isn't running. The prem-dl: link type
// starts the app with the link; once the app answers on its port the link has arrived, so the tab
// closes itself. The button is there in case Chrome wants a click before it opens an app.
const PORTS = [38719, 41853, 46379]
const link = decodeURIComponent(location.hash.slice(1))
const what = document.getElementById('what')
const open = document.getElementById('open')
const status = document.getElementById('status')

async function running() {
  for (const port of PORTS) {
    try {
      // An empty request: Prem-dl answers it without opening anything.
      const res = await fetch(`http://127.0.0.1:${port}/open`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: '{}',
        signal: AbortSignal.timeout(1000),
      })
      if ((await res.json())?.app === 'prem-dl') return true
    } catch {}
  }
  return false
}

const launch = () => (location.href = 'prem-dl://open?url=' + encodeURIComponent(link))

if (!/^https?:\/\//i.test(link)) {
  open.disabled = true
  status.textContent = 'There was no link to send.'
  status.className = 'bad'
} else {
  what.textContent = link
  open.addEventListener('click', launch)
  launch()
  const started = Date.now()
  const timer = setInterval(async () => {
    if (await running()) {
      clearInterval(timer)
      open.disabled = true
      status.textContent = 'Sent to Prem-dl. You can close this tab.'
      setTimeout(() => chrome.tabs.getCurrent((t) => t && chrome.tabs.remove(t.id)), 800)
    } else if (Date.now() - started > 60000) {
      clearInterval(timer)
      status.textContent =
        "Prem-dl didn't start. If it's installed, start it once yourself so Windows learns that it opens prem-dl links, then try again."
      status.className = 'bad'
    }
  }, 1000)
}
