import {api} from '../../api'
import {Button, Card, Field} from '../../components/ui'
import {useApp} from '../../store'
import type {TabProps} from './tab'

/** Extra download arguments and the data folder. */
export function AdvancedTab({draft, upd}: TabProps) {
  const {state, attempt} = useApp()
  return (
    <Card id="advanced" title="Advanced">
      <div className="stack">
        <Field
          label="Extra download arguments"
          hint="Command-line options added to every download and applied last, so they can override the options above. For example: --sleep-interval 3 --extractor-args &quot;youtube:player_client=web&quot;">
          <textarea rows={2} className="mono" value={draft.extraArgs} onChange={(e) => upd({extraArgs: e.target.value})} spellCheck={false} />
        </Field>
        <div className="row wrap">
          <Button icon="folder" onClick={() => void attempt(api.openDataFolder())}>
            Open app data folder
          </Button>
          <span className="hint">Settings and the queue live in {state.dataDir}</span>
        </div>
        <div className="hint">Prem-dl {state.version}</div>
      </div>
    </Card>
  )
}
