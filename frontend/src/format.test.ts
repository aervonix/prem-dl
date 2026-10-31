import {describe, expect, it} from 'vitest'
import {ago, basename, bytes, compactCount, dirname, duration, eta, goTime, hostOf, parseTime, plural, span, speed, startsAt, uploadDate, hoursOpen} from './format'

describe('bytes', () => {
  it('prints whole bytes and one decimal above', () => {
    expect(bytes(0)).toBe('0 B')
    expect(bytes(512)).toBe('512 B')
    expect(bytes(1536)).toBe('1.5 KB')
    expect(bytes(5 * 1024 ** 3)).toBe('5.0 GB')
    expect(bytes(1536, 2)).toBe('1.50 KB')
  })
  it('drops decimals from three-digit values', () => {
    expect(bytes(150 * 1024 ** 2)).toBe('150 MB')
  })
  it('rolls over instead of printing 1024 of a unit', () => {
    expect(bytes(1023.6 * 1024)).toBe('1.0 MB')
    expect(bytes(1023.4 * 1024)).toBe('1023 KB')
  })
  it('stays in TB for huge values', () => {
    expect(bytes(2048 * 1024 ** 4)).toBe('2048 TB')
  })
  it('treats bad input as nothing', () => {
    expect(bytes(-5)).toBe('0 B')
    expect(bytes(NaN)).toBe('0 B')
    expect(bytes(Infinity)).toBe('0 B')
  })
})

describe('speed', () => {
  it('is empty when not moving', () => {
    expect(speed(0)).toBe('')
    expect(speed(-1)).toBe('')
    expect(speed(2 * 1024 ** 2)).toBe('2.0 MB/s')
  })
})

describe('duration', () => {
  it('formats minutes and hours', () => {
    expect(duration(75)).toBe('1:15')
    expect(duration(5)).toBe('0:05')
    expect(duration(3725)).toBe('1:02:05')
    expect(duration(59.6)).toBe('1:00')
  })
  it('is empty when unknown', () => {
    expect(duration(0)).toBe('')
    expect(duration(-3)).toBe('')
    expect(duration(NaN)).toBe('')
  })
})

describe('eta', () => {
  it('formats each range', () => {
    expect(eta(0)).toBe('1s left')
    expect(eta(42)).toBe('42s left')
    expect(eta(125)).toBe('2m 5s left')
    expect(eta(3 * 3600 + 25 * 60)).toBe('3h 25m left')
  })
  it('rounds before splitting', () => {
    // Rounding only the seconds part gave "1m 60s".
    expect(eta(119.6)).toBe('2m 0s left')
    expect(eta(59.6)).toBe('1m 0s left')
  })
  it('is empty when unknown', () => {
    expect(eta(-1)).toBe('')
    expect(eta(Infinity)).toBe('')
  })
})

describe('uploadDate', () => {
  it('reads YYYYMMDD as a local date', () => {
    const want = new Date(2026, 9, 21).toLocaleDateString(undefined, {day: 'numeric', month: 'short', year: 'numeric'})
    expect(uploadDate('20261021')).toBe(want)
    expect(uploadDate('20261021')).toContain('2026')
  })
  it('ignores anything else', () => {
    expect(uploadDate('')).toBe('')
    expect(uploadDate('2026-10-21')).toBe('')
    expect(uploadDate('202610211')).toBe('')
  })
})

describe('compactCount', () => {
  it('is empty for zero', () => {
    expect(compactCount(0)).toBe('')
  })
  it('shortens big numbers', () => {
    expect(compactCount(1_234_567).length).toBeLessThan('1234567'.length)
    expect(compactCount(12)).toBe('12')
  })
})

describe('hostOf', () => {
  it('drops www. and keeps other subdomains', () => {
    expect(hostOf('https://www.youtube.com/watch?v=x')).toBe('youtube.com')
    expect(hostOf('https://music.youtube.com/watch?v=x')).toBe('music.youtube.com')
  })
  it('returns input that is not a URL', () => {
    expect(hostOf('not a url')).toBe('not a url')
  })
})

