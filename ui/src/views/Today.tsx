import { useEffect, useRef, useState, type ReactNode } from 'react'
import { usePoll } from '../lib/poll'
import { fetchToday, type LaneData, type StartedTicket, type TodayView, type YourPR } from '../api'
import { Card } from '../components/Card'
import { ReviewBadge, reviewLabel } from '../components/ReviewBadge'
import { dropTarget, itemTone, Legend, Tag, targetKey, ticketColor, Timeline, tone, track, type Preview } from '../components/Timeline'
import { addItem, deleteItem, fetchPlans, markItemDone, markTicketDone, setOwnPRDone, updateItem, type ItemBody, type Plan, type PlanItem } from '../planner-api'
import { ago, atHour, clamp, codeHostName, dateKey, dropStart, fmtDur, fmtHour, fmtNum, hoursIn, isDone, kindOf, reviewURL, type Kind } from '../lib/timeline'

const laneColor: Record<string, string> = {
  running: 'var(--running)',
  waiting: 'var(--waiting)',
  idle: 'var(--idle)',
}

const head = 'mb-2.5 border-b-2 pb-1.5 text-xs font-semibold uppercase tracking-wide'

function Lane({ lane }: { lane: LaneData }) {
  const cards = lane.Cards ?? []
  return (
    <section className={lane.Status === 'idle' ? 'opacity-75' : ''}>
      <h2 className={head} style={{ color: laneColor[lane.Status], borderColor: laneColor[lane.Status] ?? 'var(--border)' }}>
        {`${lane.Title} (${cards.length})`}
      </h2>
      {cards.length === 0 ? (
        <p className="text-sm text-dim">Nothing here.</p>
      ) : (
        cards.map((c) => <Card key={c.ID} card={c} />)
      )}
    </section>
  )
}

const tracker = (source: string) => (source === 'jira' ? 'Jira' : 'Linear')
const today = () => dateKey(new Date())
const doneToday = (it: PlanItem) => it.DoneAt !== null && dateKey(new Date(it.DoneAt)) === today()
const ticketKey = (it: PlanItem) => (kindOf(it) === 'ticket' ? it.RefRepo : '')

// Finished is one row of Finished today: a ticket, a review or an activity. Its time
// only orders the rows.
type Finished = { at: string; label: string; text: string; url?: string }

export function Tile(props: { label: string; num: number | string; unit?: string; sub: string; color: string }) {
  return (
    <div role="group" aria-label={props.label} className="min-w-0 rounded-[10px] border border-t-[3px] border-border bg-bg px-3 py-2.5 shadow-sm" style={{ borderTopColor: props.color }}>
      {/* One line each, always: a wrapped label would push this number below its neighbours'. */}
      <div className="truncate text-2xs font-bold uppercase tracking-wider text-dim">{props.label}</div>
      <div className={`mt-0.5 text-xl font-extrabold tabular-nums ${Number(props.num) === 0 ? 'text-dim' : ''}`}>
        {props.num}
        {props.unit && <small className="ml-0.5 text-sm font-bold text-dim">{props.unit}</small>}
      </div>
      <div className="truncate text-xs text-dim">{props.sub}</div>
    </div>
  )
}

// Target is what the done menu acts on: a plan item, a ticket started today, or
// one of the user's PRs, which is marked done on the PR while it is open.
type Target = { title: string; key: string; when: string; done: boolean; ticket: { source: string; id: string; url: string } | null; reviewURL?: string; host?: string; ownPR?: { url: string; open: boolean }; itemID: string; rect: DOMRect }

