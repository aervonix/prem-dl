// @vitest-environment jsdom
import {act} from 'react'
import {createRoot} from 'react-dom/client'
import type {Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import type {PlaylistEntry} from '../types'

vi.mock('../store', () => ({useJobs: () => []}))
const {VideoGrid} = await import('./VideoGrid')

;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true

function entry(index: number): PlaylistEntry {
  return {
    index, id: `v${index}`, title: `Video ${index}`, url: `https://www.youtube.com/watch?v=v${index}`, uploader: '', thumbnail: '',
    duration: 0, group: '', unavailable: false, isPlaylist: false, liveStatus: '', extractor: 'youtube', viewCount: 0, timestamp: 0, releaseTimestamp: 0,
  }
}
const range = (from: number, to: number) => Array.from({length: to - from + 1}, (_, i) => entry(from + i))

describe('VideoGrid reveal', () => {
  let host: HTMLDivElement
  let root: Root
  beforeEach(() => {
    host = document.createElement('div')
    root = createRoot(host)
  })
  afterEach(() => act(() => root.unmount()))

  const show = (entries: PlaylistEntry[], reveal = true) =>
    act(() => root.render(<VideoGrid entries={entries} selected={new Set()} setSelected={() => {}} reveal={reveal} />))
  // Each card's place in its batch, or null when it isn't faded in.
  const order = () =>
    [...host.querySelectorAll<HTMLElement>('.vcard')].map((c) => (c.classList.contains('reveal') ? Number(c.style.getPropertyValue('--reveal')) : null))

  it('fades a batch in from its first card', () => {
    show(range(1, 4))
    expect(order()).toEqual([0, 1, 2, 3])
  })

  it('numbers only the new cards when more are added, leaving the shown ones alone', () => {
    show(range(1, 4))
    show(range(1, 7))
    expect(order()).toEqual([0, 1, 2, 3, 0, 1, 2])
  })

  it('starts over for another list', () => {
    show(range(1, 6))
    show(range(1, 3))
    expect(order()).toEqual([0, 1, 2])
  })

  it('caps the delay for cards far down a batch', () => {
    show(range(1, 20))
    expect(order().slice(14)).toEqual([14, 15, 15, 15, 15, 15])
  })

  it('shows cards at once without reveal', () => {
    show(range(1, 3), false)
    expect(order()).toEqual([null, null, null])
  })
})
