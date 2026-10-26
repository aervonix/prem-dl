import {Card} from '../../components/ui'
import {OptionsPanel} from '../../components/OptionsPanel'
import type {TabProps} from './tab'

export function DefaultsTab({draft, upd}: TabProps) {
  return (
    <Card id="defaults" title="Default options" sub="What new downloads start with. You can still change them for each download.">
      <OptionsPanel value={draft.defaults} onChange={(defaults) => upd({defaults})} mode="defaults" playlist />
    </Card>
  )
}