// DoneMenu marks its target done or not done next to where it was clicked; for
// a ticket that moves it in its tracker too. A plan item can also leave the plan,
// which touches nothing else. Escape or a click outside closes it.
function DoneMenu({ t, onClose, onSaved }: { t: Target; onClose: () => void; onSaved: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const down = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && onClose()
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', down)
      document.removeEventListener('keydown', key)
    }
  }, [onClose])

  const mark = () =>
    (t.ownPR ? setOwnPRDone(t.ownPR.url, !t.done) : t.ticket ? markTicketDone(t.ticket.source, t.ticket.id, !t.done) : markItemDone(t.itemID, !t.done)).then(onSaved, (e: Error) => setError(e.message))
  const remove = () => deleteItem(t.itemID).then(onSaved, (e: Error) => setError(e.message))
  // Opens below the clicked element, or above it when there is no room below.
  const place = t.rect.bottom + 200 > window.innerHeight ? { bottom: window.innerHeight - t.rect.top + 6 } : { top: t.rect.bottom + 6 }
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label={t.title}
      className="fixed z-50 w-[280px] max-w-[calc(100vw-32px)] rounded-[10px] border border-border bg-bg dark:bg-raised px-3 py-2.5 shadow-xl"
      style={{ left: clamp(t.rect.left, 16, window.innerWidth - 296), ...place }}
    >
      <b className="block text-sm leading-snug">
        {t.key && `${t.key} `}
        {t.title}
      </b>
      <small className="mb-2.5 mt-0.5 block text-xs text-dim">{t.when}</small>
      {t.ticket?.url && (
        <a href={t.ticket.url} target="_blank" rel="noreferrer" className="mb-1.5 block rounded-lg border border-border px-2.5 py-1.5 text-center text-sm font-bold text-accent">
          Open in {tracker(t.ticket.source)}
        </a>
      )}
      {t.reviewURL && (
        <a href={t.reviewURL} target="_blank" rel="noreferrer" className="mb-1.5 block rounded-lg border border-border px-2.5 py-1.5 text-center text-sm font-bold text-accent">
          Open on {t.host}
        </a>
      )}
      {t.ownPR && !t.ownPR.open ? (
        <p className="text-xs text-dim">No longer open on {t.host}.</p>
      ) : (
        <button
          type="button"
          onClick={mark}
          className={`w-full rounded-lg px-2.5 py-1.5 text-sm font-bold ${t.done ? 'border border-border text-fg' : 'bg-done text-bg'}`}
        >
          {t.done ? 'Mark not done' : 'Mark done'}
        </button>
      )}
      {t.itemID && (
        <button type="button" onClick={remove} className="mt-1.5 w-full rounded-lg border border-border px-2.5 py-1.5 text-sm font-bold text-fg">
          Remove from plan
        </button>
      )}
      {t.ticket && (
        <p className="mt-1.5 text-xs text-dim">
          {t.done ? `Moves ${t.key} back to in progress in ${tracker(t.ticket.source)}.` : `Also moves ${t.key} to Done in ${tracker(t.ticket.source)}.`}
        </p>
      )}
      {error && (
        <p role="alert" className="mt-1.5 text-xs text-waiting">
          Could not save: {error}
        </p>
      )}
    </div>
  )
}

// Pill is a ticket's status, in its status colour.
const Pill = ({ text, stage }: { text: string; stage: string }) => <Tag text={text} color={ticketColor(stage, text)} />

// Chip is one task in a strip; its main part opens the done menu, and drags
// onto the calendar to give the task a time.
function Chip(props: {
  kind: Kind
  // color, for a ticket, is its status colour.
  color?: string
  hours: number
  tkey: string
  text: string
  done: boolean
  tag: ReactNode
  onPick: (e: React.MouseEvent<HTMLElement>) => void
  onDrag: (e: React.PointerEvent) => void
  children?: ReactNode
}) {
  return (
    <div className="flex max-w-full items-center gap-2 rounded-lg border border-l-4 border-border bg-bg dark:bg-raised px-2.5 py-1 shadow-sm" style={{ borderLeftColor: tone(props.kind, props.color).borderLeftColor }}>
      <button
        type="button"
        onClick={props.onPick}
        onPointerDown={props.onDrag}
        className={`flex min-w-0 cursor-grab select-none items-center gap-2 text-left ${props.done ? 'opacity-60' : ''}`}
        style={{ touchAction: 'none' }}
      >
        <span className="font-mono text-xs font-bold">
          {props.done && '✓ '}
          {fmtNum(props.hours)}h
        </span>
        {props.tkey && <span className="font-mono text-xs text-dim">{props.tkey}</span>}
        <span className={`min-w-0 truncate ${props.done ? 'line-through' : ''}`}>{props.text}</span>
        {props.tag}
      </button>
      {props.children}
    </div>
  )
}

