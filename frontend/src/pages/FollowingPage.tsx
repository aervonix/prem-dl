import {memo, useEffect, useState} from 'react'
import {api, errMessage} from '../api'
import {ErrorPanel} from '../components/ErrorPanel'
import {FeedDialog, TAB_LABELS, checkEvery, filterSummary} from '../components/FeedDialog'
import {ChannelView} from './ChannelView'
import {Button, Card, Empty, IconButton, Modal, Spinner} from '../components/ui'
import {basename, goTime, hostOf, plural, span} from '../format'
import {Icon} from '../icons'
import {systemWords} from '../platform'
import {useApp} from '../store'
import type {DownloadOptions, Feed} from '../types'

const INTERVALS: {hours: number; label: string}[] = [
  {hours: 1, label: 'every hour'},
  {hours: 3, label: 'every 3 hours'},
  {hours: 6, label: 'every 6 hours'},
  {hours: 12, label: 'every 12 hours'},
  {hours: 24, label: 'once a day'},
  {hours: 0, label: 'only when asked'},
]

/** Options that don't belong to one video, for following something from scratch. */
function feedDefaults(d: DownloadOptions): DownloadOptions {
  return {...d, preset: d.preset === 'custom' ? 'best' : d.preset, formatId: '', sectionStart: '', sectionEnd: '', chapters: [], ignoreArchive: false, liveFromStart: false, audioLang: '', outputDir: ''}
}

/** What new uploads download as: "1080p · MP4", "Audio only · MP3", plus the folder when it isn't the default. */
function optionsSummary(o: DownloadOptions): string {
  const parts: string[] = []
  if (o.preset === 'audio') {
    parts.push('Audio only', o.audioFormat === 'best' ? 'best format' : o.audioFormat.toUpperCase())
  } else {
    parts.push(o.preset === 'best' ? 'Best quality' : o.preset === '2160' ? '4K' : `${o.preset}p`)
    if (o.container) parts.push(o.container.toUpperCase())
  }
  if (o.subtitles.mode !== 'none') parts.push('subtitles')
  if (o.sponsorBlock.length > 0) parts.push('SponsorBlock')
  if (o.outputDir) parts.push(`to ${basename(o.outputDir) || o.outputDir}`)
  return parts.join(' · ')
}

/** The time now, refreshed every interval so "checked 5 minutes ago" moves on. */
function useNow(interval: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), interval)
    return () => window.clearInterval(t)
  }, [interval])
  return now
}

function statusLine(f: Feed, now: number, every: number): string {
  if (f.checking) return f.ready ? 'Checking for new videos…' : 'Noting the videos already there…'
  if (!f.ready) return f.error ? 'Couldn’t read it yet. It will try again within an hour.' : 'Waiting to note the videos already there'
  const last = goTime(f.lastCheck)
  const parts: string[] = []
  if (last) parts.push(now - last < 60_000 ? 'Checked just now' : `Checked ${span(now - last)} ago`)
  if (f.paused) {
    parts.push('paused')
  } else if (every > 0 && last) {
    const next = last + Math.min(every * 3600_000, f.error ? 3600_000 : Infinity)
    parts.push(next > now ? `next in ${span(next - now)}` : 'next check soon')
  }
  return parts.join(' · ')
}

function foundLine(f: Feed, now: number): string {
  if (!f.ready || f.error) return ''
  const found = goTime(f.lastFound)
  if (f.queued === 0) return 'No new videos yet'
  return [`${plural(f.queued, 'video')} added since following`, found ? `the last ${span(now - found)} ago` : ''].filter(Boolean).join(', ')
}

const FeedRow = memo(function FeedRow({
  feed,
  now,
  every,
  onOpen,
  onEdit,
  onUnfollow,
}: {
  feed: Feed
  now: number
  every: number
  onOpen: (f: Feed) => void
  onEdit: (f: Feed) => void
  onUnfollow: (f: Feed) => void
}) {
  const {attempt} = useApp()
  const run = (p: () => Promise<unknown>) => void attempt(p())
  const what = feed.tabs.length > 0 ? `YouTube channel · ${feed.tabs.map((t) => TAB_LABELS[t]).join(', ')}` : hostOf(feed.url)
  const found = foundLine(feed, now)

  return (
    <div className="feed">
      <span className="feed-ic" aria-hidden="true">
        <Icon name={feed.tabs.length > 0 ? 'film' : 'list'} size={18} />
      </span>
      <div style={{minWidth: 0}}>
        <button type="button" className="feed-open" title="Browse its videos" onClick={() => onOpen(feed)} style={{display: 'block', maxWidth: '100%'}}>
          <div className="jtitle truncate">
            {feed.title}
            {feed.paused && (
              <span className="badge" style={{marginLeft: 8}}>
                Paused
              </span>
            )}
          </div>
        </button>
        <div className="jsub truncate">
          {[what, optionsSummary(feed.options), filterSummary(feed.filter)].filter(Boolean).join(' · ')}
        </div>
        <div className="feed-status">
          {feed.checking && <Spinner />}
          <span>{statusLine(feed, now, every)}</span>
          {found && <span className="muted">{found}</span>}
        </div>
      </div>
      <div className="jactions">
        <IconButton icon="grid" label="Browse videos" onClick={() => onOpen(feed)} />
        <IconButton icon="refresh" label="Check now" disabled={feed.checking} onClick={() => run(() => api.checkFeed(feed.id))} />
        {feed.paused ? (
          <IconButton icon="play" label="Resume checking" onClick={() => run(() => api.pauseFeed(feed.id, false))} />
        ) : (
          <IconButton icon="pause" label="Pause checking" onClick={() => run(() => api.pauseFeed(feed.id, true))} />
        )}
        <IconButton icon="sliders" label="Options" onClick={() => onEdit(feed)} />
        <IconButton icon="external" label="Open in browser" onClick={() => run(() => api.openExternal(feed.url))} />
        <IconButton icon="trash" label="Unfollow" danger onClick={() => onUnfollow(feed)} />
      </div>
      {feed.error && (
        <div className="jerr">
          <ErrorPanel error={feed.error} onRetry={() => run(() => api.checkFeed(feed.id))} compact />
        </div>
      )}
    </div>
  )
})

