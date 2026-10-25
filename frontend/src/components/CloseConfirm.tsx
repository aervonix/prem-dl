import {useEffect, useState} from 'react'
import {api, on} from '../api'
import type {CloseRequest} from '../api'
import {plural} from '../format'
import {Button, Modal} from './ui'

/**
 * "Quit Prem-dl?" when the window is closed while downloads run or yt-dlp updates. The backend
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

  const updating = ask.downloads === 0 && ask.updating
  return (
    <Modal
      title={updating ? 'yt-dlp is updating' : 'Downloads in progress'}
      onClose={stay}
      footer={
        <>
          <Button variant="danger" onClick={quit}>
            Quit anyway
          </Button>
          <Button variant="primary" autoFocus onClick={stay}>
            {updating ? 'Wait' : 'Keep downloading'}
          </Button>
        </>
      }>
      {updating ? (
        <p>Quitting now abandons the update and keeps the current version of yt-dlp.</p>
      ) : (
        <>
          <p>
            {plural(ask.downloads, 'download')} {ask.downloads === 1 ? 'is' : 'are'} still running. Quit Prem-dl anyway?
          </p>
          <p className="muted">{ask.downloads === 1 ? 'It will be paused' : 'They will be paused'}; partial files are kept, so you can resume next time.</p>
        </>
      )}
    </Modal>
  )
}
