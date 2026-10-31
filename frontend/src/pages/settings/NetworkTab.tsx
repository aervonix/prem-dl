import {Card, Field, Switch} from '../../components/ui'
import type {TabProps} from './tab'

/** Speed limit, proxy, IPv4, retries and pauses. */
export function NetworkTab({draft, upd}: TabProps) {
  return (
    <Card id="network" title="Network">
      <div className="stack">
        <div className="grid2">
          <Field label="Speed limit" hint="Per download, e.g. 500K or 2M. Empty = unlimited.">
            <input type="text" value={draft.rateLimit} placeholder="Unlimited" onChange={(e) => upd({rateLimit: e.target.value})} />
          </Field>
          <Field label="Proxy" hint="e.g. http://127.0.0.1:8080 or socks5://127.0.0.1:1080. Also used for update checks.">
            <input type="text" value={draft.proxy} placeholder="None" onChange={(e) => upd({proxy: e.target.value})} spellCheck={false} />
          </Field>
          <Field label="Parallel fragments" hint="Threads used per download for segmented (DASH/HLS) streams.">
            <input type="number" min={1} max={16} value={draft.concurrentFragments} onChange={(e) => upd({concurrentFragments: Number(e.target.value)})} />
          </Field>
          <Field label="Retries" hint="How often a failed connection or fragment is retried within one download.">
            <input type="number" min={0} max={50} value={draft.retries} onChange={(e) => upd({retries: Number(e.target.value)})} />
          </Field>
        </div>
        <Switch
          checked={draft.forceIPv4}
          onChange={(v) => upd({forceIPv4: v})}
          label="Use IPv4 only"
          description="Turn this on if analysing links or downloads hang or time out while websites open fine in your browser. Some networks hand out IPv6 addresses that don't work; browsers quietly fall back, the download tools wait each one out."
        />
        <Switch
          checked={draft.activeHours.on}
          onChange={(v) => upd({activeHours: {...draft.activeHours, on: v}})}
          label="Only start downloads during certain hours"
          description="For a data cap that's free at night, or to keep the connection free by day. Downloads already running finish; the rest wait. The queue has a Start now button."
        />
        {draft.activeHours.on && (
          <div className="grid2">
            <Field label="From">
              <input type="time" value={draft.activeHours.from} onChange={(e) => upd({activeHours: {...draft.activeHours, from: e.target.value}})} />
            </Field>
            <Field label="Until" hint="Earlier than From means overnight.">
              <input type="time" value={draft.activeHours.to} onChange={(e) => upd({activeHours: {...draft.activeHours, to: e.target.value}})} />
            </Field>
          </div>
        )}
        <Switch
          checked={draft.autoRetry}
          onChange={(v) => upd({autoRetry: v})}
          label="Retry failed downloads automatically"
          description="When a download fails with a problem that usually passes (a dropped connection, a 403, rate limiting), try it again after 30 seconds, 2 minutes and 10 minutes (longer when rate-limited)."
        />
        <div className="grid2">
          <Field label="Pause between requests (seconds)" hint="Slows how fast a download reads the site. 1–2 helps if YouTube keeps rate-limiting you. 0 = no pause.">
            <input type="number" min={0} max={300} step={0.5} value={draft.sleepRequests} onChange={(e) => upd({sleepRequests: Number(e.target.value)})} />
          </Field>
          <Field label="Wait before each download (seconds)" hint="A gap before every download starts, for long playlists. 0 = no wait.">
            <input type="number" min={0} max={3600} value={draft.sleepInterval} onChange={(e) => upd({sleepInterval: Number(e.target.value)})} />
          </Field>
        </div>
      </div>
    </Card>
  )
}