function Strip(props: { label: string; count: number; note?: string; empty: string; children: ReactNode[] }) {
  return (
    <section aria-label={props.label} className="mt-3">
      <h3 className="mb-1.5 text-xs font-semibold text-dim">
        {props.label} · <b className="text-fg">{props.count}</b>
        {props.note && <span className="font-medium"> - {props.note}</span>}
      </h3>
      <div className="flex flex-wrap gap-2">{props.children.length ? props.children : <span className="text-xs italic text-dim">{props.empty}</span>}</div>
    </section>
  )
}

// PlanCard is today's plan: tabs when there are several, a calendar or a list
// of it, how much is done, and the strips of what has no time yet and of what
// was started outside the plan. A click on a task opens the done menu. The
// calendar is the planner's: blocks move and resize, and chips dragged onto it
// get a time, all saved to the plan.
function PlanCard({ plans, started, reload }: { plans: Plan[]; started: StartedTicket[]; reload: () => void }) {
  const [selected, setSelected] = useState('')
  const [view, setView] = useState<'Calendar' | 'List'>('Calendar')
  const [menu, setMenu] = useState<Target | null>(null)
  const [over, setOver] = useState<string | null>(null)
  const [ghost, setGhost] = useState<{ x: number; y: number; text: string; kind: Kind } | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  // dragged is set by a chip drag, so the click that ends it opens no menu.
  const dragged = useRef(false)

  if (!plans.length) {
    return (
      <section aria-label="Today's plan">
        <p className="text-sm text-dim">
          No plan for today.{' '}
          <a href="#/planner" className="underline">
            Open the planner
          </a>
        </p>
      </section>
    )
  }

  const p = plans.find((x) => x.ID === selected) ?? plans[0]
  const date = today()
  const win = { start: hoursIn(date, p.WindowStart), end: hoursIn(date, p.WindowEnd) }
  const hours = { from: Math.min(7, Math.floor(win.start)), to: Math.max(20, Math.ceil(win.end)) }
  const rows = p.Rows ?? []
  const items = p.Items ?? []
  const timed = items.filter((it) => it.Start !== null).sort((a, b) => Date.parse(a.Start!) - Date.parse(b.Start!))
  const anytime = items.filter((it) => it.Start === null)
  const startOf = (it: PlanItem) => hoursIn(date, it.Start!)
  const rowName = (it: PlanItem) => rows.find((r) => r.ID === it.RowID)?.Name ?? ''

  const iso = (h: number) => atHour(date, h).toISOString()
  const save = (q: Promise<unknown>) => q.then(reload).catch(() => {})
  const body = (it: PlanItem, patch: Partial<ItemBody>): ItemBody => ({ rowId: it.RowID, text: it.Text, start: it.Start, intendedHours: it.IntendedHours, ...patch })
  // dragChip drags a chip onto a plan row, where place gives it that row and a
  // start, centred on the pointer.
  const dragChip = (e: React.PointerEvent, text: string, kind: Kind, h: number, place: (rowId: string, start: string) => Promise<unknown>) => {
    e.preventDefault()
    dragged.current = false
    track(
      (ev) => {
        if (!dragged.current && Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 5) return
        dragged.current = true
        const target = dropTarget(ev.clientX, ev.clientY, hours)
        // Over a row the chip shows at its own length where it will land; elsewhere it floats.
        const onRow = target && typeof target === 'object' && 'rowId' in target
        setPreview(onRow ? { rowId: target.rowId, start: dropStart(target.hour, h, hours), hours: h, kind, text } : null)
        setGhost(onRow ? null : { x: ev.clientX, y: ev.clientY, text, kind })
        setOver(targetKey(target))
      },
      (ev) => {
        setGhost(null)
        setPreview(null)
        setOver(null)
        const t = dragged.current && dropTarget(ev.clientX, ev.clientY, hours)
        if (t && typeof t === 'object' && 'rowId' in t) save(place(t.rowId, iso(dropStart(t.hour, h, hours))))
      },
    )
  }

  const pickItem = (it: PlanItem, e: React.MouseEvent<HTMLElement>) =>
    setMenu({
      title: it.Text,
      key: ticketKey(it),
      when: it.Start === null ? `${fmtDur(it.IntendedHours)}, no time yet` : `${fmtHour(startOf(it))}-${fmtHour(startOf(it) + it.IntendedHours)} · ${rowName(it)}`,
      done: isDone(it),
      ticket: kindOf(it) === 'ticket' ? { source: it.RefSource, id: it.RefID, url: it.RefURL } : null,
      reviewURL: kindOf(it) === 'review' || kindOf(it) === 'yourpr' ? reviewURL(it) : '',
      host: codeHostName(it.RefSource),
      ownPR: kindOf(it) === 'yourpr' ? { url: reviewURL(it), open: !!it.RefURL } : undefined,
      itemID: it.ID,
      rect: e.currentTarget.getBoundingClientRect(),
    })
  const pickStarted = (t: StartedTicket, e: React.MouseEvent<HTMLElement>) =>
    setMenu({
      title: t.Title,
      key: t.Key,
      when: `Started ${fmtHour(hoursIn(date, t.StartedAt))}, not in your plan`,
      done: t.Stage === 'completed',
      ticket: { source: t.Source, id: t.ID, url: t.URL },
      itemID: '',
      rect: e.currentTarget.getBoundingClientRect(),
    })
  // tag is a ticket's status pill, an open own PR's review state, or the row an
  // activity sits in.
  const tag = (it: PlanItem) =>
    kindOf(it) === 'yourpr' && !isDone(it) ? <ReviewBadge state={it.RefState} /> : kindOf(it) === 'ticket' ? <Pill text={isDone(it) ? 'Done' : it.RefState} stage={isDone(it) ? 'completed' : it.RefStage} /> : <span className="text-xs text-dim">{rowName(it)}</span>

  const listRow = (it: PlanItem) => {
    const d = isDone(it)
    return (
      <li key={it.ID}>
        <button
          type="button"
          onClick={(e) => pickItem(it, e)}
          className={`grid w-full grid-cols-[96px_minmax(0,1fr)_auto] items-center gap-3 border-b border-l-4 border-border px-3 py-1.5 text-left hover:bg-panel ${d ? 'opacity-60' : ''}`}
          style={{ borderLeftColor: itemTone(it, d).borderLeftColor }}
        >
          <span className="font-mono text-xs text-dim">
            {d && '✓ '}
            {it.Start === null ? fmtDur(it.IntendedHours) : `${fmtHour(startOf(it))}-${fmtHour(startOf(it) + it.IntendedHours)}`}
          </span>
          <span className={`min-w-0 truncate ${d ? 'line-through' : ''}`}>
            {ticketKey(it) && <span className="mr-1.5 font-mono text-xs text-dim">{ticketKey(it)}</span>}
            {it.Text}
          </span>
          <span className="flex items-center gap-1.5">{tag(it)}</span>
        </button>
      </li>
    )
  }
  const listHead = (text: string) => <li className="border-b border-border bg-panel px-3 py-1.5 text-2xs font-bold uppercase tracking-wider text-dim">{text}</li>

  return (
    <section aria-label="Today's plan" className="rounded-[14px] border border-border bg-panel px-3.5 pb-3.5 pt-3 shadow-sm">
      <div className="mb-2.5 flex flex-wrap items-center gap-x-3.5 gap-y-2.5">
        <h2 className="text-xs font-bold uppercase tracking-wider text-accent">Today's plan</h2>
        {plans.length > 1 && (
          <div role="tablist" className="flex flex-wrap gap-1">
            {plans.map((x) => (
              <button
                key={x.ID}
                role="tab"
                aria-selected={x === p}
                onClick={() => setSelected(x.ID)}
                className={`rounded-full border px-3 py-0.5 text-xs ${x === p ? 'border-accent bg-bg dark:bg-raised text-fg shadow-[inset_0_-2px_0_var(--accent)]' : 'border-border text-dim'}`}
              >
                {x.Name}{' '}
                <span className="font-mono">
                  {fmtHour(hoursIn(date, x.WindowStart))}-{fmtHour(hoursIn(date, x.WindowEnd))}
                </span>
              </button>
            ))}
          </div>
        )}
        <div role="group" aria-label="View" className="inline-flex overflow-hidden rounded-lg border border-border bg-bg dark:bg-raised">
          {(['Calendar', 'List'] as const).map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={view === v}
              onClick={() => {
                setView(v)
                setMenu(null)
              }}
              className={`px-3 py-1 text-xs font-bold ${view === v ? 'bg-accent text-bg' : 'text-dim'}`}
            >
              {v}
            </button>
          ))}
        </div>
        <a href={`#/planner?plan=${p.ID}`} className="ml-auto whitespace-nowrap rounded-lg bg-accent px-3 py-1.5 text-sm font-bold text-bg">
          Edit in Planner →
        </a>
      </div>

      {view === 'Calendar' ? (
        <Timeline
          date={date}
          view={hours}
          win={win}
          rows={rows}
          items={items}
          meetings={p.Meetings ?? []}
          over={over}
          onOver={setOver}
          onMove={(it, rowId, start) => save(updateItem(it.ID, body(it, { rowId, start: start === null ? null : iso(start) })))}
          onResize={(it, h) => save(updateItem(it.ID, body(it, { intendedHours: h })))}
          onPick={pickItem}
          isDone={isDone}
          preview={preview}
        />
      ) : (
        <ul aria-label="Plan items" className="overflow-hidden rounded-[10px] border border-border bg-bg dark:bg-raised">
          {listHead(`Scheduled · ${timed.length}`)}
          {timed.map(listRow)}
          {listHead(`No time yet · ${anytime.length}`)}
          {anytime.map(listRow)}
        </ul>
      )}

      {/* The list already shows the tasks without a time, so this strip is for the calendar only. */}
      {view === 'Calendar' && (
        <Strip label="Picked for today, no time yet" count={anytime.length} empty="Nothing picked without a time.">
          {anytime.map((it) => (
            <Chip
              key={it.ID}
              kind={kindOf(it)}
              color={itemTone(it, isDone(it)).borderLeftColor as string}
              hours={it.IntendedHours}
              tkey={ticketKey(it)}
              text={it.Text}
              done={isDone(it)}
              tag={tag(it)}
              onPick={(e) => !dragged.current && pickItem(it, e)}
              onDrag={(e) => dragChip(e, it.Text, kindOf(it), it.IntendedHours, (rowId, start) => updateItem(it.ID, body(it, { rowId, start })))}
            />
          ))}
        </Strip>
      )}
      <Strip label="Started today, not in your plan" count={started.length} note="tickets that moved to in progress in Linear or Jira today" empty="Nothing started outside the plan.">
        {started.map((t) => (
          <Chip
            key={`${t.Source}/${t.ID}`}
            kind="ticket"
            color={ticketColor(t.Stage, t.State)}
            hours={t.Hours}
            tkey={t.Key}
            text={t.Title}
            done={false}
            tag={<Pill text={t.State} stage={t.Stage} />}
            onPick={(e) => !dragged.current && pickStarted(t, e)}
            onDrag={(e) =>
              dragChip(e, t.Title, 'ticket', t.Hours, (rowId, start) => addItem(p.ID, { rowId, refSource: t.Source, refId: t.ID, text: t.Title, start, intendedHours: t.Hours }))
            }
          >
            <span className="whitespace-nowrap font-mono text-xs text-dim">since {fmtHour(hoursIn(date, t.StartedAt))}</span>
            <button
              type="button"
              onClick={() =>
                addItem(p.ID, { rowId: '', refSource: t.Source, refId: t.ID, text: t.Title, start: null, intendedHours: t.Hours })
                  .then(reload)
                  .catch(() => {})
              }
              className="whitespace-nowrap rounded-md border border-accent px-2 text-xs font-bold text-accent"
            >
              + Add to plan
            </button>
          </Chip>
        ))}
      </Strip>
      <Legend />

      {ghost && (
        <div data-testid="drag-ghost" className="pointer-events-none fixed z-50 max-w-[260px] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-l-4 border-border px-2.5 py-1.5 text-sm shadow-xl" style={{ left: ghost.x, top: ghost.y, ...tone(ghost.kind) }}>
          {ghost.text}
        </div>
      )}

      {menu && (
        <DoneMenu
          key={`${menu.itemID}/${menu.ticket?.id}`}
          t={menu}
          onClose={() => setMenu(null)}
          onSaved={() => {
            setMenu(null)
            reload()
          }}
        />
      )}
    </section>
  )
}

