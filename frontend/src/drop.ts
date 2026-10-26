// Reading links out of things dropped on the window: a link dragged from a browser, text, or
// files (.txt lists, .url shortcuts, a cookies.txt).

const LINK = /https?:\/\/[^\s"'<>`]+/gi
const COOKIES = /^#\s*(Netscape\s+)?HTTP Cookie File/i
const TEXT_FILE = /\.(txt|url|csv|tsv|md|html?|json|list)$/i
const MAX_FILE = 4 << 20

/** Most links one drop adds: the queue holds 500 jobs. */
export const MAX_DROPPED = 500

/** The distinct http(s) links in text, in order. */
export function linksIn(text: string): string[] {
  const out = new Set<string>()
  for (const m of text.matchAll(LINK)) {
    const link = trimLink(m[0])
    if (link.length > 'https://'.length) out.add(link)
  }
  return [...out]
}

const count = (s: string, ch: string) => s.split(ch).length - 1

/** Drops punctuation that ends the sentence rather than the link: "see https://x.com/a)." */
function trimLink(link: string): string {
  for (;;) {
    const t = link.replace(/[.,;:!?]+$/, '')
    const last = t.slice(-1)
    const open = last === ')' ? '(' : last === ']' ? '[' : ''
    const next = open && count(t, open) < count(t, last) ? t.slice(0, -1) : t
    if (next === link) return link
    link = next
  }
}

export interface Dropped {
  links: string[]
  /** The text of a dropped cookies.txt file, if there was one. */
  cookies: string
  /** Names of dropped files that aren't text. */
  skipped: string[]
}

/**
 * Reads a drop. Call it from the drop event itself: the dragged text is only readable during the
 * event (files can be read later, which is why this is async).
 */
export async function readDrop(dt: DataTransfer): Promise<Dropped> {
  const text = droppedText(dt)
  const files = Array.from(dt.files)
  const res: Dropped = {links: linksIn(text), cookies: '', skipped: []}
  for (const f of files) {
    if (f.size > MAX_FILE || !(TEXT_FILE.test(f.name) || f.type.startsWith('text/'))) {
      res.skipped.push(f.name)
      continue
    }
    let body: string
    try {
      body = await f.text()
    } catch {
      res.skipped.push(f.name)
      continue
    }
    if (COOKIES.test(body.trimStart())) {
      res.cookies ||= body
      continue
    }
    for (const l of linksIn(body)) if (!res.links.includes(l)) res.links.push(l)
  }
  return res
}

/** The dragged text: a browser puts a dragged link in text/uri-list and text/plain. */
export function droppedText(dt: DataTransfer): string {
  return dt.getData('text/uri-list') || dt.getData('text/plain')
}
