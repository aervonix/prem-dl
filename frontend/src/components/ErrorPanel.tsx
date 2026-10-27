import {useState} from 'react'
import {Icon} from '../icons'
import {useApp} from '../store'
import type {AppError} from '../types'
import {Button} from './ui'

export const EJS_DOCS = 'https://github.com/yt-dlp/yt-dlp/wiki/EJS'

/**
 * Presents a structured error from the backend: what happened, what to do, and a button that
 * performs the most likely fix.
 */
export function ErrorPanel({
  error,
  onRetry,
  onDetails,
  onEdit,
  onWait,
  compact,
}: {
  error: AppError
  onRetry?: () => void
  onDetails?: () => void
  /** For a stream that hasn't started: download it once it does. */
  onWait?: () => void
  /** Change the download's options and run it again. */
  onEdit?: () => void
  compact?: boolean
}) {
  const {go, repairTools, checkAppUpdate, toast, copy, state} = useApp()
  const [checking, setChecking] = useState(false)
  // A new Prem-dl shows in a banner at the top; say so when there isn't one.
  const checkUpdate = async () => {
    setChecking(true)
    const u = await checkAppUpdate()
    setChecking(false)
    if (u?.error) toast('error', `Couldn't check for updates: ${u.error}`)
    else if (u && !u.available) toast('info', `Prem-dl ${u.current} is the newest version. A fix may come in a later release.`)
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
        {error.action === 'update' && !state.appUpdate?.available && (
          <Button size="sm" variant="primary" icon="refresh" loading={checking} onClick={() => void checkUpdate()}>
            Check for updates
          </Button>
        )}
        {(error.action === 'repair' || error.action === 'install_ytdlp' || error.action === 'install_ffmpeg') && state.tools.builtIn && (
          <Button size="sm" variant="primary" icon="refresh" loading={state.repairing} onClick={() => void repairTools()}>
            Repair
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
        {onWait && error.code === 'upcoming' && (
          <Button size="sm" variant="primary" icon="clock" onClick={onWait}>
            Wait for it
          </Button>
        )}
        {onRetry && error.retryable && (
          <Button size="sm" icon="retry" onClick={onRetry}>
            Try again
          </Button>
        )}
        {onEdit && (
          <Button size="sm" icon="sliders" onClick={onEdit}>
            Change options…
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
