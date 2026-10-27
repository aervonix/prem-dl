import {useEffect, useState} from 'react'
import {api, on} from '../api'
import type {CloseRequest} from '../api'
import {plural} from '../format'
import {Button, Modal} from './ui'

/**
 * "Quit Prem-dl?" when the window is closed while downloads run. The backend
 * keeps the window open and asks here; Quit closes it for real, anything else keeps it open.
 */
export function CloseConfirm() {
  const [ask, setAsk] = useState<CloseRequest | null>(null)

  useEffect(
    () =>
      on('app:confirm-close', (req) => {
        setAsk(req)
        // Without this the backend falls back to the system dialog on the next close.
        void api.closeShown()
      }),
    [],
  )

  if (!ask) return null
  const stay = () => {
    setAsk(null)
    void api.cancelClose()
  }
  const quit = () => void api.confirmClose()
  const hide = () => {
    setAsk(null)
    void api.hideWindow()
  }

  return (
    <Modal
      title="Downloads in progress"
      onClose={stay}
      footer={
        <>
          <Button variant="danger" onClick={quit}>
            Quit anyway
          </Button>
          {ask.background && <Button onClick={hide}>Download in the background</Button>}
          <Button variant="primary" autoFocus onClick={stay}>
            Keep downloading
          </Button>
        </>
      }>
      <p>
        {plural(ask.downloads, 'download')} {ask.downloads === 1 ? 'is' : 'are'} still running. Quit Prem-dl anyway?
      </p>
      <p className="muted">{ask.downloads === 1 ? 'It will be paused' : 'They will be paused'}; partial files are kept, so you can resume next time.</p>
      {ask.background && <p className="muted">Or close the window and let them finish: Prem-dl stays in the notification area until you quit it there.</p>}
    </Modal>
  )
}
