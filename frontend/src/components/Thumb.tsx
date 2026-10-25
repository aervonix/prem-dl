import {useEffect, useState} from 'react'
import {duration} from '../format'
import {Icon} from '../icons'

/** A thumbnail that falls back to an icon when the image is missing or fails to load. */
export function Thumb({src, seconds, className, style}: {src?: string; seconds?: number; className?: string; style?: React.CSSProperties}) {
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [src])
  return (
    <div className={className ?? 'thumb'} style={style}>
      {src && !failed ? <img src={src} alt="" loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailed(true)} /> : <Icon name="film" size={28} />}
      {seconds ? <span className="dur">{duration(seconds)}</span> : null}
    </div>
  )
}
