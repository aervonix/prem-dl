import {Icon} from '../icons'
import {useApp} from '../store'
import {Button, IconButton} from './ui'

export function Toasts() {
  const {state, dismissToast} = useApp()
  return (
    <div className="toasts" aria-live="polite">
      {state.toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind}`} role={t.kind === 'error' ? 'alert' : 'status'}>
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
                dismissToast(t.id)
              }}>
              {t.action.label}
            </Button>
          )}
          <IconButton icon="x" label="Dismiss" onClick={() => dismissToast(t.id)} />
        </div>
      ))}
    </div>
  )
}