export function FollowingPage() {
  const {state, attempt, toast, setSettings} = useApp()
  const settings = state.settings
  const feeds = state.feeds
  const [link, setLink] = useState('')
  const [dialog, setDialog] = useState<{link?: {url: string; title: string; uploader: string}; feed?: Feed; options: DownloadOptions} | null>(null)
  const [unfollowing, setUnfollowing] = useState<Feed | null>(null)
  const [openId, setOpenId] = useState('')
  const open = feeds.find((f) => f.id === openId)
  const now = useNow(30_000)
  const every = settings?.followEvery ?? 6

  const setEvery = async (hours: number) => {
    if (!settings) return
    const res = await attempt(api.saveSettings({...settings, followEvery: hours}))
    if (!res) return
    if (res.error) return toast('error', res.error)
    setSettings(res.settings)
  }

  const add = () => {
    const url = link.trim()
    if (!url || !settings) return
    setDialog({link: {url, title: '', uploader: ''}, options: feedDefaults(settings.defaults)})
  }

  // "Follow" from the Chrome helper's menu.
  const follow = state.follow
  useEffect(() => {
    if (!follow || !settings) return
    setOpenId('')
    setDialog({link: {url: follow.url, title: '', uploader: ''}, options: feedDefaults(settings.defaults)})
    // Only a new request opens it, not a change of settings.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [follow?.n, !!settings])

  const unfollow = async (f: Feed) => {
    setUnfollowing(null)
    try {
      await api.unfollowFeed(f.id)
      toast('info', `Stopped following ${f.title}`)
    } catch (e) {
      toast('error', errMessage(e))
    }
  }

  const active = feeds.filter((f) => !f.paused).length

  // An unfollowed feed's page closes with it.
  if (open) return <ChannelView key={open.id} feed={open} onBack={() => setOpenId('')} />

  return (
    <div className="content">
      <div className="page-head">
        <h1>Following</h1>
        <div className="spacer" />
        <label className="row" style={{gap: 8, color: 'var(--muted)', fontSize: 13}}>
          Check
          <select value={every} onChange={(e) => void setEvery(Number(e.target.value))} aria-label="How often to check">
            {INTERVALS.map((i) => (
              <option key={i.hours} value={i.hours}>
                {i.label}
              </option>
            ))}
          </select>
        </label>
        <Button icon="refresh" disabled={active === 0} onClick={() => void attempt(api.checkFeed(''))}>
          Check all now
        </Button>
      </div>

      <Card>
        <div className="row">
          <input
            type="text"
            className="grow"
            value={link}
            placeholder="Paste a channel or playlist link to follow"
            aria-label="Channel or playlist link"
            spellCheck={false}
            onChange={(e) => setLink(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') add()
            }}
          />
          <Button variant="primary" icon="bell" disabled={!link.trim()} onClick={add}>
            Follow…
          </Button>
        </div>
        <div className="hint" style={{marginTop: 8}}>
          New uploads are added to the queue {checkEvery(every)}, while Prem-dl is open or in the tray. You can also follow a channel or playlist after analysing it on the
          Download page.
        </div>
      </Card>

      {feeds.length === 0 ? (
        <Empty title="You aren't following anything yet">
          Follow a YouTube channel or a playlist and its new videos download by themselves. Turn on “{systemWords(state.platform).startAtSignIn}” and “Keep running when the window is closed” in
          Settings to catch them while the window is closed.
        </Empty>
      ) : (
        <div style={{marginTop: 16}}>
          {feeds.map((f) => (
            <FeedRow key={f.id} feed={f} now={now} every={every} onOpen={(feed) => setOpenId(feed.id)} onEdit={(feed) => setDialog({feed, options: feed.options})} onUnfollow={setUnfollowing} />
          ))}
        </div>
      )}

      {dialog && <FeedDialog link={dialog.link} feed={dialog.feed} options={dialog.options} onClose={() => setDialog(null)} onSaved={() => dialog.link && setLink('')} />}
      {unfollowing && (
        <Modal
          title={`Unfollow ${unfollowing.title}?`}
          onClose={() => setUnfollowing(null)}
          footer={
            <>
              <Button onClick={() => setUnfollowing(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => void unfollow(unfollowing)}>
                Unfollow
              </Button>
            </>
          }>
          <p>New uploads won't be downloaded any more. Downloads it already added stay in the queue, and files stay on disk.</p>
        </Modal>
      )}
    </div>
  )
}