// YourPRs is the user's open PRs that have a review. A row opens the done menu,
// and joins the first of today's plans without a time, or leaves the plan it is in.
function YourPRs({ prs, plans, reload }: { prs: YourPR[]; plans: Plan[]; reload: () => void }) {
  const [menu, setMenu] = useState<Target | null>(null)
  const [showDone, setShowDone] = useState(false)
  const planned = new Map(plans.flatMap((p) => (p.Items ?? []).map((it) => [it.RefID, it.ID] as const)))
  const open = prs.filter((pr) => !pr.Done)
  const done = prs.filter((pr) => pr.Done)
  const row = (pr: YourPR) => (
    <li key={pr.RefID} className="flex items-center gap-3 rounded-lg border border-l-4 border-border bg-panel px-3 py-2 text-sm" style={{ borderLeftColor: tone('yourpr').borderLeftColor }}>
      <button
        type="button"
        onClick={(e) =>
          setMenu({
            title: pr.Title,
            key: '',
            when: pr.Done ? 'Marked done' : `${reviewLabel(pr.ReviewState)} by ${pr.Reviewer}, ${ago(pr.ReviewedAt, new Date())}`,
            done: pr.Done,
            ticket: null,
            reviewURL: pr.URL,
            host: codeHostName(pr.Source),
            ownPR: { url: pr.URL, open: true },
            itemID: '',
            rect: e.currentTarget.getBoundingClientRect(),
          })
        }
        className={`grid min-w-0 flex-1 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 text-left ${pr.Done ? 'opacity-60' : ''}`}
      >
        <ReviewBadge state={pr.ReviewState} />
        <span className={`truncate ${pr.Done ? 'line-through' : ''}`}>{pr.Title}</span>
        <span className="whitespace-nowrap font-mono text-xs text-dim">
          {pr.Repo} · {pr.Source} · {pr.Reviewer} · {ago(pr.ReviewedAt, new Date())}
        </span>
      </button>
      {planned.has(pr.RefID) ? (
        <>
          <span className="whitespace-nowrap text-xs text-dim">In plan</span>
          <button
            type="button"
            aria-label={`Remove ${pr.Title} from plan`}
            onClick={() =>
              deleteItem(planned.get(pr.RefID)!)
                .then(reload)
                .catch(() => {})
            }
            className="whitespace-nowrap rounded-md border border-border px-2 text-xs font-bold text-dim hover:text-fg"
          >
            Remove
          </button>
        </>
      ) : (
        plans.length > 0 && (
          <button
            type="button"
            aria-label={`Add ${pr.Title} to plan`}
            onClick={() =>
              addItem(plans[0].ID, { rowId: '', refSource: pr.Source, refId: pr.RefID, text: pr.Title, start: null, intendedHours: pr.Hours })
                .then(reload)
                .catch(() => {})
            }
            className="whitespace-nowrap rounded-md border border-accent px-2 text-xs font-bold text-accent"
          >
            + Add to plan
          </button>
        )
      )}
    </li>
  )
  return (
    <section aria-label="Your PRs">
      <h2 className={head} style={{ color: 'var(--tag)', borderColor: 'var(--tag)' }}>
        {`Your PRs · reviewed, need you (${open.length})`}
      </h2>
      {open.length === 0 ? <p className="text-sm text-dim">Nothing needs you right now.</p> : <ul className="flex flex-col gap-2">{open.map(row)}</ul>}
      {done.length > 0 && (
        <div className="mt-2">
          <button type="button" onClick={() => setShowDone((s) => !s)} className="text-xs text-dim hover:text-fg">
            {showDone ? 'Hide' : 'Show'} done ({done.length})
          </button>
          {showDone && <ul className="mt-2 flex flex-col gap-2">{done.map(row)}</ul>}
        </div>
      )}
      {menu && (
        <DoneMenu
          key={menu.ownPR?.url}
          t={menu}
          onClose={() => setMenu(null)}
          onSaved={() => {
            setMenu(null)
            reload()
          }}
        />
      )}
    </section>
  )
}

