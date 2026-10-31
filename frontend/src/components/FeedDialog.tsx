import {useEffect, useState} from 'react'
import {api, errMessage} from '../api'
import {hostOf} from '../format'
import {Icon} from '../icons'

/** "youtube.com/@name": what to call a pasted link before the first check names it. */
function shortLink(url: string): string {
  try {
    const u = new URL(url)
    return hostOf(url) + (u.pathname === '/' ? '' : u.pathname.replace(/\/$/, ''))
  } catch {
    return url
  }
}
import {useApp} from '../store'
import type {ChannelTab, DownloadOptions, Feed, FeedFilter, FeedTarget} from '../types'
import {OptionsPanel, optionsProblem} from './OptionsPanel'
import {Button, Chip, Field, Modal, Spinner} from './ui'

const NO_FILTER: FeedFilter = {minMinutes: 0, maxMinutes: 0, include: [], exclude: []}

/** "Episode, Part 2" -> ["Episode", "Part 2"]. */
const words = (text: string) =>
  text
    .split(',')
    .map((w) => w.trim())
    .filter(Boolean)

/** A filter as one line for the Following page: "10-90 min · title has Episode · not Rerun", or ''. */
export function filterSummary(f: FeedFilter | undefined): string {
  if (!f) return ''
  const len =
    f.minMinutes && f.maxMinutes ? `${f.minMinutes}–${f.maxMinutes} min` : f.minMinutes ? `${f.minMinutes} min or longer` : f.maxMinutes ? `up to ${f.maxMinutes} min` : ''
  return [len, f.include.length ? `title has ${f.include.join(' or ')}` : '', f.exclude.length ? `not ${f.exclude.join(', ')}` : ''].filter(Boolean).join(' · ')
}

/** The problem with a filter as typed, or ''. */
function filterProblem(min: string, max: string): string {
  const lo = Number(min || 0)
  const hi = Number(max || 0)
  if (!Number.isInteger(lo) || !Number.isInteger(hi) || lo < 0 || hi < 0) return 'Lengths are whole minutes.'
  if (hi > 0 && hi < lo) return 'The longest length is shorter than the shortest.'
  return ''
}

export const TAB_LABELS: Record<ChannelTab, string> = {videos: 'Videos', shorts: 'Shorts', streams: 'Live'}
const TABS: ChannelTab[] = ['videos', 'shorts', 'streams']

/** "every 6 hours", or how checks happen when they don't run by themselves. */
export function checkEvery(hours: number): string {
  if (hours <= 0) return 'when you click Check now'
  return hours === 1 ? 'every hour' : `every ${hours} hours`
}

/**
 * Follow a channel or playlist (link), or change a followed one (feed): which tabs of a YouTube
 * channel are checked, and the options new uploads download with.
 */
