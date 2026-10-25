import {Icon} from '../icons'
import {useApp} from '../store'
import type {AppError} from '../types'
import {Button} from './ui'
import {InstallFfmpegButton} from './FfmpegInstall'

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
  const {go, updateYtdlp, copy, state} = useApp()
  const updating = state.toolProgress !== null

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
            <InstallFfmpegButton />
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
          <Button size="sm" variant="ghost" icon="copy" onClick={() => void copy(`${error.title}\n${error.message}\n\n${error.detail}`, 'Error details copied')}>
            Copy details
          </Button>
        )}
      </div>
    </div>
  )
}
