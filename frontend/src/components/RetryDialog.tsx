import {useEffect, useState} from 'react'
import {api, errMessage} from '../api'
import {useApp} from '../store'
import type {DownloadOptions, Job, VideoInfo} from '../types'
import {isAudioFormat, OptionsPanel, optionsProblem} from './OptionsPanel'
import {Button, Modal, Spinner} from './ui'

/** Changes a failed or cancelled download's options and runs it again. */
export function RetryDialog({job, onClose}: {job: Job; onClose: () => void}) {
  const {state, toast} = useApp()
  const [opts, setOpts] = useState<DownloadOptions>(() => structuredClone(job.options))
  // The video is read again so its formats, heights and subtitles can be offered.
  const [video, setVideo] = useState<VideoInfo | null>(null)
  const [reading, setReading] = useState(true)
  const [unreadable, setUnreadable] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let alive = true
    api
      .probeJob(job.id)
      .then((res) => {
        if (!alive) return
        const v = res.result?.kind === 'video' ? res.result.video : null
        if (v) {
          setVideo(v)
          // Jobs queued before audio-only picks were flagged: tell from the list.
          setOpts((o) => (o.preset === 'custom' ? {...o, formatAudioOnly: isAudioFormat(v, o.formatId)} : o))
        } else if (!res.cancelled) setUnreadable(true)
      })
      .catch(() => alive && setUnreadable(true))
      .finally(() => alive && setReading(false))
    return () => {
      alive = false
    }
  }, [job.id])

  const problem = optionsProblem(opts)
  const save = async () => {
    setSaving(true)
    try {
      await api.retryJobWith(job.id, opts)
      onClose()
    } catch (e) {
      toast('error', errMessage(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title={`Change options: ${job.title}`}
      onClose={onClose}
      footer={
        <>
          <span className="truncate" style={{flex: 1, minWidth: 0, color: 'var(--danger)', fontSize: 12.5}}>
            {problem}
          </span>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" icon="retry" loading={saving} disabled={!!problem} onClick={() => void save()}>
            Save and retry
          </Button>
        </>
      }>
      {reading ? (
        <div className="row muted" style={{marginBottom: 12}}>
          <Spinner /> Reading the video’s formats…
        </div>
      ) : unreadable ? (
        <div className="hint" style={{marginBottom: 12}}>
          The video couldn’t be read again, so its formats can’t be listed. The quality presets still work.
        </div>
      ) : null}
      <OptionsPanel value={opts} onChange={setOpts} video={video} playlist={job.playlistCount > 0} mode="download" archiveOn={state.settings?.useArchive} />
    </Modal>
  )
}
