import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {ComponentType} from 'react'
import {api, errMessage} from '../api'
import {Button} from '../components/ui'
import {useApp} from '../store'
import type {Settings} from '../types'
import {AdvancedTab} from './settings/AdvancedTab'
import {CookiesTab} from './settings/CookiesTab'
import {DefaultsTab} from './settings/DefaultsTab'
import {GeneralTab} from './settings/GeneralTab'
import {NetworkTab} from './settings/NetworkTab'
import {PresetsTab} from './settings/PresetsTab'
import type {TabProps} from './settings/tab'
import {UpdatesTab} from './settings/UpdatesTab'

const SECTIONS: readonly {id: string; label: string; Tab: ComponentType<TabProps>}[] = [
  {id: 'general', label: 'General', Tab: GeneralTab},
  {id: 'defaults', label: 'Default options', Tab: DefaultsTab},
  {id: 'presets', label: 'Presets', Tab: PresetsTab},
  {id: 'cookies', label: 'Cookies', Tab: CookiesTab},
  {id: 'network', label: 'Network', Tab: NetworkTab},
  {id: 'updates', label: 'Updates & tools', Tab: UpdatesTab},
  {id: 'advanced', label: 'Advanced', Tab: AdvancedTab},
]
const isSection = (id: string) => SECTIONS.some((s) => s.id === id)

export function SettingsPage() {
  const {state, setSettings, toast} = useApp()
  const saved = state.settings
  const [draft, setDraft] = useState<Settings | null>(saved)
  const [saving, setSaving] = useState(false)
  // Bumped by Discard to remount General, whose "Custom template…" pick describes the draft.
  const [discards, setDiscards] = useState(0)

  const dirty = useMemo(() => !!saved && !!draft && JSON.stringify(saved) !== JSON.stringify(draft), [saved, draft])

  // Follow saved changes (imports, "save as defaults", changes the backend makes): fields the user
  // hasn't touched take the new value, unsaved edits are kept.
  const prevSaved = useRef(saved)
  useEffect(() => {
    const prev = prevSaved.current
    prevSaved.current = saved
    if (!saved || prev === saved) return
    setDraft((d) => {
      if (!d || !prev) return saved
      const next: Record<string, unknown> = {...d}
      for (const k of Object.keys(saved) as (keyof Settings)[]) {
        if (JSON.stringify(d[k]) === JSON.stringify(prev[k])) next[k] = saved[k]
      }
      return next as unknown as Settings
    })
  }, [saved])

  // One section shows at a time. Switching starts at the top of the page. The others stay mounted
  // (hidden), so a check or a Chrome cookie request running on one survives a look at another.
  const [tab, setTab] = useState('general')
  const rootRef = useRef<HTMLDivElement>(null)
  const openTab = useCallback((id: string) => {
    setTab(id)
    rootRef.current?.closest('.page')?.scrollTo({top: 0})
  }, [])

  // Open the section requested from elsewhere (error panels, banners), once per request: the
  // request stays in state, and opening Settings later must not jump there again.
  const handledFor = useRef(0)
  useEffect(() => {
    if (!state.section || state.page !== 'settings' || handledFor.current === state.section.n) return
    handledFor.current = state.section.n
    if (isSection(state.section.id)) openTab(state.section.id)
  }, [state.section, state.page, openTab])

  const upd = useCallback((patch: Partial<Settings>) => setDraft((d) => (d ? {...d, ...patch} : d)), [])

  if (!saved || !draft) return null

  const save = async () => {
    setSaving(true)
    try {
      const res = await api.saveSettings(draft)
      if (res.error) {
        toast('error', res.error)
        return
      }
      setSettings(res.settings)
      setDraft(res.settings)
      toast('success', 'Settings saved')
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="settings-layout" ref={rootRef}>
      <div className="page-head settings-head">
        <h1>Settings</h1>
      </div>
      <nav className="settings-nav" aria-label="Settings sections">
        {SECTIONS.map((s) => (
          <button key={s.id} type="button" aria-current={tab === s.id ? 'page' : undefined} onClick={() => openTab(s.id)}>
            {s.label}
          </button>
        ))}
      </nav>

      <div className="settings-body">
        {SECTIONS.map(({id, Tab}) => (
          <div key={id} className="stack" hidden={tab !== id}>
            <Tab key={id === 'general' ? discards : undefined} draft={draft} saved={saved} upd={upd} />
          </div>
        ))}

        {dirty && (
          <div className="savebar" role="region" aria-label="Unsaved changes">
            <span className="grow">You have unsaved changes</span>
            <Button
              variant="ghost"
              onClick={() => {
                setDraft(saved)
                setDiscards((n) => n + 1)
              }}>
              Discard
            </Button>
            <Button variant="primary" icon="save" loading={saving} onClick={() => void save()}>
              Save changes
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
