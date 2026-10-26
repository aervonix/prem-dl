import {useState} from 'react'
import {duration, parseTime, plural} from '../format'
import type {DownloadOptions, VideoInfo} from '../types'
import {Button, Field, Segmented} from './ui'
import type {SegOption} from './ui'

type Part = 'whole' | 'range' | 'chapters'

/** Picks what part of one video to download: all of it, a time range, or some of its chapters. */
export function PartPicker({value, onChange, video}: {value: DownloadOptions; onChange: (v: DownloadOptions) => void; video?: VideoInfo | null}) {
  const chapters = video?.chapters ?? []
  const [part, setPart] = useState<Part>('whole')
  // What the options hold wins; a choice with nothing filled in yet is remembered here. A new
  // video without chapters can't stay on "Chapters".
  const shown: Part =
    value.chapters.length > 0 ? 'chapters' : value.sectionStart || value.sectionEnd ? 'range' : part === 'chapters' && chapters.length === 0 ? 'whole' : part

  const choose = (p: Part) => {
    setPart(p)
    onChange({...value, sectionStart: '', sectionEnd: '', chapters: []})
  }
  const options: SegOption<Part>[] = [
    {value: 'whole', label: 'Whole video'},
    {value: 'range', label: 'Time range'},
    ...(chapters.length > 0 || value.chapters.length > 0 ? [{value: 'chapters' as Part, label: chapters.length > 0 ? `Chapters (${chapters.length})` : 'Chapters'}] : []),
  ]

  const startOk = value.sectionStart === '' || parseTime(value.sectionStart) !== null
  const endOk = value.sectionEnd === '' || parseTime(value.sectionEnd) !== null
  const s = parseTime(value.sectionStart)
  const e = parseTime(value.sectionEnd)
  const orderOk = s === null || e === null || s < e

  const picked = new Set(value.chapters)
  // Kept in the video's order. Chapters that share a title are picked together: yt-dlp finds
  // them by title, so it would download both anyway.
  const pick = (titles: Set<string>) => onChange({...value, chapters: [...new Set(chapters.filter((c) => titles.has(c.title)).map((c) => c.title))]})
  const toggle = (title: string) => {
    const next = new Set(picked)
    if (next.has(title)) next.delete(title)
    else next.add(title)
    pick(next)
  }
  const pickedSeconds = chapters.filter((c) => picked.has(c.title)).reduce((n, c) => n + Math.max(0, c.end - c.start), 0)

  return (
    <div className="optgroup">
      <div className="opt-head">Part of the video</div>
      <Segmented label="Part of the video" value={shown} options={options} onChange={choose} />

      {shown === 'range' && (
        <>
          <div className="row wrap" style={{marginTop: 8}}>
            <Field label="From" hint="e.g. 1:30">
              <input
                type="text"
                value={value.sectionStart}
                placeholder="0:00"
                aria-invalid={!startOk}
                style={{width: 110, ...(startOk ? {} : {borderColor: 'var(--danger)'})}}
                onChange={(ev) => onChange({...value, sectionStart: ev.target.value})}
              />
            </Field>
            <Field label="To" hint="blank = until the end">
              <input
                type="text"
                value={value.sectionEnd}
                placeholder="end"
                aria-invalid={!endOk}
                style={{width: 110, ...(endOk ? {} : {borderColor: 'var(--danger)'})}}
                onChange={(ev) => onChange({...value, sectionEnd: ev.target.value})}
              />
            </Field>
          </div>
          {(!startOk || !endOk) && <div className="hint" style={{color: 'var(--danger)'}}>Use seconds (90), minutes:seconds (1:30) or h:mm:ss.</div>}
          {startOk && endOk && !orderOk && <div className="hint" style={{color: 'var(--danger)'}}>The start must be before the end.</div>}
        </>
      )}

      {shown === 'chapters' && chapters.length > 0 && (
        <div style={{marginTop: 8}}>
          <div className="row wrap" style={{marginBottom: 8}}>
            <Button size="sm" onClick={() => pick(new Set(chapters.map((c) => c.title)))}>
              Select all
            </Button>
            <Button size="sm" onClick={() => pick(new Set())}>
              None
            </Button>
            <span className="muted">
              {picked.size === 0 ? 'Tick the chapters you want' : `${plural(picked.size, 'chapter')} · ${duration(pickedSeconds)}`}
            </span>
          </div>
          <div className="ch-list" role="group" aria-label="Chapters">
            {chapters.map((c, i) => (
              <label key={i} className="ch-row">
                {/* yt-dlp picks chapters by title, so one without a title can't be picked. */}
                <input type="checkbox" checked={picked.has(c.title)} disabled={!c.title.trim()} onChange={() => toggle(c.title)} />
                <span className="idx">{i + 1}</span>
                <span className="truncate" title={c.title}>
                  {c.title || `Chapter ${i + 1}`}
                </span>
                <span className="muted">
                  {duration(c.start) || '0:00'} · {duration(c.end - c.start)}
                </span>
              </label>
            ))}
          </div>
          <div className="hint" style={{marginTop: 6}}>
            {picked.size === 0
              ? 'With none ticked, the whole video downloads.'
              : 'Each chapter is saved as its own file, in a folder named after the video, as “03 - Chapter title”. Needs ffmpeg.'}
          </div>
        </div>
      )}

      {shown === 'chapters' && chapters.length === 0 && (
        // A queued job's options, before (or without) the video's chapter list.
        <div className="hint" style={{marginTop: 8}}>
          Picked: {value.chapters.join(', ')}.{' '}
          <Button size="sm" variant="ghost" onClick={() => choose('whole')}>
            Download the whole video instead
          </Button>
        </div>
      )}
    </div>
  )
}
