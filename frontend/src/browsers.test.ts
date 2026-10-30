import {describe, expect, it} from 'vitest'
import {OTHER, browserChoice, helperOf} from './browsers'
import type {Browser, Cookies} from './types'

const b = (id: string, extra: Partial<Browser> = {}): Browser => ({id, label: id, family: id, root: '', profiles: [], installed: true, helper: false, default: false, ...extra})
const helium = b('helium', {family: 'chromium', root: '/home/u/.config/net.imput.helium', helper: true, default: true})
const browsers = [b('firefox'), b('chrome', {helper: true}), b('chromium', {helper: true}), helium]
const cookies = (browser: string, profile = ''): Cookies => ({mode: 'browser', browser, profile, file: ''})

describe('browserChoice', () => {
  it('finds browsers yt-dlp knows by name', () => {
    expect(browserChoice(browsers, cookies('firefox', 'work'), false)).toBe('firefox')
    expect(browserChoice(browsers, cookies('chromium'), false)).toBe('chromium')
  })

  it('tells a profile folder of a listed browser from one of another', () => {
    expect(browserChoice(browsers, cookies('chromium', '/home/u/.config/net.imput.helium/Default'), false)).toBe('helium')
    expect(browserChoice(browsers, cookies('chromium', '/home/u/.config/net.imput.helium'), false)).toBe('helium')
    expect(browserChoice(browsers, cookies('chromium', '/home/u/.config/net.imput.heliumx/Default'), false)).toBe(OTHER)
    expect(browserChoice(browsers, cookies('chromium', '/opt/x/Default'), false)).toBe(OTHER)
  })

  it('keeps Other picked while its folder is still empty', () => {
    expect(browserChoice(browsers, cookies('chromium'), true)).toBe(OTHER)
  })

  it('keeps a browser that is no longer listed', () => {
    expect(browserChoice(browsers, cookies('vivaldi'), false)).toBe('vivaldi')
  })
})

describe('helperOf', () => {
  it('picks the chosen browser, else the default, else the first', () => {
    expect(helperOf(browsers, 'chrome')?.id).toBe('chrome')
    expect(helperOf(browsers, '')?.id).toBe('helium')
    expect(helperOf(browsers, 'firefox')?.id).toBe('helium') // Firefox can't run it
    expect(helperOf([b('chrome', {helper: true}), b('brave', {helper: true})], 'gone')?.id).toBe('chrome')
    expect(helperOf([b('firefox')], '')).toBeUndefined()
  })
})
