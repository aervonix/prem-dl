import {useCallback, useEffect, useState} from 'react'
import {Icon} from '../icons'
import {useApp} from '../store'
import type {Toast} from '../store'
import {FinishNotices} from './FinishNotices'
import {Button, IconButton} from './ui'

export function Toasts() {
  const {state, dismissToast} = useApp()
  // Each toast is its own live region (status, or alert for errors); a live container around
  // them would announce everything twice.
  return (
    <div className="toasts">
      {/* Finished-download cards share the corner, above the toasts, so the two never overlap. */}
      <FinishNotices />
      {state.toasts.map((t) => (
        <ToastItem key={t.id} t={t} dismiss={dismissToast} />
      ))}
    </div>
  )
}

function ToastItem({t, dismiss}: {t: Toast; dismiss: (id: number) => void}) {
  const done = useCallback(() => dismiss(t.id), [dismiss, t.id])
  // The countdown stops while the pointer or focus is on the toast, so it can be read or acted on.
  const [held, setHeld] = useState(false)
  useEffect(() => {
    if (held) return
    const timer = window.setTimeout(done, t.kind === 'error' ? 9000 : 5000)
    return () => window.clearTimeout(timer)
  }, [held, done, t.kind])

  return (
    <div
      className={`toast ${t.kind}`}
      role={t.kind === 'error' ? 'alert' : 'status'}
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={() => setHeld(false)}>
      <span className="ic">
        <Icon name={t.kind === 'error' ? 'alert' : t.kind === 'success' ? 'checkCircle' : 'info'} />
      </span>
      <span style={{flex: 1}}>{t.text}</span>
      {t.action && (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            t.action?.run()
            done()
          }}>
          {t.action.label}
        </Button>
      )}
      <IconButton icon="x" label="Dismiss" onClick={done} />
    </div>
  )
}
