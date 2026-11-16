import { useEffect, useState, type MouseEvent } from 'react'
import { fetchDetail, focusSession, refreshPoints, renameSession, type CardData, type SessionPoints } from '../api'
import { Icon } from './Icon'
import { Points } from './Points'
import { dateKey, finishedLabel, workingFor } from '../lib/timeline'

const statusVar: Record<string, string> = {
  running: 'var(--running)',
  waiting: 'var(--waiting)',
  idle: 'var(--idle)',
}

function signalText(signals: Record<string, number> | null): string {
  if (!signals) return ''
  return Object.entries(signals)
    .sort(([a], [b]) => Number(b === 'prompt') - Number(a === 'prompt'))
    .map(([k, v]) => `${v} ${k}${v === 1 ? '' : 's'}`)
    .join(', ')
}

function hhmm(iso: string): string {
  const d = new Date(iso)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// defaultTitle names a session the user has not renamed: its branch, or the
// repo's last segment when the branch says nothing (detached HEAD, none).
export function defaultTitle(card: Pick<CardData, 'Branch' | 'Project'>): string {
  return card.Branch && card.Branch !== 'HEAD' ? card.Branch : card.Project.split('/').pop() || card.Project
}

type Detail ={ state: 'loading' } | { state: 'done'; text: string } | { state: 'error'; error: string }

// hostLabels names the button that opens a session in its app; a live session
// in an app not listed gets no button.
const hostLabels: Record<string, string> = {
  orca: 'Open in Orca',
  vscode: 'Open in VS Code',
  cursor: 'Open in Cursor',
  'windows-terminal': 'Open in Terminal',
  console: 'Open in Terminal',
  terminal: 'Open in Terminal',
  iterm: 'Open in iTerm',
  'claude-desktop': 'Open in Claude app',
  'codex-app': 'Open in Codex',
}

// appOwned hosts keep a session open between turns, when no claude process
// runs for it, so it is opened there rather than resumed. Where DevTopo
// cannot bring an app forward (Linux) a live session gets no button and a
// closed one, app-owned or not, is resumed.
const appOwned = new Set(['vscode', 'cursor', 'claude-desktop', 'codex-app'])

// openAction is the button that opens a session: bring its app forward, or
// resume it. A live session in an app not listed gets none (null).
export function openAction(card: CardData): { action: 'focus' | 'resume'; label: string } | null {
  const focus = card.Live || (card.CanFocus && appOwned.has(card.Host))
  if (!focus) return { action: 'resume', label: 'Resume' }
  const label = card.CanFocus ? hostLabels[card.Host] : undefined
  return label ? { action: 'focus', label } : null
}

// Card is one session. Collapsed it shows project, branch and headline numbers;
// clicking it reveals what the session did today, buttons to show and hide a
// longer summary, and the whitelisted hover signals.
export function Card({ card }: { card: CardData }) {
  const [open, setOpen] = useState(false)
  const [resumeErr, setResumeErr] = useState('')
  const act = openAction(card)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [refreshed, setRefreshed] = useState<SessionPoints | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [refreshErr, setRefreshErr] = useState('')
  const [name, setName] = useState(card.Name ?? '')
  const [editing, setEditing] = useState(false)
  useEffect(() => setName(card.Name ?? ''), [card.Name])

  // rename saves what was typed (trimmed; empty goes back to the default title)
  // and shows it at once; the next poll brings the stored name back.
  function rename(typed: string) {
    setEditing(false)
    const next = typed.trim()
    if (next === name) return
    setName(next)
    renameSession(card.ID, next).catch(() => setName(card.Name ?? ''))
  }
  const signals = signalText(card.Signals)
  // A refresh shows at once, until a poll brings points that cover later activity.
  const useRefreshed = refreshed !== null && new Date(refreshed.AsOf) > new Date(card.TodayAsOf)
  const points = (useRefreshed ? refreshed.Points : card.TodayPoints) ?? []
  const pointsAsOf = useRefreshed ? refreshed.AsOf : card.TodayAsOf
  const activeToday = dateKey(new Date(card.LastActivity)) === dateKey(new Date())
  // A running task's time counts on here from its start, with no new data.
  const [now, setNow] = useState(() => new Date())
  const counting = card.Status === 'running' && !!card.WorkingSince
  useEffect(() => {
    if (!counting) return
    setNow(new Date())
    const tick = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(tick)
  }, [counting])
  const working = counting ? workingFor(card.WorkingSince, now) : ''
  const finished = card.Status === 'running' ? '' : finishedLabel(card.FinishedAt, new Date())

  async function refresh(e: MouseEvent) {
    e.stopPropagation()
    setRefreshing(true)
    setRefreshErr('')
    try {
      setRefreshed(await refreshPoints(card.ID))
    } catch (err) {
      setRefreshErr(err instanceof Error ? err.message : String(err))
    } finally {
      setRefreshing(false)
    }
  }

  const refreshButton = activeToday && (
    <button
      type="button"
      aria-label="Refresh summary"
      title="Refresh summary"
      disabled={refreshing}
      onClick={refresh}
      className="rounded p-0.5 text-dim hover:text-accent disabled:hover:text-dim"
    >
      <span className={`pointer-events-none block${refreshing ? ' animate-spin' : ''}`}>
        <Icon size={12}>
          <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" />
          <path d="M21 3v5h-5" />
          <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" />
          <path d="M8 16H3v5" />
        </Icon>
      </span>
    </button>
  )

  async function moreDetail(e: MouseEvent) {
    e.stopPropagation()
    setDetail({ state: 'loading' })
    try {
      setDetail({ state: 'done', text: (await fetchDetail(card.ID)).Text })
    } catch (err) {
      setDetail({ state: 'error', error: err instanceof Error ? err.message : String(err) })
    }
  }

  return (
    <div
      className="mb-2.5 cursor-pointer rounded-lg border border-border bg-panel p-3"
      style={{ borderLeft: `3px solid ${statusVar[card.Status] ?? 'var(--border)'}` }}
      onClick={() => setOpen((o) => !o)}
    >
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          {editing ? (
            <input
              autoFocus
              aria-label="Session name"
              defaultValue={name}
              onClick={(e) => e.stopPropagation()}
              onFocus={(e) => e.currentTarget.select()}
              onBlur={(e) => rename(e.currentTarget.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') setEditing(false)
                if (e.key === 'Enter') e.currentTarget.blur()
              }}
              className="w-full rounded border border-border bg-bg dark:bg-raised px-1 font-semibold"
            />
          ) : (
            <button
              type="button"
              title="Click to rename"
              onClick={(e) => {
                e.stopPropagation()
                setEditing(true)
              }}
              className="max-w-full truncate rounded text-left font-semibold hover:underline hover:decoration-dotted"
            >
              {name || card.Title || defaultTitle(card)}
            </button>
          )}
        </div>
        {card.Agent === 'codex' ? (
          <span className="shrink-0 rounded border border-border px-1 text-2xs text-codex">Codex</span>
        ) : (
          <span className="shrink-0 rounded border border-border px-1 text-2xs text-claude">Claude</span>
        )}
        {resumeErr && <span className="text-xs text-dim">{resumeErr}</span>}
        {act && (
          <button
            type="button"
            className="shrink-0 text-xs text-dim hover:text-fg"
            onClick={(e) => {
              e.stopPropagation()
              focusSession(card.ID, act.action).catch((err: Error) => {
                // why an app could not open a session is not always accurate, so
                // only a failed resume is shown; the server logs a failed open
                if (act.action === 'focus') return
                setResumeErr(err.message)
                setTimeout(() => setResumeErr(''), 8000)
              })
            }}
          >
            {act.label}
          </button>
        )}
      </div>
      <div data-testid="card-meta" className="mt-0.5 text-xs text-dim" title={signals}>
        <span className="break-all">{card.Project}</span> &middot;{' '}
        <span className="rounded bg-raised px-1.5 text-tag">{card.Branch}</span> &middot;{' '}
        <span className="text-aiH">{card.AIHours.toFixed(1)} AI-h</span> &middot;{' '}
        {card.Prompts} {card.Prompts === 1 ? 'prompt' : 'prompts'}
        {!!card.Decisions && (
          <>
            {' '}
            &middot; {card.Decisions} {card.Decisions === 1 ? 'decision' : 'decisions'}
          </>
        )}
        {card.Status === 'running' && <span className="text-running"> &middot; working{working && ` ${working}`}</span>}
        {finished && <> &middot; {finished}</>}
      </div>
      {!!card.BackgroundTasks && card.Status !== 'idle' && (
        <div className="mt-1.5 text-xs text-dim">
          {card.BackgroundTasks} background {card.BackgroundTasks === 1 ? 'task' : 'tasks'} running
        </div>
      )}
      {(card.RunningAgents ?? []).length > 0 && (
        <>
          <div className="mt-1.5 text-xs text-dim">
            {card.RunningAgents!.length} {card.RunningAgents!.length === 1 ? 'agent' : 'agents'} running
          </div>
          <ul aria-label="Running sub-agents" className="flex flex-wrap text-xs text-fg">
            {card.RunningAgents!.map((a, i) => (
              <li key={i} className="whitespace-pre-wrap">
                {i > 0 && ' · '}
                {a}
              </li>
            ))}
          </ul>
        </>
      )}
      {open && (
        <div className="mt-2 text-sm">
          {points.length > 0 ? (
            <>
              <Points points={points} />
              <div className="mt-1 flex items-center gap-1 text-xs text-dim">
                <span>as of {hhmm(pointsAsOf)}</span>
                {refreshButton}
              </div>
            </>
          ) : (
            <div className="flex items-center gap-1 text-dim">
              <span>{activeToday ? 'No summary yet' : 'No activity today'}</span>
              {refreshButton}
            </div>
          )}
          {refreshErr && <div className="mt-1 text-xs text-waiting">{refreshErr}</div>}
          {activeToday && detail?.state !== 'loading' && detail?.state !== 'done' && (
            <button type="button" className="mt-2 rounded-lg border border-border bg-bg dark:bg-raised px-2 py-0.5 text-xs hover:text-accent" onClick={moreDetail}>
              More detail
            </button>
          )}
          {detail?.state === 'loading' && <div className="mt-2 text-xs text-dim">Summarizing...</div>}
          {detail?.state === 'error' && <div className="mt-2 text-xs text-waiting">{detail.error}</div>}
          {detail?.state === 'done' && (
            <>
              <div className="mt-2 whitespace-pre-wrap">{detail.text}</div>
              <button
                type="button"
                className="mt-2 rounded-lg border border-border bg-bg dark:bg-raised px-2 py-0.5 text-xs hover:text-accent"
                onClick={(e) => {
                  e.stopPropagation()
                  setDetail(null)
                }}
              >
                Hide detail
              </button>
            </>
          )}
        </div>
      )}
      {open && signals && (
        <div data-testid="signals" className="mt-1 text-xs text-dim">
          {signals}
        </div>
      )}
    </div>
  )
}
