// @vitest-environment jsdom
import {act} from 'react'
import {createRoot} from 'react-dom/client'
import {expect, it, vi} from 'vitest'
vi.mock('../store', () => ({useJobs: () => []}))
const {useSteps} = await import('./VideoGrid')

;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let observer: {cb: (e: {isIntersecting: boolean}[]) => void} | null = null
globalThis.IntersectionObserver = class {
  constructor(public cb: (e: {isIntersecting: boolean}[]) => void) {
    observer = this
  }
  observe() {}
  disconnect() {}
} as unknown as typeof IntersectionObserver

type J = {id: number; percent: number}
let shownLen = 0
function Probe({jobs}: {jobs: J[]}) {
  const {shown, sentinel} = useSteps(jobs, (j) => j.id)
  shownLen = shown.length
  return <div>{sentinel}</div>
}

it('keeps the rows shown when the first one is replaced by an update', () => {
  const jobs: J[] = Array.from({length: 400}, (_, i) => ({id: i, percent: 0}))
  const root = createRoot(document.createElement('div'))
  act(() => root.render(<Probe jobs={jobs} />))
  expect(shownLen).toBe(240)
  act(() => observer!.cb([{isIntersecting: true}])) // scrolled near the end
  expect(shownLen).toBe(400)
  // Job 0 reports progress: the jobs reducer replaces its object.
  act(() => root.render(<Probe jobs={[{...jobs[0], percent: 5}, ...jobs.slice(1)]} />))
  expect(shownLen).toBe(400)
  // Another list (a filter) starts over.
  act(() => root.render(<Probe jobs={jobs.slice(100)} />))
  expect(shownLen).toBe(240)
  act(() => root.unmount())
})
