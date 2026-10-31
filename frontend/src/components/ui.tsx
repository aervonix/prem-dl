import {Children, cloneElement, isValidElement, useEffect, useId, useRef, useState} from 'react'
import type {ButtonHTMLAttributes, ReactNode} from 'react'
import {Icon} from '../icons'
import type {IconName} from '../icons'

/**
 * A button that asks for a second click before doing something that can't be undone: the first
 * arms it (its label says what the next click will do), and it disarms itself after 4 s.
 */
export function useConfirm(): [armed: boolean, press: (run: () => void, needed?: boolean) => void] {
  const [armed, setArmed] = useState(false)
  useEffect(() => {
    if (!armed) return
    const t = window.setTimeout(() => setArmed(false), 4000)
    return () => window.clearTimeout(t)
  }, [armed])
  const press = (run: () => void, needed = true) => {
    if (needed && !armed) return setArmed(true)
    setArmed(false)
    run()
  }
  return [armed, press]
}

export function Spinner() {
  return <span className="spinner" role="status" aria-label="Loading" />
}

type BtnProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'secondary' | 'primary' | 'ghost' | 'danger'
  size?: 'sm' | 'md' | 'lg'
  icon?: IconName
  loading?: boolean
}

export function Button({variant = 'secondary', size = 'md', icon, loading, children, className, disabled, ...rest}: BtnProps) {
  const cls = ['btn', variant === 'secondary' ? '' : variant, size === 'md' ? '' : size, className ?? ''].join(' ').trim()
  return (
    <button type="button" className={cls} disabled={disabled || loading} {...rest}>
      {loading ? <Spinner /> : icon ? <Icon name={icon} size={size === 'sm' ? 15 : 17} /> : null}
      {children}
    </button>
  )
}

export function IconButton({icon, label, danger, ...rest}: {icon: IconName; label: string; danger?: boolean} & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button type="button" className={`icon-btn${danger ? ' danger' : ''}`} title={label} aria-label={label} {...rest}>
      <Icon name={icon} size={17} />
    </button>
  )
}

export function Switch({
  checked,
  onChange,
  label,
  description,
  disabled,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: ReactNode
  description?: ReactNode
  disabled?: boolean
}) {
  return (
    <label className="switch" style={disabled ? {opacity: 0.5} : undefined}>
      <input type="checkbox" role="switch" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span className="track" />
      <span>
        <div className="label">{label}</div>
        {description && <div className="desc">{description}</div>}
      </span>
    </label>
  )
}

export interface SegOption<T extends string> {
  value: T
  label: ReactNode
  disabled?: boolean
  title?: string
  /** The option's name for screen readers, when label is only an icon. */
  ariaLabel?: string
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: SegOption<T>[]
  onChange: (v: T) => void
  label: string
}) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === value} disabled={o.disabled} title={o.title} aria-label={o.ariaLabel} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function Field({label, hint, children, className}: {label?: ReactNode; hint?: ReactNode; children: ReactNode; className?: string}) {
  const id = useId()
  return (
    <div className={`field ${className ?? ''}`}>
      {label && (
        <label className="label" htmlFor={id}>
          {label}
        </label>
      )}
      <FieldControl id={id}>{children}</FieldControl>
      {hint && <div className="hint">{hint}</div>}
    </div>
  )
}

// Passes the generated id to the field's input/select so the label is associated with it: the
// child itself, or the first one inside a plain wrapper (a box with a Browse button beside it).
function FieldControl({id, children}: {id: string; children: ReactNode}) {
  let used = false
  const give = (node: ReactNode, depth: number): ReactNode => {
    if (used || !isValidElement<{id?: string; children?: ReactNode}>(node) || typeof node.type !== 'string') return node
    if (['input', 'select', 'textarea'].includes(node.type)) {
      used = true
      return node.props.id ? node : cloneElement(node, {id})
    }
    if (depth === 0 || node.props.children === undefined) return node
    const kids = Children.map(node.props.children, (c) => give(c, depth - 1))
    return used ? cloneElement(node, {}, kids) : node
  }
  return <>{give(children, 2)}</>
}

