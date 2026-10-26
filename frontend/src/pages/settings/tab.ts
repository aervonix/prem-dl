import type {Settings} from '../../types'

/** What every Settings tab gets: the unsaved draft, the saved copy, and a way to change the draft. */
export interface TabProps {
  draft: Settings
  saved: Settings
  upd: (patch: Partial<Settings>) => void
}
