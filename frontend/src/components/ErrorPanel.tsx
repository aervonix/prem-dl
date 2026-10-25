import {api} from '../api'
import {Icon} from '../icons'
import {useApp} from '../store'
import type {AppError} from '../types'
import {Button} from './ui'

export const FFMPEG_INSTALL_CMD = 'winget install Gyan.FFmpeg'
export const EJS_DOCS = 'https://github.com/yt-dlp/yt-dlp/wiki/EJS'

/**
 * Presents a structured error from the backend: what happened, what to do, and a button that
 * performs the most likely fix.
 */
export function ErrorPanel({
  error,
  onRetry,
  onDetails,
  compact,
}: {
  error: AppError
  onRetry?: () => void
  onDetails?: () => void
  compact?: boolean
}) {
  const {go, updateYtdlp, toast, state} = useApp()
  const updating = state.toolProgress !== null

  const copy = async () => {
    try {
      await api.copyText(`${error.title}\n${error.message}\n\n${error.detail}`)
      toast('success', 'Error details copied')
    } catch {
      toast('error', 'Could not copy to the clipboard')
    }
  }

  return (
    <div className={`errpanel${compact ? ' compact' : ''}`} role="alert">
      <div className="t">
        <Icon name="warning" size={17} />
        {error.title}
      </div>
      <div className="m">{error.message}</div>
      {error.hint && <div className="h">{error.hint}</div>}
      <div className="a">
        {error.action === 'update' && (
          <Button size="sm" variant="primary" icon="refresh" loading={updating} onClick={() => void updateYtdlp()}>
            Update yt-dlp
          </Button>
        )}
        {error.action === 'install_ytdlp' && (
          <Button size="sm" variant="primary" icon="download" loading={updating} onClick={() => void updateYtdlp()}>
            Download yt-dlp
          </Button>
        )}
        {error.action === 'cookies' && (
          <Button size="sm" variant="primary" icon="cookie" onClick={() => go('settings', 'cookies')}>
            Cookie settings
          </Button>
        )}
        {error.action === 'settings' && (
          <Button size="sm" variant="primary" icon="settings" onClick={() => go('settings')}>
            Open settings
          </Button>
        )}
        {error.action === 'install_ffmpeg' && (
          <>
            <Button
              size="sm"
              variant="primary"
              icon="copy"
              onClick={() => {
                void api.copyText(FFMPEG_INSTALL_CMD)
                toast('info', `Copied “${FFMPEG_INSTALL_CMD}” — run it in a terminal, then restart the app`)
              }}>
              Copy install command
            </Button>
            <Button size="sm" icon="settings" onClick={() => go('settings', 'advanced')}>
              Set ffmpeg location
            </Button>
          </>
        )}
        {onRetry && error.retryable && (
          <Button size="sm" icon="retry" onClick={onRetry}>
            Try again
          </Button>
        )}
        {onDetails && (
          <Button size="sm" variant="ghost" onClick={onDetails}>
            Show log
          </Button>
        )}
        {error.detail && (
          <Button size="sm" variant="ghost" icon="copy" onClick={() => void copy()}>
            Copy details
          </Button>
        )}
      </div>
    </div>
  )
}
