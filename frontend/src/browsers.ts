import type {Browser, Cookies} from './types'

/** The browser choice for a profile folder of a Chromium browser that isn't listed. */
export const OTHER = 'other'

/**
 * Which listed browser the cookie settings point at (Linux). Browsers yt-dlp doesn't know by name
 * are their family plus a profile folder under their root; any other folder is OTHER.
 */
export function browserChoice(browsers: Browser[], c: Cookies, otherPicked: boolean): string {
  const folder = c.profile.startsWith('/')
  for (const b of browsers) {
    if (b.family !== c.browser) continue
    if (b.root ? folder && (c.profile === b.root || c.profile.startsWith(b.root + '/')) : !folder && !otherPicked) return b.id
  }
  return folder || otherPicked ? OTHER : c.browser
}

/** The browser the helper runs in, as the backend picks it: the chosen one, the default, the first. */
export function helperOf(browsers: Browser[], chosen: string): Browser | undefined {
  const can = browsers.filter((b) => b.helper)
  return can.find((b) => b.id === chosen) ?? can.find((b) => b.default) ?? can[0]
}
