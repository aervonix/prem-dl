import {useEffect, useState} from 'react'
import {Icon} from '../icons'
import {forPreset, parseSites} from '../presets'
import type {DownloadOptions, NamedPreset} from '../types'
import {OptionsPanel} from './OptionsPanel'
import {Button, Field, IconButton} from './ui'

// Typed text is kept while it still means the same sites, so a trailing comma or space survives.
function SitesInput({value, onChange}: {value: string[]; onChange: (v: string[]) => void}) {
  const [text, setText] = useState(value.join(', '))
  useEffect(() => {
    setText((t) => (parseSites(t).join() === value.join() ? t : value.join(', ')))
  }, [value])
  return (
    <input
      type="text"
      value={text}
      placeholder="None (pick it by hand)"
      spellCheck={false}
      onChange={(e) => {
        setText(e.target.value)
        onChange(parseSites(e.target.value))
      }}
    />
  )
}

function newName(presets: NamedPreset[]): string {
  const taken = new Set(presets.map((p) => p.name.toLowerCase()))
  for (let n = 1; ; n++) {
    const name = n === 1 ? 'New preset' : `New preset ${n}`
    if (!taken.has(name.toLowerCase())) return name
  }
}

/** The presets in Settings: names, sites and options, saved with the rest of the page. */
export function PresetList({presets, defaults, onChange}: {presets: NamedPreset[]; defaults: DownloadOptions; onChange: (p: NamedPreset[]) => void}) {
  const [open, setOpen] = useState<number | null>(null)
  const set = (i: number, patch: Partial<NamedPreset>) => onChange(presets.map((p, j) => (j === i ? {...p, ...patch} : p)))
  return (
    <div className="stack">
      {presets.length === 0 && <div className="muted">No presets yet. Add one here, or use “Save as preset…” on the Download page.</div>}
      {presets.map((p, i) => (
        <div key={i} className="preset">
          <div className="preset-head">
            <Field label="Name">
              <input type="text" maxLength={40} value={p.name} onChange={(e) => set(i, {name: e.target.value})} />
            </Field>
            <Field label="Use it for links from">
              <SitesInput value={p.sites} onChange={(sites) => set(i, {sites})} />
            </Field>
            <div className="preset-actions">
              <Button icon={open === i ? 'chevronDown' : 'chevronRight'} aria-expanded={open === i} onClick={() => setOpen(open === i ? null : i)}>
                Options
              </Button>
              <IconButton
                icon="trash"
                label={`Delete ${p.name || 'preset'}`}
                danger
                onClick={() => {
                  onChange(presets.filter((_, j) => j !== i))
                  setOpen(null)
                }}
              />
            </div>
          </div>
          {open === i && (
            <div className="preset-body">
              <OptionsPanel value={p.options} onChange={(options) => set(i, {options})} mode="preset" playlist />
            </div>
          )}
        </div>
      ))}
      <div>
        <Button
          icon="plus"
          onClick={() => {
            onChange([...presets, {name: newName(presets), sites: [], options: forPreset(defaults)}])
            setOpen(presets.length)
          }}>
          Add preset
        </Button>
      </div>
      <div className="hint">
        <Icon name="info" size={13} /> A site also covers its subdomains, and the most specific one wins: with presets for youtube.com and music.youtube.com, a Music link picks the second.
      </div>
    </div>
  )
}
