// Named presets: saved option sets, and the site rules that pick one for a link.
import type {DownloadOptions, NamedPreset} from './types'

/** Choices tied to the video they were made on; presets never carry them. */
const PER_VIDEO = {sectionStart: '', sectionEnd: '', ignoreArchive: false, liveFromStart: false, audioLang: ''} satisfies Partial<DownloadOptions>

/** Options as a preset keeps them (as the Go side does): a copy, with nothing tied to one video. */
export function forPreset(o: DownloadOptions): DownloadOptions {
  const out = {...structuredClone(o), ...PER_VIDEO}
  // A format picked from one video's list means nothing for the next.
  return out.preset === 'custom' ? {...out, preset: 'best', formatId: ''} : out
}

/** A preset's options as a fresh form. */
export function presetOptions(p: NamedPreset): DownloadOptions {
  return forPreset(p.options)
}

/** Another preset's options on the form, keeping what was chosen for this video (a time range...). */
export function switchTo(next: DownloadOptions, current: DownloadOptions | null): DownloadOptions {
  if (!current) return next
  const keep = Object.fromEntries(Object.keys(PER_VIDEO).map((k) => [k, current[k as keyof typeof PER_VIDEO]]))
  return {...next, ...keep}
}

/** Whether two forms differ only in choices tied to one video. */
export function sameOptions(a: DownloadOptions, b: DownloadOptions): boolean {
  return JSON.stringify({...a, ...PER_VIDEO}) === JSON.stringify({...b, ...PER_VIDEO})
}

/**
 * The preset a link's site picks, and the site that matched. A site also covers its subdomains,
 * and the longest match wins: music.youtube.com beats youtube.com.
 */
export function presetFor(link: string, presets: NamedPreset[]): {preset: NamedPreset; site: string} | null {
  let host: string
  try {
    host = new URL(link.trim()).hostname.toLowerCase().replace(/\.$/, '')
  } catch {
    return null
  }
  let best: {preset: NamedPreset; site: string} | null = null
  for (const preset of presets) {
    for (const site of preset.sites) {
      if ((host === site || host.endsWith(`.${site}`)) && site.length > (best?.site.length ?? 0)) best = {preset, site}
    }
  }
  return best
}

/** Sites as typed in a text box: "music.youtube.com, example.org" -> ['music.youtube.com', 'example.org']. */
export function parseSites(text: string): string[] {
  return text
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}
