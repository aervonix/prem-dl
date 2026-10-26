import {memo, useCallback, useRef, useState} from 'react'
import type {Dispatch, MouseEvent, SetStateAction} from 'react'
import {ago, compactCount, duration} from '../format'
import {Icon} from '../icons'
import type {PlaylistEntry} from '../types'
import {Button} from './ui'

/** Whether an entry can be ticked: not private or deleted, not a playlist, not a stream yet to start. */
export function selectable(e: PlaylistEntry): boolean {
  return !e.unavailable && !e.isPlaylist && e.liveStatus !== 'is_upcoming'
}

/** "110K views · 3 days ago", from what the listing says. */
function metaLine(e: PlaylistEntry, uploader: boolean): string {
  return [uploader ? e.uploader : '', e.viewCount ? `${compactCount(e.viewCount)} views` : '', ago(e.timestamp)].filter(Boolean).join(' · ')
}

const Card = memo(function Card({
  entry,
  checked,
  status,
  uploader,
  onPick,
  onOpen,
}: {
  entry: PlaylistEntry
  checked: boolean
  status: string
  uploader: boolean
  onPick: (index: number, range: boolean) => void
  onOpen?: (url: string) => void
}) {
  const [failedSrc, setFailedSrc] = useState('')
  const e = entry
  const playlist = e.isPlaylist && !e.unavailable
  const can = selectable(e)
  const pick = (ev: MouseEvent) => {
    if (playlist) return onOpen?.(e.url)
    if (can) onPick(e.index, ev.shiftKey)
  }
  const title = e.title || e.id

  return (
    <div className={`vcard${checked ? ' on' : ''}${!can && !playlist ? ' off' : ''}`} onClick={pick} title={title}>
      <div className="vthumb">
        {e.thumbnail && failedSrc !== e.thumbnail ? (
          <img src={e.thumbnail} alt="" loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailedSrc(e.thumbnail)} />
        ) : (
          <Icon name={playlist ? 'list' : 'film'} size={26} />
        )}
        {e.liveStatus === 'is_live' && <span className="vtag live">LIVE</span>}
        {e.liveStatus === 'is_upcoming' && <span className="vtag">Upcoming</span>}
        {playlist && <span className="vtag">Playlist</span>}
        {e.duration > 0 && <span className="dur">{duration(e.duration)}</span>}
        {can && (
          // The control assistive tech meets; the card around it is a bigger target for the mouse.
          <input
            type="checkbox"
            className="vcheck"
            checked={checked}
            aria-label={`Select ${title}`}
            onClick={(ev) => {
              ev.stopPropagation()
              onPick(e.index, ev.shiftKey)
            }}
            onChange={() => {}}
          />
        )}
      </div>
      <div className="vtitle">{title}</div>
      <div className="vmeta truncate">
        {e.unavailable ? 'Unavailable (private or deleted)' : metaLine(e, uploader)}
        {status && <span className="vstatus"> · {status}</span>}
      </div>
      {playlist && (
        <Button
          size="sm"
          style={{marginTop: 6}}
          onClick={(ev) => {
            ev.stopPropagation()
            onOpen?.(e.url)
          }}>
          Open
        </Button>
      )}
    </div>
  )
})

/**
 * Videos as a grid of thumbnails, the way YouTube shows a channel, each with a checkbox. Shift+click
 * ticks or clears everything since the last one clicked. Entries are keyed by their index.
 */
export function VideoGrid({
  entries,
  selected,
  setSelected,
  onOpen,
  tall,
  uploader,
  status,
}: {
  entries: PlaylistEntry[]
  selected: Set<number>
  setSelected: Dispatch<SetStateAction<Set<number>>>
  /** Open a playlist listed among the videos (a channel's Playlists tab). */
  onOpen?: (url: string) => void
  /** Portrait thumbnails, for Shorts. */
  tall?: boolean
  /** Show each video's uploader (a playlist mixes channels; a channel page doesn't need it). */
  uploader?: boolean
  /** A word about each video, such as "Downloaded", or ''. */
  status?: (e: PlaylistEntry) => string
}) {
  const anchor = useRef<number | null>(null)
  const list = useRef(entries)
  list.current = entries

  const onPick = useCallback(
    (index: number, range: boolean) => {
      // Read now: the updater may run later, after the anchor has moved on.
      const from = range && anchor.current !== null ? list.current.findIndex((e) => e.index === anchor.current) : -1
      anchor.current = index
      setSelected((prev) => {
        const next = new Set(prev)
        const on = !prev.has(index)
        const to = list.current.findIndex((e) => e.index === index)
        if (from >= 0 && to >= 0) {
          for (const e of list.current.slice(Math.min(from, to), Math.max(from, to) + 1)) {
            if (!selectable(e)) continue
            if (on) next.add(e.index)
            else next.delete(e.index)
          }
        } else if (on) {
          next.add(index)
        } else {
          next.delete(index)
        }
        return next
      })
    },
    [setSelected],
  )

  let lastGroup = ''
  return (
    <div className={`vgrid${tall ? ' tall' : ''}${selected.size > 0 ? ' selecting' : ''}`} role="group" aria-label="Videos">
      {entries.map((e) => {
        const header = e.group && e.group !== lastGroup ? (
          <div key={`g-${e.index}`} className="vgroup">
            {e.group}
          </div>
        ) : null
        lastGroup = e.group
        const card = <Card key={e.index} entry={e} checked={selected.has(e.index)} status={status?.(e) ?? ''} uploader={!!uploader} onPick={onPick} onOpen={onOpen} />
        return header ? [header, card] : card
      })}
    </div>
  )
}
