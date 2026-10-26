import {api} from '../../api'
import {Button, Card, Field} from '../../components/ui'
import {useApp} from '../../store'
import type {TabProps} from './tab'

/** Tool locations, extra yt-dlp arguments and the data folder. */
export function AdvancedTab({draft, upd}: TabProps) {
  const {state, attempt} = useApp()
  return (
    <Card id="advanced" title="Advanced">
      <div className="stack">
        <Field label="yt-dlp location" hint="Leave empty to find it automatically (next to the app, in the working folder, the app's own copy, then PATH).">
          <div className="row">
            <input type="text" className="grow" value={draft.ytdlpPath} placeholder="Automatic" onChange={(e) => upd({ytdlpPath: e.target.value})} spellCheck={false} />
            <Button
              onClick={async () => {
                const p = await api.pickFile('Choose yt-dlp', 'Programs (*.exe)', '*.exe')
                if (p) upd({ytdlpPath: p})
              }}>
              Browse…
            </Button>
          </div>
        </Field>
        <Field label="ffmpeg location" hint="A folder containing ffmpeg.exe, or the file itself. Leave empty to find it automatically.">
          <div className="row">
            <input type="text" className="grow" value={draft.ffmpegPath} placeholder="Automatic" onChange={(e) => upd({ffmpegPath: e.target.value})} spellCheck={false} />
            <Button
              onClick={async () => {
                const p = await api.pickDirectory('Choose the folder containing ffmpeg.exe', draft.ffmpegPath)
                if (p) upd({ffmpegPath: p})
              }}>
              Browse…
            </Button>
          </div>
        </Field>
        <Field
          label="Extra yt-dlp arguments"
          hint="Added to every download and applied last, so they can override the options above. For example: --sleep-interval 3 --extractor-args &quot;youtube:player_client=web&quot;">
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
