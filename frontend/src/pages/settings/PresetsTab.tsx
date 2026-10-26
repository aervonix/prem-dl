import {Card} from '../../components/ui'
import {PresetList} from '../../components/PresetList'
import type {TabProps} from './tab'

export function PresetsTab({draft, upd}: TabProps) {
  return (
    <Card id="presets" title="Presets" sub="Named sets of options to pick on the Download page. A preset tied to a site is picked by itself for links from it.">
      <PresetList presets={draft.presets} defaults={draft.defaults} onChange={(presets) => upd({presets})} />
    </Card>
  )
}