export function Card({
  title,
  sub,
  actions,
  children,
  id,
}: {
  title?: ReactNode
  sub?: ReactNode
  actions?: ReactNode
  children: ReactNode
  id?: string
}) {
  return (
    <section className="card" id={id} style={{scrollMarginTop: 8}}>
      {(title || actions) && (
        <div className="card-head">
          <div>
            <h2>{title}</h2>
            {sub && <div className="sub">{sub}</div>}
          </div>
          <div className="spacer" />
          {actions}
        </div>
      )}
      {children}
    </section>
  )
}

export function Progress({percent, state, label}: {percent: number; state?: 'busy' | 'done' | 'paused' | 'failed' | 'active'; label?: string}) {
  const unknown = percent < 0
  const cls = ['progress', state && state !== 'active' ? state : '', unknown ? 'indeterminate' : ''].join(' ').trim()
  return (
    <div className={cls} role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={unknown ? undefined : Math.round(percent)}>
      <i style={unknown ? undefined : {transform: `scaleX(${Math.min(100, Math.max(0, percent)) / 100})`}} />
    </div>
  )
}

/** A toggle (pressed set) or, with removes, a picked item that a click takes away. */
export function Chip({
  pressed,
  removes,
  onClick,
  children,
  title,
}: {
  pressed?: boolean
  /** The chip stands for something picked, and clicking it removes that (named for screen readers). */
  removes?: string
  onClick?: () => void
  children: ReactNode
  title?: string
}) {
  return (
    <button
      type="button"
      className={`chip${removes ? ' picked' : ''}`}
      aria-pressed={removes ? undefined : pressed}
      aria-label={removes ? `Remove ${removes}` : undefined}
      onClick={onClick}
      title={title}>
      {children}
    </button>
  )
}

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export function Modal({
  title,
  onClose,
  children,
  footer,
  dismissable = true,
}: {
  title: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  /** A click outside closes it. Off for dialogs with a form, where a stray click would lose the edits. */
  dismissable?: boolean
}) {
  const titleId = useId()
  const box = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  close.current = onClose
  // Read during the first render, before a child's autoFocus moves focus into the dialog.
  const [opener] = useState(() => document.activeElement as HTMLElement | null)

  // Focus moves into the dialog, Tab cycles inside it, and focus goes back to whatever opened it.
  useEffect(() => {
    // A child with autoFocus has already taken focus by now; keep it.
    if (!box.current?.contains(document.activeElement)) box.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      // A dialog on a hidden page (Ctrl+1 away from Settings) stays mounted; it must not keep
      // Tab and Escape from the page that is showing.
      if (!box.current || box.current.getClientRects().length === 0) return
      if (e.key === 'Escape') return close.current()
      if (e.key !== 'Tab' || !box.current) return
      const items = [...box.current.querySelectorAll<HTMLElement>(FOCUSABLE)]
      if (items.length === 0) return e.preventDefault()
      const first = items[0]
      const last = items[items.length - 1]
      const inside = box.current.contains(document.activeElement)
      if (e.shiftKey && (!inside || document.activeElement === first || document.activeElement === box.current)) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (!inside || document.activeElement === last)) {
        e.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
      opener?.focus?.()
    }
  }, [opener])
  return (
    <div className="overlay" onMouseDown={(e) => dismissable && e.target === e.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} ref={box}>
        <div className="modal-head">
          <h2 className="truncate" id={titleId} title={typeof title === 'string' ? title : undefined}>
            {title}
          </h2>
          <IconButton icon="x" label="Close" onClick={onClose} />
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-head" style={{borderTop: '1px solid var(--border)', borderBottom: 0, justifyContent: 'flex-end'}}>{footer}</div>}
      </div>
    </div>
  )
}

export function Empty({title, children}: {title: string; children?: ReactNode}) {
  return (
    <div className="empty">
      <h3>{title}</h3>
      {/* A div: callers pass buttons and blocks, which a <p> can't hold. */}
      {children && <div className="empty-text">{children}</div>}
    </div>
  )
}
