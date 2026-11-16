import { usePoll } from '../lib/poll'
import { fetchStatus, fetchUpdate, fetchUsage, type UsageView } from '../api'

// StatusBar is the bottom bar: daemon state and live counts, polled from
// /api/status, a quiet note when a newer release is out, and the Claude and
// Codex usage limits on the right.
export function StatusBar() {
  const { data, error } = usePoll(fetchStatus, 5000)
  return (
    <footer className="flex items-center justify-between gap-4 border-t border-border bg-panel dark:bg-bg px-4 py-1.5 text-xs text-dim">
      {error || !data ? (
        <span>connecting to the daemon...</span>
      ) : (
        <span>
          <span style={{ color: 'var(--done)' }}>&#9679;</span> {data.daemon} &middot;{' '}
          {data.activeSessions} running &middot; {data.waiting} waiting on you &middot;{' '}
          {data.dayAIHours.toFixed(1)} AI-hours today
        </span>
      )}
      <span className="flex items-center gap-4">
        <UpdateChip />
        <Usage />
      </span>
    </footer>
  )
}

// UpdateChip is a small link to Settings' Updates card when a newer release
// is out, polled every 30 minutes (the daemon itself asks GitHub twice a day).
// Pull, not push: no popup, nothing to dismiss, gone once updated.
function UpdateChip() {
  const { data } = usePoll(fetchUpdate, 30 * 60_000)
  if (!data?.Available || !data.Latest) return null
  return (
    <a
      href="#/settings?section=updates"
      title={`DevTopo ${data.Latest} is out (you run ${data.Current})`}
      className="text-2xs text-dim underline-offset-2 hover:text-accent hover:underline"
    >
      Update available: {data.Latest}
    </a>
  )
}

// Usage draws one small gauge per usage window, polled every 5 minutes (the
// server caches the endpoint for as long): Claude's, then Codex's when there is
// Codex data. A failed read shows nothing.
function Usage() {
  const { data } = usePoll(fetchUsage, 5 * 60_000)
  if (!data) return null
  const codex = data.Codex ?? []
  return (
    <span className="flex items-center gap-4">
      {data.Windows.length === 0 ? (
        <span className="text-2xs text-dim">{data.Status}</span>
      ) : (
        <>
          <ClaudeMark />
          {data.Windows.map((w) => (
            <Gauge key={w.Label} w={w} />
          ))}
        </>
      )}
      {codex.length > 0 && (
        <>
          <CodexMark />
          {codex.map((w) => (
            <Gauge key={`codex-${w.Label}`} id={`codex-${w.Label}`} w={w} />
          ))}
        </>
      )}
    </span>
  )
}

// CodexMark is OpenAI's knot, which Codex wears: six rounded links around the
// centre, each set off to one side and turned 60 degrees so they interlace, in
// the text colour so it reads on both themes.
function CodexMark() {
  return (
    <svg role="img" aria-label="Codex usage" viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0 text-fg">
      <title>Codex usage</title>
      <g fill="none" stroke="currentColor" strokeWidth="1.6">
        {Array.from({ length: 6 }, (_, i) => (
          <rect key={i} x="10.2" y="1.5" width="6.4" height="12" rx="3.2" transform={`rotate(${i * 60} 12 12)`} />
        ))}
      </g>
    </svg>
  )
}

// ClaudeMark is Claude's starburst in its brand terracotta, which reads on both
// themes: twelve rounded rays of alternating length around the centre.
function ClaudeMark() {
  return (
    <svg role="img" aria-label="Claude usage" viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0">
      <title>Claude usage</title>
      <g stroke="#D97757" strokeWidth="2.6" strokeLinecap="round">
        {Array.from({ length: 12 }, (_, i) => (
          <line key={i} x1="12" y1="12" x2="12" y2={i % 2 ? 3.5 : 1.5} transform={`rotate(${i * 30} 12 12)`} />
        ))}
      </g>
    </svg>
  )
}

// Gauge is one window; id (its label by default) keys its test ids.
function Gauge({ w, id = w.Label }: { w: UsageView['Windows'][number]; id?: string }) {
  const reset = untilReset(w.ResetsAt, new Date())
  const pct = Math.round(w.Percent)
  const title = `${w.Label}: ${pct}% used` + (reset ? `, resets ${new Date(w.ResetsAt).toLocaleString()}` : '')
  const color = w.Percent > 85 ? 'var(--danger)' : w.Percent >= 60 ? 'var(--waiting)' : 'var(--accent)'
  return (
    <span data-testid={`usage-${id}`} title={title} className="flex items-center gap-1.5 text-2xs">
      <span className="text-fg">{w.Label}</span>
      <span className="h-1.5 w-16 overflow-hidden rounded-full bg-border">
        <span
          data-testid={`usage-fill-${id}`}
          className="block h-full rounded-full"
          style={{ width: `${w.Percent}%`, background: color }}
        />
      </span>
      {reset && <span className="tabular-nums">{reset}</span>}
    </span>
  )
}

// untilReset is the time left until resetsAt: "4d 3h", "4h 40m" or "35m", and
// '' when there is no reset or it has passed.
export function untilReset(resetsAt: string, now: Date): string {
  const mins = Math.floor((new Date(resetsAt).getTime() - now.getTime()) / 60_000)
  if (!(mins > 0)) return ''
  const d = Math.floor(mins / 1440)
  const h = Math.floor((mins % 1440) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${mins % 60}m`
  return `${mins}m`
}
