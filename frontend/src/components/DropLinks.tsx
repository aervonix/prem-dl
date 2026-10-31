import {useEffect, useState} from 'react'
import {MAX_DROPPED, droppedText, linksIn, readDrop} from '../drop'
import {Icon} from '../icons'
import {plural} from '../format'
import {useApp} from '../store'

/** Whether t takes typed text (where a paste or a drop of text is the box's own business). */
export function editable(t: EventTarget | null): boolean {
  return (
    t instanceof HTMLElement &&
    (t.isContentEditable || t instanceof HTMLTextAreaElement || (t instanceof HTMLInputElement && ['text', 'search', 'url', 'number', ''].includes(t.type)))
  )
}

/**
 * Links dropped anywhere on the window go to the Download page: one dragged from a browser, or a
 * .txt or .url file of them. A dropped cookies.txt goes to Settings to be imported.
 *
 * Every drop has to be handled here: left alone, WebView2 opens a dropped file or link in place of
 * the app.
 */
export function DropLinks() {
  const {openLinks, offerCookies, toast} = useApp()
  const [over, setOver] = useState(false)

  useEffect(() => {
    let depth = 0
    // Dragging selected text inside the app is the page's own business.
    let inside = false
    const wanted = (e: DragEvent) => {
      if (inside || !e.dataTransfer) return false
      const types = Array.from(e.dataTransfer.types)
      return types.includes('Files') || types.includes('text/uri-list') || types.includes('text/plain')
    }
    const onStart = () => (inside = true)
    const onEnd = () => (inside = false)
    const onEnter = (e: DragEvent) => {
      if (!wanted(e)) return
      e.preventDefault()
      depth++
      setOver(true)
    }
    const onOver = (e: DragEvent) => {
      if (!wanted(e)) return
      e.preventDefault()
      e.dataTransfer!.dropEffect = 'copy'
    }
    const onLeave = (e: DragEvent) => {
      if (!wanted(e)) return
      depth = Math.max(0, depth - 1)
      // relatedTarget is null once the pointer has left the window.
      if (depth === 0 || e.relatedTarget === null) {
        depth = 0
        setOver(false)
      }
    }
    const onDrop = (e: DragEvent) => {
      depth = 0
      setOver(false)
      const dt = e.dataTransfer
      if (inside || !dt) return
      // Plain text without links dropped in a text box is just typing.
      if (dt.files.length === 0 && linksIn(droppedText(dt)).length === 0 && editable(e.target)) return
      e.preventDefault()
      void readDrop(dt).then(({links, cookies, skipped}) => {
        if (cookies) return offerCookies(cookies)
        if (links.length > 0) {
          openLinks(links.slice(0, MAX_DROPPED))
          if (links.length > MAX_DROPPED) toast('info', `Only the first ${MAX_DROPPED} of ${links.length} links were added.`)
          return
        }
        if (skipped.length > 0) toast('info', skipped.length === 1 ? `${skipped[0]} isn't a list of links.` : `${plural(skipped.length, 'file')} without links.`)
        else toast('info', 'There are no links in what you dropped.')
      })
    }
    const events: [string, (e: DragEvent) => void][] = [
      ['dragstart', onStart],
      ['dragend', onEnd],
      ['dragenter', onEnter],
      ['dragover', onOver],
      ['dragleave', onLeave],
      ['drop', onDrop],
    ]
    for (const [name, fn] of events) window.addEventListener(name, fn as EventListener)
    return () => {
      for (const [name, fn] of events) window.removeEventListener(name, fn as EventListener)
    }
  }, [openLinks, offerCookies, toast])

  if (!over) return null
  return (
    <div className="drop-overlay" aria-hidden="true">
      <div className="drop-card">
        <Icon name="link" size={22} />
        <div className="drop-title">Drop to download</div>
        <div className="hint">Links, or .txt and .url files with links in them</div>
      </div>
    </div>
  )
}