export function FeedDialog({
  link,
  feed,
  options,
  onClose,
  onSaved,
}: {
  link?: {url: string; title: string; uploader: string}
  feed?: Feed
  options: DownloadOptions
  onClose: () => void
  /** After following or saving, before the dialog closes. */
  onSaved?: () => void
}) {
  const {state, toast, go} = useApp()
  const [target, setTarget] = useState<FeedTarget | null>(feed ? {url: feed.url, channel: feed.tabs.length > 0, tabs: feed.tabs, followed: feed.id} : null)
  const [problem, setProblem] = useState('')
  const [tabs, setTabs] = useState<ChannelTab[]>(feed?.tabs ?? [])
  // A format picked from one video's list can't apply to uploads that don't exist yet.
  const [opts, setOpts] = useState<DownloadOptions>(() => (options.preset === 'custom' ? {...options, preset: 'best', formatId: ''} : options))
  const [saving, setSaving] = useState(false)
  const start = feed?.filter ?? NO_FILTER
  const [minLen, setMinLen] = useState(start.minMinutes ? String(start.minMinutes) : '')
  const [maxLen, setMaxLen] = useState(start.maxMinutes ? String(start.maxMinutes) : '')
  const [include, setInclude] = useState(start.include.join(', '))
  const [exclude, setExclude] = useState(start.exclude.join(', '))
  const filter: FeedFilter = {minMinutes: Number(minLen || 0), maxMinutes: Number(maxLen || 0), include: words(include), exclude: words(exclude)}
  const filtering = !!(filter.minMinutes || filter.maxMinutes || filter.include.length || filter.exclude.length)
  const [showFilter, setShowFilter] = useState(filtering)

  const url = link?.url
  useEffect(() => {
    if (!url) return
    let alive = true
    api
      .inspectFeed(url)
      .then((res) => {
        if (!alive) return
        if (res.error) return setProblem(res.error)
        setTarget(res.target)
        setTabs(res.target.tabs)
      })
      .catch((e) => alive && setProblem(errMessage(e)))
    return () => {
      alive = false
    }
  }, [url])

  const name = feed?.title || (target?.channel ? link?.uploader : '') || link?.title || (target ? shortLink(target.url) : '')
  const already = !feed && !!target?.followed
  const hours = state.settings?.followEvery ?? 6
  const optsProblem = optionsProblem(opts)
  const tabsProblem = target?.channel && tabs.length === 0 ? 'Choose at least one tab to check.' : ''
  const lenProblem = filterProblem(minLen, maxLen)

  const toggleTab = (t: ChannelTab) => setTabs((cur) => (cur.includes(t) ? cur.filter((x) => x !== t) : TABS.filter((x) => x === t || cur.includes(x))))

  const save = async () => {
    if (!target || optsProblem || tabsProblem || lenProblem) return
    setSaving(true)
    try {
      const res = feed
        ? await api.editFeed(feed.id, tabs, opts, filter)
        : await api.followFeed({url: link?.url ?? target.url, title: link?.title ?? '', uploader: link?.uploader ?? '', tabs, options: opts, filter})
      if (res.error) return toast('error', res.error)
      if (feed) {
        toast('success', 'Saved')
      } else {
        toast('success', `Following ${res.feed?.title || name}. New uploads will be added to the queue.`, {label: 'View', run: () => go('following')})
      }
      onSaved?.()
      onClose()
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      dismissable={false}
      title={feed ? `Options for ${name}` : already ? 'Already following' : `Follow ${name || 'this'}`}
      onClose={onClose}
      footer={
        already ? (
          <>
            <Button onClick={onClose}>Close</Button>
            <Button
              variant="primary"
              onClick={() => {
                onClose()
                go('following')
              }}>
              Open Following
            </Button>
          </>
        ) : (
          <>
            <span className="truncate" style={{flex: 1, minWidth: 0, color: 'var(--danger)', fontSize: 12.5}}>
              {tabsProblem || lenProblem || optsProblem}
            </span>
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" icon={feed ? 'save' : 'bell'} loading={saving} disabled={!target || !!tabsProblem || !!lenProblem || !!optsProblem} onClick={() => void save()}>
              {feed ? 'Save' : 'Follow'}
            </Button>
          </>
        )
      }>
      {problem ? (
        <div style={{color: 'var(--danger)'}}>{problem}</div>
      ) : !target ? (
        <div className="row" style={{color: 'var(--muted)'}}>
          <Spinner /> Checking the link…
        </div>
      ) : already ? (
        <p>You already follow {name || 'this'}. Its options can be changed on the Following page.</p>
      ) : (
        <div className="stack">
          {!feed && (
            <p className="muted" style={{margin: 0}}>
              The videos there now aren't downloaded. From now on Prem-dl checks {checkEvery(hours)}, even from the tray, and adds new uploads to the queue with the options
              below.
            </p>
          )}
          {target.channel && (
            <div>
              <div className="section-title">Check these tabs</div>
              <div className="chips">
                {TABS.map((t) => (
                  <Chip key={t} pressed={tabs.includes(t)} onClick={() => toggleTab(t)}>
                    {TAB_LABELS[t]}
                  </Chip>
                ))}
              </div>
              {feed && <div className="hint">Videos already on a tab you add aren't downloaded.</div>}
            </div>
          )}
          <div>
            <button type="button" className="collapse-btn" aria-expanded={showFilter} onClick={() => setShowFilter((v) => !v)}>
              <span className="chev">
                <Icon name="chevronRight" size={16} />
              </span>
              <span className="opt-head">Only download some uploads</span>
              {!showFilter && filtering && <span className="muted truncate">{filterSummary(filter)}</span>}
            </button>
            {showFilter && (
              <div className="stack" style={{marginTop: 8}}>
                <div className="grid2">
                  <Field label="At least (minutes)" hint="Skips shorter videos. Empty for no limit.">
                    <input type="number" min={0} step={1} inputMode="numeric" value={minLen} onChange={(e) => setMinLen(e.target.value)} placeholder="No minimum" />
                  </Field>
                  <Field label="At most (minutes)" hint="Skips longer ones, such as stream reruns.">
                    <input type="number" min={0} step={1} inputMode="numeric" value={maxLen} onChange={(e) => setMaxLen(e.target.value)} placeholder="No maximum" />
                  </Field>
                  <Field label="Title contains one of" hint="Separate words with commas. Case doesn't matter.">
                    <input type="text" value={include} onChange={(e) => setInclude(e.target.value)} placeholder="e.g. Episode, Part" />
                  </Field>
                  <Field label="Title doesn't contain" hint="Uploads with any of these are skipped.">
                    <input type="text" value={exclude} onChange={(e) => setExclude(e.target.value)} placeholder="e.g. #shorts, Rerun" />
                  </Field>
                </div>
                <div className="hint">Skipped uploads aren't downloaded later if you change this. Videos the site gives no length for pass the length limits.</div>
              </div>
            )}
          </div>
          <OptionsPanel value={opts} onChange={setOpts} playlist feed mode="download" archiveOn={state.settings?.useArchive} />
        </div>
      )}
    </Modal>
  )
}
