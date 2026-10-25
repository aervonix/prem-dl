import {bytes} from '../format'
import {useApp} from '../store'
import {Button, Progress} from './ui'

export const FFMPEG_INSTALL_CMD = 'winget install Gyan.FFmpeg'

/**
 * Installs the app's own copy of ffmpeg. Where there's no build for this computer it copies the
 * winget command instead.
 */
export function InstallFfmpegButton({variant = 'primary', label = 'Install ffmpeg'}: {variant?: 'primary' | 'secondary'; label?: string}) {
  const {state, installFfmpeg, copy} = useApp()
  if (!state.tools.canInstallFfmpeg) {
    return (
      <Button size="sm" variant={variant} icon="copy" onClick={() => void copy(FFMPEG_INSTALL_CMD, `Copied “${FFMPEG_INSTALL_CMD}”. Run it in a terminal, then restart the app.`)}>
        Copy install command
      </Button>
    )
  }
  return (
    <Button size="sm" variant={variant} icon="download" loading={state.ffmpegProgress !== null} onClick={() => void installFfmpeg()}>
      {label}
    </Button>
  )
}

/** Progress of a running ffmpeg install; nothing otherwise. */
export function FfmpegProgress() {
  const p = useApp().state.ffmpegProgress
  if (!p) return null
  const unpacking = p.total > 0 && p.done >= p.total
  const text = unpacking ? 'Unpacking…' : p.total > 0 ? `Downloading ffmpeg: ${bytes(p.done)} of ${bytes(p.total)}` : 'Starting…'
  return (
    <div style={{marginTop: 8}}>
      <Progress percent={p.total > 0 && !unpacking ? (p.done / p.total) * 100 : -1} label="Installing ffmpeg" />
      <div className="hint" style={{marginTop: 4}}>
        {text}
      </div>
    </div>
  )
}
