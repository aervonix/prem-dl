// Sends the page, or a link on it, to Prem-dl: from the toolbar button and the right-click menu.
// The menu can also download at once, with the default options or one of the user's presets, and
// follow a YouTube channel or playlist. While Prem-dl runs it listens on one of these loopback
// ports (internal/linkbridge in the app) and only accepts this extension. When it isn't running,
// launch.html starts it through the prem-dl: link type.
const PORTS = [38719, 41853, 46379]
const WEB = ['http://*/*', 'https://*/*']
// What can be followed: YouTube channels and playlists.
const FOLLOWABLE = [
  'https://www.youtube.com/@*',
  'https://www.youtube.com/channel/*',
  'https://www.youtube.com/c/*',
  'https://www.youtube.com/user/*',
  'https://www.youtube.com/playlist?*',
  'https://m.youtube.com/@*',
  'https://m.youtube.com/playlist?*',
]

// The presets the menu was last built with, as JSON; null until it has been built in this run of
// the service worker. Every answer from Prem-dl names its presets, and the menu follows them.
let built = null

chrome.runtime.onInstalled.addListener(() => void hello())
chrome.runtime.onStartup.addListener(() => void hello())

// hello asks Prem-dl for its presets, to build the menu; without an answer it has no presets.
async function hello() {
  const found = await ask('/hello', {})
  buildMenu(found?.body?.presets ?? [])
}

function buildMenu(presets) {
  const key = JSON.stringify(presets)
  if (key === built) return
  built = key
  chrome.contextMenus.removeAll(() => {
    // Chrome gathers an extension's items under one "Prem-dl helper" entry.
    const add = (id, title, link) =>
      chrome.contextMenus.create(
        link
          ? {id: `link|${id}`, title, contexts: ['link'], targetUrlPatterns: WEB}
          : {id: `page|${id}`, title, contexts: ['page', 'frame', 'video', 'audio'], documentUrlPatterns: WEB},
      )
    for (const link of [true, false]) {
      add('open', link ? 'Open link in Prem-dl' : 'Open in Prem-dl', link)
      add('download|', link ? 'Download link now' : 'Download now', link)
      for (const name of presets) add(`download|${name}`, `${link ? 'Download link' : 'Download'} as ${name}`, link)
    }
    chrome.contextMenus.create({id: 'link|follow', title: 'Follow this channel or playlist', contexts: ['link'], targetUrlPatterns: FOLLOWABLE})
    chrome.contextMenus.create({id: 'page|follow', title: 'Follow this channel or playlist', contexts: ['page'], documentUrlPatterns: FOLLOWABLE})
  })
}

chrome.contextMenus.onClicked.addListener((info, tab) => {
  const [where, action, ...name] = String(info.menuItemId).split('|')
  const preset = name.join('|') // a preset's name may hold a |
  // In an embedded player (a frame) the video is the frame's page, not the one around it.
  const url = where === 'link' ? info.linkUrl : info.frameUrl || info.pageUrl
  void send({url, action: action === 'open' ? '' : action, preset}, tab)
})

chrome.action.onClicked.addListener((tab) => void send({url: tab.url, action: '', preset: ''}, tab))

// ask posts to Prem-dl's first port that answers as Prem-dl, or returns null.
async function ask(path, body) {
  for (const port of PORTS) {
    let res, reply
    try {
      res = await fetch(`http://127.0.0.1:${port}${path}`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(2000),
      })
      reply = await res.json()
    } catch {
      continue // nothing there, or something that isn't Prem-dl
    }
    if (reply?.app === 'prem-dl') return {ok: res.ok, body: reply}
  }
  return null
}

const DONE = {'': 'Sent to Prem-dl', download: 'Added to Prem-dl’s queue', follow: 'Opened in Prem-dl to follow'}

async function send(req, tab) {
  if (!/^https?:\/\//i.test(req.url ?? '')) return flag(tab, '!', 'Prem-dl can only download http and https pages.')
  const found = await ask('/open', req)
  if (found) {
    if (Array.isArray(found.body.presets)) buildMenu(found.body.presets)
    return found.ok ? flag(tab, 'OK', DONE[req.action] ?? DONE['']) : flag(tab, '!', found.body.error || "Prem-dl couldn't take that link.")
  }
  // Prem-dl isn't running: start it with the link.
  const hash = new URLSearchParams({url: req.url, action: req.action, preset: req.preset}).toString()
  await chrome.tabs.create({
    url: chrome.runtime.getURL('launch.html') + '#' + hash,
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

// A service worker started for an event (not install or browser start) builds the menu too, so
// presets changed while it slept still show.
void hello()
