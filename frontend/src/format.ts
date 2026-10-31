const UNITS = ['B', 'KB', 'MB', 'GB', 'TB']

export function bytes(n: number, digits = 1): string {
  if (!isFinite(n) || n <= 0) return '0 B'
  let i = Math.min(UNITS.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  // 1023.6 KB would print as "1024 KB"; show it as the next unit instead.
  if (i < UNITS.length - 1 && n / Math.pow(1024, i) >= 1023.5) i++
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
  // Round once, up front: rounding the seconds part alone gave "1m 60s" for 119.6.
  const s = Math.max(1, Math.round(sec))
  if (s < 60) return `${s}s left`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s left`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m left`
}

/** "20261021" → "21 Oct 2026" */
export function uploadDate(d: string): string {
  if (!/^\d{8}$/.test(d)) return ''
  const date = new Date(Number(d.slice(0, 4)), Number(d.slice(4, 6)) - 1, Number(d.slice(6, 8)))
  return date.toLocaleDateString(undefined, {day: 'numeric', month: 'short', year: 'numeric'})
}

// Made once: building a formatter costs ~20 µs, and a big channel shows thousands of counts.
const compact = new Intl.NumberFormat(undefined, {notation: 'compact', maximumFractionDigits: 1})

/** Whether downloads may start now under these hours, and if not the "HH:MM" they open. Mirrors settings.ActiveHours.Open. */
export function hoursOpen(h: {on: boolean; from: string; to: string} | undefined, now = new Date()): {open: boolean; opens: string} {
  const mins = (s: string) => {
    const m = /^(\d{1,2}):(\d{2})$/.exec(s.trim())
    return m ? Number(m[1]) * 60 + Number(m[2]) : NaN
  }
  if (!h?.on) return {open: true, opens: ''}
  const from = mins(h.from)
  const to = mins(h.to)
  if (Number.isNaN(from) || Number.isNaN(to) || from === to) return {open: true, opens: ''}
  const cur = now.getHours() * 60 + now.getMinutes()
  const open = from < to ? cur >= from && cur < to : cur >= from || cur < to
  return {open, opens: open ? '' : h.from.trim()}
}

export function compactCount(n: number): string {
  if (!n) return ''
  return compact.format(n)
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

/** Milliseconds since the epoch of a time from Go, or null for Go's zero time (year 1). */
export function goTime(iso: string): number | null {
  const t = Date.parse(iso)
  return t > Date.UTC(2000, 0) ? t : null
}

/** A rough length of time: "a minute", "5 minutes", "3 hours", "2 days". */
export function span(ms: number): string {
  const m = Math.round(Math.abs(ms) / 60000)
  if (m <= 1) return 'a minute'
  if (m < 60) return `${m} minutes`
  const h = Math.round(m / 60)
  if (h < 36) return plural(h, 'hour')
  return plural(Math.round(h / 24), 'day')
}

/** "Tue 29 Oct, 20:45 (in 3 days)" for a scheduled start in Unix seconds, or '' when unknown (0). */
export function startsAt(unix: number, now = Date.now()): string {
  if (!unix) return ''
  const ms = unix * 1000
  const when = new Date(ms).toLocaleString(undefined, {weekday: 'short', day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit'})
  return ms > now ? `${when} (in ${span(ms - now)})` : `${when} (${span(now - ms)} ago)`
}

/** "3 days ago" for a Unix time in seconds, or '' when unknown (0). */
export function ago(unix: number, now = Date.now()): string {
  if (!unix) return ''
  const ms = now - unix * 1000
  const days = ms / 86400_000
  if (days >= 365) return `${plural(Math.floor(days / 365), 'year')} ago`
  if (days >= 30) return `${plural(Math.floor(days / 30), 'month')} ago`
  if (days >= 7) return `${plural(Math.floor(days / 7), 'week')} ago`
  return `${span(ms)} ago`
}
