const UNITS = ['B', 'KB', 'MB', 'GB', 'TB']

export function bytes(n: number, digits = 1): string {
  if (!isFinite(n) || n <= 0) return '0 B'
  const i = Math.min(UNITS.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / Math.pow(1024, i)
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(digits)} ${UNITS[i]}`
}

export function speed(bps: number): string {
  return bps > 0 ? `${bytes(bps)}/s` : ''
}

/** 3725 → "1:02:05", 75 → "1:15" */
export function duration(sec: number): string {
  if (!isFinite(sec) || sec <= 0) return ''
  const s = Math.round(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = s % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(ss)}` : `${m}:${pad(ss)}`
}

/** Remaining time as a short phrase; '' when unknown (negative). */
export function eta(sec: number): string {
  if (sec < 0 || !isFinite(sec)) return ''
  if (sec < 60) return `${Math.max(1, Math.round(sec))}s left`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${Math.round(sec % 60)}s left`
  return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m left`
}

/** "20261021" → "21 Oct 2026" */
export function uploadDate(d: string): string {
  if (!/^\d{8}$/.test(d)) return ''
  const date = new Date(Number(d.slice(0, 4)), Number(d.slice(4, 6)) - 1, Number(d.slice(6, 8)))
  return date.toLocaleDateString(undefined, {day: 'numeric', month: 'short', year: 'numeric'})
}

export function compactCount(n: number): string {
  if (!n) return ''
  return new Intl.NumberFormat(undefined, {notation: 'compact', maximumFractionDigits: 1}).format(n)
}

export function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

/** Parse "1:30" / "90" / "1:02:03" into seconds, or null if it isn't a time. */
export function parseTime(t: string): number | null {
  if (!/^(\d+:){0,2}\d+(\.\d+)?$/.test(t.trim())) return null
  return t
    .trim()
    .split(':')
    .reduce((acc, part) => acc * 60 + parseFloat(part), 0)
}

export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`
}

export function basename(p: string): string {
  return p.split(/[\\/]/).pop() ?? p
}

export function dirname(p: string): string {
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return i > 0 ? p.slice(0, i) : p
}
