// Sends the page, or a link on it, to Prem-dl: from the toolbar button and the right-click menu.
// While Prem-dl runs it listens on one of these loopback ports (internal/linkbridge in the app)
// and only accepts this extension. When it isn't running, launch.html starts it through the
// prem-dl: link type.
const PORTS = [38719, 41853, 46379]
const WEB = ['http://*/*', 'https://*/*']

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({id: 'link', title: 'Download link with Prem-dl', contexts: ['link'], targetUrlPatterns: WEB})
    chrome.contextMenus.create({id: 'page', title: 'Download with Prem-dl', contexts: ['page', 'frame', 'video', 'audio'], documentUrlPatterns: WEB})
  })
})

chrome.contextMenus.onClicked.addListener((info, tab) => {
  // In an embedded player (a frame) the video is the frame's page, not the one around it.
  const url = info.menuItemId === 'link' ? info.linkUrl : info.frameUrl || info.pageUrl
  void send(url, tab)
})

chrome.action.onClicked.addListener((tab) => void send(tab.url, tab))

async function send(url, tab) {
  if (!/^https?:\/\//i.test(url ?? '')) return flag(tab, '!', 'Prem-dl can only download http and https pages.')
  for (const port of PORTS) {
    let res, body
    try {
      res = await fetch(`http://127.0.0.1:${port}/open`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({url}),
        signal: AbortSignal.timeout(2000),
      })
      body = await res.json()
    } catch {
      continue // nothing there, or something that isn't Prem-dl
    }
    if (body?.app !== 'prem-dl') continue
    return res.ok ? flag(tab, 'OK', 'Sent to Prem-dl') : flag(tab, '!', body.error || "Prem-dl couldn't take that link.")
  }
  // Prem-dl isn't running: start it with the link.
  await chrome.tabs.create({
    url: chrome.runtime.getURL('launch.html') + '#' + encodeURIComponent(url),
    index: tab ? tab.index + 1 : undefined,
    openerTabId: tab?.id,
  })
}

// flag shows a short badge on the toolbar button, with the message as its tooltip.
function flag(tab, text, title) {
  const tabId = tab?.id
  if (tabId === undefined) return
  void chrome.action.setBadgeBackgroundColor({tabId, color: text === 'OK' ? '#1e7a45' : '#c62828'})
  void chrome.action.setBadgeText({tabId, text})
  void chrome.action.setTitle({tabId, title})
  setTimeout(() => {
    void chrome.action.setBadgeText({tabId, text: ''}).catch(() => {})
    void chrome.action.setTitle({tabId, title: 'Download with Prem-dl'}).catch(() => {})
  }, 4000)
}