function FinishedList({ label, rows }: { label: string; rows: Finished[] }) {
  return (
    <div role="group" aria-label={label} className="min-w-0 rounded-[10px] border border-border bg-bg dark:bg-raised px-3 py-2.5">
      <h3 className="mb-1.5 text-xs font-bold">
        {label} <span className="font-semibold text-dim">{rows.length}</span>
      </h3>
      {rows.length === 0 ? (
        <p className="text-xs italic text-dim">None yet.</p>
      ) : (
        <ul>
          {rows.map((r, i) => (
            <li key={i} className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-2.5 py-0.5 text-sm">
              <span className="whitespace-nowrap font-mono text-xs text-dim">{r.label}</span>
              {r.url ? (
                <a href={r.url} target="_blank" rel="noreferrer" className="truncate hover:underline">
                  {r.text}
                </a>
              ) : (
                <span className="truncate">{r.text}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// finishedToday sorts out what got finished today: tickets from the trackers,
// and the other tasks - plan items marked done today plus reviews submitted on
// a code host - with the counts of reviews and own activities among them.
function finishedToday(data: TodayView, plans: Plan[]) {
  const newest = (a: Finished, b: Finished) => Date.parse(b.at) - Date.parse(a.at)
  // A planned review's ref is its PR url: one also submitted on its host today is
  // already in ReviewsDone, so it is not counted twice.
  const submitted = new Set(data.ReviewsDone.map((r) => r.URL))
  const marked = plans.flatMap((p) =>
    (p.Items ?? [])
      .filter((it) => kindOf(it) !== 'ticket' && doneToday(it) && !(kindOf(it) === 'review' && submitted.has(it.RefID)))
      .map((it) => ({ it, row: p.Rows?.find((r) => r.ID === it.RowID)?.Name })),
  )
  const markedReviews = marked.filter(({ it }) => kindOf(it) === 'review').length
  return {
    tickets: data.FinishedToday.map((t) => ({ at: t.CompletedAt, label: t.Key, text: t.Title, url: t.URL })).sort(newest),
    other: [
      ...marked.map(({ it, row }) => ({ at: it.DoneAt!, label: kindOf(it) === 'review' ? 'PR review' : row || 'Your activity', text: it.Text })),
      ...data.ReviewsDone.map((r) => ({ at: r.SubmittedAt, label: 'PR review', text: r.Title, url: r.URL })),
    ].sort(newest),
    reviews: markedReviews + data.ReviewsDone.length,
    own: marked.length - markedReviews,
  }
}

// The Today page is five modules the user can put in any order by dragging.
const modules = ['numbers', 'sessions', 'plan', 'yourprs', 'finished'] as const
type ModuleID = (typeof modules)[number]
const moduleName: Record<ModuleID, string> = {
  numbers: 'Today in numbers',
  yourprs: 'Your PRs',
  plan: "Today's plan",
  finished: 'Finished today',
  sessions: 'Sessions',
}
const orderKey = 'devtopo.today.order'

// savedOrder is the order the user last left. A module added since then goes
// right after the one before it by default, or first if that one isn't placed.
function savedOrder(): ModuleID[] {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(orderKey) ?? '[]')
    if (Array.isArray(saved)) {
      const order = modules.filter((m) => saved.includes(m)).sort((a, b) => saved.indexOf(a) - saved.indexOf(b))
      modules.forEach((m, i) => {
        if (!order.includes(m)) order.splice(i === 0 ? 0 : order.indexOf(modules[i - 1]) + 1, 0, m)
      })
      return order
    }
  } catch {
    // no storage, or a broken value: the default order
  }
  return [...modules]
}

// Today is the live surface: the day in numbers, today's plan, what got
// finished and the session lanes, as modules. It polls the daemon every 5s.
export function Today() {
  const { data, error, reload } = usePoll(fetchToday, 5000)
  const plans = usePoll(fetchPlans, 30000)
  const [order, setOrder] = useState(savedOrder)
  // draft is the order while a module is dragged: the others move aside as it
  // passes, so it shows where it will land. It is kept only when dropped.
  const [draft, setDraft] = useState<ModuleID[] | null>(null)
  const dragged = useRef<ModuleID | null>(null)
  const shown = draft ?? order

  // passOver moves the dragged module next to the one under the pointer: before it
  // in its top half, after it in its bottom half. Halves keep it steady when the
  // modules differ in height, where taking the other's place would flick back.
  const passOver = (target: ModuleID, y: number, box: DOMRect) => {
    const from = dragged.current
    if (!from || from === target) return
    const after = y >= box.top + box.height / 2
    setDraft((d) => {
      const next = (d ?? order).filter((m) => m !== from)
      next.splice(next.indexOf(target) + (after ? 1 : 0), 0, from)
      return next
    })
  }

  // drop keeps the order the drag shows.
  const drop = () => {
    if (!dragged.current || !draft) return
    setOrder(draft)
    try {
      localStorage.setItem(orderKey, JSON.stringify(draft))
    } catch {
      // the order still applies until the page reloads
    }
  }

  // endDrag forgets the drag; without a drop, the modules go back as they were.
  const endDrag = () => {
    dragged.current = null
    setDraft(null)
  }

  if (error) {
    return <p className="text-dim">Could not reach the daemon. Is `devtopo app` running?</p>
  }
  if (!data) {
    return <p className="text-dim">Loading your day...</p>
  }

  const todays = (plans.data ?? []).filter((p) => dateKey(new Date(p.WindowStart)) === today())
  const fin = finishedToday(data, plans.data ?? [])

  const content: Record<ModuleID, ReactNode> = {
    numbers: (
      <div aria-label="Today in numbers" className="grid gap-2.5 [grid-template-columns:repeat(auto-fit,minmax(150px,1fr))]">
        <Tile label="AI hours" num={data.DayAIHours.toFixed(1)} unit="h" sub={`across ${data.DaySessions} sessions`} color="var(--accent)" />
        <Tile label="Prompts sent" num={data.PromptsToday} sub="to your coding agents" color="var(--accent)" />
        <Tile label="Tickets finished" num={fin.tickets.length} sub="Linear and Jira" color="var(--done)" />
        <Tile label="PR reviews finished" num={fin.reviews} sub="submitted by you" color="var(--waiting)" />
        <Tile label="Other tasks finished" num={fin.own} sub="your own activities" color="var(--done)" />
      </div>
    ),
    yourprs: (
      <YourPRs
        prs={data.YourPRs ?? []}
        plans={todays}
        reload={() => {
          reload()
          plans.reload()
        }}
      />
    ),
    plan: plans.data && (
      <PlanCard
        plans={todays}
        started={data.StartedToday}
        reload={() => {
          reload()
          plans.reload()
        }}
      />
    ),
    finished: (
      <section aria-label="Finished today">
        <h2 className={head} style={{ color: 'var(--done)', borderColor: 'var(--done)' }}>
          Finished today ({fin.tickets.length + fin.other.length})
        </h2>
        <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(300px,1fr))]">
          <FinishedList label="Tickets" rows={fin.tickets} />
          <FinishedList label="Other tasks" rows={fin.other} />
        </div>
      </section>
    ),
    sessions: (
      <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))]">
        <Lane lane={data.Running} />
        <Lane lane={data.Waiting} />
        <Lane lane={data.Idle} />
      </div>
    ),
  }

  return (
    <div className="flex flex-col gap-6">
      {shown.map((id) => (
        <div
          key={id}
          data-module={id}
          data-dragging={draft && dragged.current === id ? 'true' : undefined}
          onDragOver={(e) => {
            if (!dragged.current) return // a chip on its way to the calendar
            e.preventDefault()
            passOver(id, e.clientY, e.currentTarget.getBoundingClientRect())
          }}
          onDrop={(e) => {
            e.preventDefault()
            drop()
          }}
          className={`group flex gap-1 rounded-lg transition-opacity ${draft && dragged.current === id ? 'opacity-40' : ''}`}
        >
          <button
            draggable
            aria-label={`Move ${moduleName[id]}`}
            title="Drag to move"
            onDragStart={(e) => {
              dragged.current = id
              setDraft(order)
              e.dataTransfer?.setData('text/plain', id)
            }}
            onDragEnd={endDrag}
            className="w-4 shrink-0 cursor-grab self-start pt-1 text-dim opacity-0 hover:text-accent focus:opacity-100 group-hover:opacity-100"
          >
            ⠿
          </button>
          <div className="min-w-0 flex-1">{content[id]}</div>
        </div>
      ))}
    </div>
  )
}