describe('parseTime', () => {
  it('reads seconds, m:ss and h:mm:ss', () => {
    expect(parseTime('90')).toBe(90)
    expect(parseTime('1:30')).toBe(90)
    expect(parseTime('1:02:03')).toBe(3723)
    expect(parseTime(' 1:30.5 ')).toBe(90.5)
    expect(parseTime('0')).toBe(0)
  })
  it('rejects anything else', () => {
    expect(parseTime('')).toBeNull()
    expect(parseTime('1:2:3:4')).toBeNull()
    expect(parseTime('1m30s')).toBeNull()
    expect(parseTime('-5')).toBeNull()
    expect(parseTime(':30')).toBeNull()
  })
})

describe('plural', () => {
  it('picks the form by count', () => {
    expect(plural(1, 'video')).toBe('1 video')
    expect(plural(0, 'video')).toBe('0 videos')
    expect(plural(2, 'entry', 'entries')).toBe('2 entries')
  })
})

describe('basename / dirname', () => {
  it('handles both separators', () => {
    expect(basename('C:\\Users\\me\\a.mp4')).toBe('a.mp4')
    expect(basename('/tmp/x/b.webm')).toBe('b.webm')
    expect(basename('c.mkv')).toBe('c.mkv')
    expect(dirname('C:\\Users\\me\\a.mp4')).toBe('C:\\Users\\me')
    expect(dirname('C:\\dir/mixed\\a.mp4')).toBe('C:\\dir/mixed')
    expect(dirname('a.mp4')).toBe('a.mp4')
  })
})

describe('goTime', () => {
  it("treats Go's zero time as unset", () => {
    expect(goTime('0001-01-01T00:00:00Z')).toBeNull()
    expect(goTime('')).toBeNull()
    expect(goTime('2026-10-26T12:00:00Z')).toBe(Date.UTC(2026, 9, 26, 12))
  })
})

describe('span', () => {
  const min = 60_000
  it('rounds to a rough length', () => {
    expect(span(0)).toBe('a minute')
    expect(span(90 * 1000)).toBe('2 minutes')
    expect(span(59 * min)).toBe('59 minutes')
    expect(span(60 * min)).toBe('1 hour')
    expect(span(35 * 60 * min)).toBe('35 hours')
    expect(span(36 * 60 * min)).toBe('2 days')
  })
  it('ignores the sign', () => {
    expect(span(-5 * min)).toBe('5 minutes')
  })
})

describe('ago', () => {
  const now = Date.UTC(2026, 9, 26)
  const daysBack = (d: number) => (now - d * 86400_000) / 1000
  it('is empty when unknown', () => {
    expect(ago(0, now)).toBe('')
  })
  it('steps through hours, days, weeks, months and years', () => {
    expect(ago((now - 3 * 3600_000) / 1000, now)).toBe('3 hours ago')
    expect(ago(daysBack(3), now)).toBe('3 days ago')
    expect(ago(daysBack(7), now)).toBe('1 week ago')
    expect(ago(daysBack(29), now)).toBe('4 weeks ago')
    expect(ago(daysBack(30), now)).toBe('1 month ago')
    expect(ago(daysBack(400), now)).toBe('1 year ago')
    expect(ago(daysBack(800), now)).toBe('2 years ago')
  })
})

describe('startsAt', () => {
  const now = Date.UTC(2026, 9, 26, 12)
  it('is empty when unknown', () => {
    expect(startsAt(0, now)).toBe('')
  })
  it('says how far off a start is, either way', () => {
    expect(startsAt(now / 1000 + 3 * 86400, now)).toMatch(/ \(in 3 days\)$/)
    expect(startsAt(now / 1000 - 2 * 3600, now)).toMatch(/ \(2 hours ago\)$/)
  })
})

describe('hoursOpen', () => {
  const at = (h: number, m = 0) => new Date(2026, 9, 31, h, m)
  it('mirrors settings.ActiveHours.Open, overnight too', () => {
    const night = {on: true, from: '23:00', to: '07:00'}
    expect(hoursOpen(night, at(2)).open).toBe(true)
    expect(hoursOpen(night, at(12))).toEqual({open: false, opens: '23:00'})
    expect(hoursOpen({on: true, from: '09:00', to: '17:00'}, at(17))).toEqual({open: false, opens: '09:00'})
    expect(hoursOpen({on: false, from: '09:00', to: '17:00'}, at(20)).open).toBe(true)
  })
})
