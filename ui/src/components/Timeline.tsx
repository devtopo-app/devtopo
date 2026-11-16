import { Fragment, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import type { Meeting, PlanItem, PlanRow } from '../planner-api'
import { clamp, dateKey, fmtDur, fmtHour, fmtNum, hourWidth, hoursIn, kindOf, pack, snap, SNAP, type Kind } from '../lib/timeline'

const GUT = 176 // row label gutter
const TRACK = 46 // one sub-track
const PAD = 5

// The calendar's look.
const BLOCK = 'blk group absolute z-[11] select-none overflow-hidden rounded-md border border-l-4 pl-2 pt-1 text-fg shadow-sm hover:z-20 hover:shadow-lg'
const BLOCK_TITLE = 'blk-title truncate text-xs font-semibold leading-tight'
const BLOCK_META = 'blk-meta truncate font-mono text-2xs text-dim dark:text-[color:var(--ink)]'
const RULER = 'relative h-[22px] border-b border-border font-mono'
const TICK = 'absolute inset-y-0 border-l border-border pl-1 text-xs text-dim'
const LANE_NAME = 'text-xs font-bold uppercase tracking-wider'
const LANE_SUB = 'pl-1.5 font-mono text-2xs text-dim'

export type Span = { start: number; end: number }

// tone colours a block or chip by kind from the theme tokens, or by color when
// given (a ticket's status, see ticketColor).
export function tone(kind: Kind | 'meeting', color?: string): CSSProperties {
  const c = color ?? { ticket: 'var(--accent)', review: 'var(--waiting)', yourpr: 'var(--tag)', own: 'var(--done)', meeting: 'var(--idle)' }[kind]
  const ink = kind === 'meeting' ? 'var(--fg)' : 'var(--blockInk)'
  return { background: `color-mix(in srgb, ${c} var(--tint), var(--raised))`, borderColor: `color-mix(in srgb, ${c} 60%, var(--raised))`, borderLeftColor: c, '--ink': ink, '--stripe': c } as CSSProperties
}

// inReview tells a started ticket waiting on review or testing from one being
// worked on: the tracker gives both the same stage, so it goes by the name.
const inReview = (state: string) => /review|qa|test|verif|approv/i.test(state)

// ticketColor is a ticket's status colour, for its bar, hours box and pills:
// orange while it is worked on, green in review, teal to do, quiet when it is
// in the backlog, and faded once done or cancelled.
export const ticketColor = (stage: string, state = '') =>
  stage === 'started'
    ? inReview(state)
      ? 'var(--done)'
      : 'var(--waiting)'
    : stage === 'unstarted'
      ? 'var(--accent)'
      : stage === 'completed' || stage === 'canceled'
        ? 'var(--idle)'
        : 'var(--dim)'

// itemTone colours a plan item's card outside the calendar: a ticket by its
// status, the rest by kind. On the calendar every block keeps its kind's colour.
export const itemTone = (it: PlanItem, done = false) =>
  kindOf(it) === 'ticket' ? tone('ticket', ticketColor(done ? 'completed' : it.RefStage, it.RefState)) : tone(kindOf(it))

// Tag is a ticket's status pill, tinted with its status colour.
export function Tag({ text, color }: { text: string; color: string }) {
  return (
    <span
      className="flex-none whitespace-nowrap rounded-full border px-1.5 text-2xs leading-4 text-fg"
      style={{ background: `color-mix(in srgb, ${color} 18%, var(--raised))`, borderColor: `color-mix(in srgb, ${color} 45%, var(--raised))` }}
    >
      {text}
    </span>
  )
}

// Legend names the block colours.
export function Legend() {
  return (
    <div className="mt-4 flex flex-wrap gap-x-4 gap-y-1.5 text-xs text-dim">
      {(['ticket', 'review', 'yourpr', 'own', 'meeting'] as const).map((k) => (
        <span key={k} className="inline-flex items-center gap-1.5">
          <span className="h-3 w-4 rounded-sm border-l-4" style={tone(k)} />
          {{ ticket: 'Ticket', review: 'PR review', yourpr: 'Your PR', own: 'Your own activity', meeting: 'Meeting (calendar)' }[k]}
        </span>
      ))}
    </div>
  )
}

// track follows one pointer drag on the document until the pointer is released.
export function track(move: (e: PointerEvent) => void, up: (e: PointerEvent) => void) {
  const end = (e: PointerEvent) => {
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', end)
    up(e)
  }
  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', end)
}

type Target = { rowId: string; hour: number } | { group: string } | 'anytime' | 'unplan' | null

// dropTarget finds the plan row (and the hour on it), the Not planned list
// (unplan), a row's group of activities or an anytime drop zone under the pointer.
export function dropTarget(x: number, y: number, view: { from: number; to: number }): Target {
  const inside = (r: DOMRect) => x >= r.left && x <= r.right && y >= r.top && y <= r.bottom
  for (const el of document.querySelectorAll<HTMLElement>('[data-row]')) {
    const r = el.getBoundingClientRect()
    if (inside(r)) return { rowId: el.dataset.row!, hour: view.from + ((x - r.left) / r.width) * (view.to - view.from) }
  }
  for (const el of document.querySelectorAll('[data-unplan]')) {
    if (inside(el.getBoundingClientRect())) return 'unplan'
  }
  for (const el of document.querySelectorAll<HTMLElement>('[data-group-row]')) {
    if (inside(el.getBoundingClientRect())) return { group: el.dataset.groupRow! }
  }
  for (const el of document.querySelectorAll('[data-anytime]')) {
    if (inside(el.getBoundingClientRect())) return 'anytime'
  }
  return null
}

export const targetKey = (t: Target) => (!t || typeof t === 'string' ? t : 'group' in t ? `group:${t.group}` : t.rowId)

interface Props {
  date: string
  view: { from: number; to: number }
  win: Span
  rows: PlanRow[]
  items: PlanItem[]
  meetings: Meeting[]
  over: string | null
  editing?: string | null
  focusRow?: string | null
  onOver: (key: string | null) => void
  onMove: (it: PlanItem, rowId: string, start: number | null) => void
  onResize: (it: PlanItem, hours: number) => void
  // The planner's own controls: each one left out is not shown, as on Today.
  onWindowDrag?: (w: Span) => void
  onWindowDrop?: (w: Span) => void
  onRemove?: (it: PlanItem) => void
  onAddActivity?: (rowId: string, start: number) => void
  onEdit?: (id: string) => void
  onRename?: (it: PlanItem, text: string) => void
  onRenameRow?: (row: PlanRow, name: string) => void
  onRemoveRow?: (row: PlanRow) => void
  onAddRow?: () => void
  // A click on a block that was not dragged (the done menu on Today and in the
  // planner), and what counts as done.
  onPick?: (it: PlanItem, e: React.MouseEvent<HTMLElement>) => void
  isDone?: (it: PlanItem) => boolean
  // preview is a card being dragged onto a row: drawn at its own length where it
  // will land, so the drop matches what the user saw.
  preview?: Preview | null
}

export type Preview = { rowId: string; start: number; hours: number; kind: Kind; text: string }

// Timeline is the plan's day: Start/End window handles, an hour ruler, the
// calendar track and the plan rows, all on one hour axis that fills the width.
export function Timeline(p: Props) {
  const { from, to } = p.view
  const scroller = useRef<HTMLDivElement>(null)
  const ruler = useRef<HTMLDivElement>(null)
  const [avail, setAvail] = useState(0)
  const [moving, setMoving] = useState<{ id: string; start: number; dy: number } | null>(null)
  const [sizing, setSizing] = useState<{ id: string; hours: number } | null>(null)
  const [hover, setHover] = useState<string | null>(null)
  // dragged is set by a move or a resize, so the click that ends one picks nothing.
  const dragged = useRef(false)
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(t)
  }, [])
  // The now line shows only on today's plan, within the visible hours.
  const nowH = now.getHours() + now.getMinutes() / 60
  const showNow = dateKey(now) === p.date && nowH >= from && nowH <= to

  useLayoutEffect(() => {
    const el = scroller.current!
    const measure = () => setAvail(el.clientWidth)
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const done = p.isDone ?? ((it: PlanItem) => it.RefStage === 'completed')
  const hw = hourWidth(avail - GUT - 4, from, to)
  const W = (to - from) * hw
  const X = (h: number) => (h - from) * hw
  const hourAt = (clientX: number, el: Element) => from + (clientX - el.getBoundingClientRect().left) / hw

  const hours: number[] = []
  for (let h = Math.ceil(from); h < to; h++) hours.push(h)

  const dragWindow = (e: React.PointerEvent, edge: 'start' | 'end') => {
    e.preventDefault()
    let w = p.win
    track(
      (ev) => {
        const h = snap(hourAt(ev.clientX, ruler.current!))
        w = edge === 'start' ? { ...w, start: clamp(h, from, w.end - 0.5) } : { ...w, end: clamp(h, w.start + 0.5, to) }
        p.onWindowDrag?.(w)
      },
      () => p.onWindowDrop?.(w),
    )
  }

  const dragBlock = (e: React.PointerEvent, it: PlanItem, start: number) => {
    if ((e.target as HTMLElement).closest('button,input,[data-grab]')) return
    e.preventDefault()
    let s = start
    let moved = (dragged.current = false)
    track(
      (ev) => {
        const dx = ev.clientX - e.clientX
        const dy = ev.clientY - e.clientY
        if (!moved && Math.hypot(dx, dy) < 4) return
        moved = dragged.current = true
        s = clamp(snap(start + dx / hw), from, to - it.IntendedHours)
        setMoving({ id: it.ID, start: s, dy })
        p.onOver(targetKey(dropTarget(ev.clientX, ev.clientY, p.view)))
      },
      (ev) => {
        setMoving(null)
        p.onOver(null)
        if (!moved) return
        const t = dropTarget(ev.clientX, ev.clientY, p.view)
        if (t === 'anytime') {
          if (it.RefID) p.onMove(it, '', null)
        } else if (t === 'unplan') {
          if (it.RefID) p.onRemove?.(it)
        } else if (t && 'group' in t) {
          // An activity changes row and keeps its time. Anything else has no row
          // of activities, so it's kept for the day, as on any anytime zone.
          if (it.RefID) p.onMove(it, '', null)
          else p.onMove(it, t.group, start)
        } else if (t) p.onMove(it, t.rowId, s)
      },
    )
  }

  const resize = (e: React.PointerEvent, it: PlanItem, start: number) => {
    e.preventDefault()
    e.stopPropagation()
    dragged.current = true
    const lane = (e.currentTarget as HTMLElement).closest('[data-row]')!
    let h = it.IntendedHours
    track(
      (ev) => {
        h = clamp(snap(hourAt(ev.clientX, lane) - start), SNAP, to - start)
        setSizing({ id: it.ID, hours: h })
      },
      () => {
        setSizing(null)
        p.onResize(it, h)
      },
    )
  }

  // The label floats over the block being hovered, moved or resized. It names the
  // block in full and gives its live times, so the block keeps its real width
  // and its right edge stays its end.
  const active = sizing?.id ?? moving?.id ?? hover
  const label = (start: number, hours: number, t: number, name: string, dy: number, sized: boolean) => {
    const right = X(start) > W / 2 // a block on the right half anchors its label to its end
    return (
      <div
        role="tooltip"
        className="pointer-events-none absolute z-50 flex items-baseline gap-2 whitespace-nowrap rounded-md border border-border bg-raised px-2 py-0.5 text-xs text-fg shadow-lg"
        style={{
          top: PAD + t * TRACK - 8, // clear of the remove button on the block's corner
          transform: `translateY(calc(-100% + ${dy}px))`,
          ...(right ? { right: W - X(start + hours) } : { left: X(start) }),
          maxWidth: Math.max(160, right ? X(start + hours) : W - X(start)),
        }}
      >
        <span className="truncate font-semibold">{name}</span>
        <span className="shrink-0 font-mono text-2xs text-dim">
          {sized ? `until ${fmtHour(start + hours)}` : `${fmtHour(start)}-${fmtHour(start + hours)}`} · {fmtDur(hours)}
        </span>
      </div>
    )
  }

  // block is one placed card, at its own width whether hovered or not.
  const block = (key: string, look: CSSProperties, kind: Kind | 'meeting', start: number, hours: number, t: number, extra: CSSProperties, name: string, children: ReactNode, rest: object = {}) => (
    <Fragment key={key}>
      <div
        {...rest}
        onPointerEnter={() => setHover(key)}
        onPointerLeave={() => setHover((h) => (h === key ? null : h))}
        // z-[11] keeps blocks above the plan window's tint (z-10), not behind it;
        // pr-3 leaves room for the resize grip.
        className={`${BLOCK} pr-3 ${
          hours * hw < 110 ? 'narrow' : ''
        } ${kind === 'meeting' ? 'cursor-default' : 'cursor-grab'}`}
        style={{ left: X(start), width: Math.max(8, hours * hw - 2), top: PAD + t * TRACK, height: TRACK - PAD, touchAction: 'none', ...look, ...extra }}
      >
        {children}
      </div>
      {active === key && label(start, hours, t, name, moving?.id === key ? moving.dy : 0, sizing?.id === key)}
    </Fragment>
  )

  const meetings = (p.meetings ?? []).map((m) => ({ m, start: hoursIn(p.date, m.Start), end: hoursIn(p.date, m.End) }))
  const mpack = pack(meetings)
  const meetingSum = meetings.reduce((s, m) => s + m.end - m.start, 0)

  return (
    <div ref={scroller} className="overflow-x-auto overflow-y-hidden">
      <div style={{ width: GUT + W }}>
        {p.onWindowDrop && (
        <div className="relative h-[30px]" style={{ marginLeft: GUT }}>
          {(['start', 'end'] as const).map((edge) => (
            <div
              key={edge}
              onPointerDown={(e) => dragWindow(e, edge)}
              className={`absolute top-0.5 z-30 flex h-6 cursor-ew-resize select-none items-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-2.5 font-mono text-xs font-bold text-bg shadow ${
                edge === 'start' ? '-translate-x-full rounded-br-none' : 'rounded-bl-none'
              }`}
              style={{ left: X(p.win[edge]), touchAction: 'none' }}
            >
              <small className="text-2xs font-medium uppercase tracking-wider opacity-80">{edge === 'start' ? 'Start' : 'End'}</small>
              {fmtHour(p.win[edge])}
            </div>
          ))}
        </div>
        )}

        <div className="relative">
          <div ref={ruler} className={RULER} style={{ marginLeft: GUT, width: W }}>
            {hours.map((h) => (
              <div key={h} className={TICK} style={{ left: X(h) }}>
                {fmtHour(h)}
              </div>
            ))}
          </div>

          <Lane
            W={W}
            tracks={mpack.count}
            grid={gridLines(from, to, X)}
            label={
              <>
                <span className={`px-1.5 ${LANE_NAME} text-dim`}>Calendar</span>
                <span className={LANE_SUB}>{fmtNum(meetingSum)}h meetings</span>
              </>
            }
          >
            {meetings.map(({ m, start, end }, i) =>
              block(`m${i}`, tone('meeting'), 'meeting', start, end - start, mpack.track[i], {}, m.Title, (
                <>
                  <div className={BLOCK_TITLE}>{m.Title}</div>
                  <div className={BLOCK_META}>
                    {fmtHour(start)}-{fmtHour(end)}
                    {m.Calendar ? ` · ${m.Calendar}` : ''}
                  </div>
                </>
              )),
            )}
          </Lane>

          {p.rows.map((row) => {
            const blocks = p.items
              .filter((it) => it.RowID === row.ID && it.Start !== null)
              .map((it) => {
                const start = moving?.id === it.ID ? moving.start : hoursIn(p.date, it.Start!)
                const hours = sizing?.id === it.ID ? sizing.hours : it.IntendedHours
                return { it, start, hours, end: start + hours }
              })
            const bp = pack(blocks)
            return (
              <Lane
                key={row.ID}
                W={W}
                tracks={bp.count}
                grid={gridLines(from, to, X)}
                over={p.over === row.ID}
                rest={{
                  'data-row': row.ID,
                  'data-testid': `row-${row.ID}`,
                  onDoubleClick: (e: React.MouseEvent) => {
                    if (e.target !== e.currentTarget || !p.onAddActivity) return
                    p.onAddActivity(row.ID, clamp(snap(hourAt(e.clientX, e.currentTarget) - 0.5), from, to - 1))
                  },
                }}
                label={
                  <>
                    {!p.onRenameRow ? (
                      <span className={`truncate px-1.5 ${LANE_NAME} text-fg`}>{row.Name}</span>
                    ) : (
                    <input
                      key={row.Name}
                      aria-label="Row name"
                      defaultValue={row.Name}
                      autoFocus={p.focusRow === row.ID}
                      onFocus={(e) => e.currentTarget.select()}
                      onBlur={(e) => {
                        const v = e.currentTarget.value.trim()
                        if (v && v !== row.Name) p.onRenameRow!(row, v)
                        else e.currentTarget.value = row.Name
                      }}
                      onKeyDown={(e) => {
                        if (e.key === 'Escape') e.currentTarget.value = row.Name
                        if (e.key === 'Enter' || e.key === 'Escape') e.currentTarget.blur()
                      }}
                      className={`w-full rounded border border-transparent bg-transparent px-1 py-0.5 ${LANE_NAME} text-fg hover:border-border focus:border-border focus:bg-bg dark:focus:bg-raised focus:outline-none`}
                    />
                    )}
                    <span className={`flex items-center gap-2 ${LANE_SUB}`}>
                      {fmtNum(blocks.reduce((s, b) => s + b.hours, 0))}h
                      {p.onRemoveRow && p.rows.length > 1 && (
                        <button
                          aria-label="Remove row"
                          onClick={() => p.onRemoveRow!(row)}
                          className="font-sans underline opacity-0 hover:text-fg group-hover/lane:opacity-100"
                        >
                          remove
                        </button>
                      )}
                    </span>
                  </>
                }
              >
                {!blocks.length && (
                  <div className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-xs italic text-dim">
                    drop cards here{p.onAddActivity && ', or double-click to add an activity'}
                  </div>
                )}
                {p.preview?.rowId === row.ID && (
                  <div
                    data-testid="drop-preview"
                    className="pointer-events-none absolute z-30 overflow-hidden rounded-md border-2 border-dashed px-2 pt-1 text-xs font-semibold text-fg shadow-lg"
                    style={{ left: X(p.preview.start), width: Math.max(8, p.preview.hours * hw - 2), top: PAD, height: TRACK - PAD, ...tone(p.preview.kind) }}
                  >
                    <div className="truncate">{p.preview.text}</div>
                    <div className="truncate font-mono text-2xs font-normal text-dim">
                      {fmtHour(p.preview.start)}-{fmtHour(p.preview.start + p.preview.hours)}
                    </div>
                  </div>
                )}
                {blocks.map(({ it, start, hours }, i) => (
                  <Fragment key={it.ID}>
                  {block(
                    it.ID,
                    tone(kindOf(it)),
                    kindOf(it),
                    start,
                    hours,
                    bp.track[i],
                    moving?.id === it.ID ? { transform: `translateY(${moving.dy}px)`, zIndex: 40, cursor: 'grabbing' } : {},
                    kindOf(it) === 'ticket' && it.RefRepo ? `${it.RefRepo} ${it.Text}` : it.Text,
                    <>
                      {p.editing === it.ID ? (
                        <input
                          autoFocus
                          defaultValue={it.Text}
                          aria-label="Activity name"
                          onFocus={(e) => e.currentTarget.select()}
                          onBlur={(e) => p.onRename?.(it, e.currentTarget.value.trim())}
                          onKeyDown={(e) => {
                            if (e.key === 'Escape') e.currentTarget.value = it.Text
                            if (e.key === 'Enter' || e.key === 'Escape') e.currentTarget.blur()
                          }}
                          className="w-full rounded border border-border bg-bg dark:bg-raised px-1 text-xs font-semibold"
                        />
                      ) : (
                        <div className={`${BLOCK_TITLE} ${done(it) ? 'line-through opacity-60' : ''}`}>
                          {done(it) && '✓ '}
                          {kindOf(it) === 'ticket' && it.RefRepo && <span className="font-mono text-dim dark:text-[color:var(--ink)]">{it.RefRepo} </span>}
                          {it.Text}
                        </div>
                      )}
                      <div className={BLOCK_META}>
                        {fmtHour(start)}-{fmtHour(start + hours)} · {fmtDur(hours)}
                      </div>
                      {/* The grip shows on hover, and stays while resizing, at the block's end. */}
                      <div data-grab onPointerDown={(e) => resize(e, it, start)} className="absolute inset-y-0 right-0 flex w-2.5 cursor-ew-resize items-center justify-center">
                        <div
                          className={`h-1/2 w-[3px] rounded-full group-hover:opacity-100 ${sizing?.id === it.ID ? 'opacity-100' : 'opacity-0'}`}
                          style={{ background: 'var(--stripe)' }}
                        />
                      </div>
                    </>,
                    {
                      role: p.onPick && 'button',
                      onPointerDown: (e: React.PointerEvent) => dragBlock(e, it, start),
                      onClick: (e: React.MouseEvent<HTMLElement>) => !dragged.current && p.editing !== it.ID && p.onPick?.(it, e),
                      onDoubleClick: (e: React.MouseEvent) => {
                        e.stopPropagation()
                        if (!it.RefID) p.onEdit?.(it.ID)
                      },
                    },
                  )}
                  {/* On the block's top right corner, outside it: a short block has no
                      room for it. Hovering it keeps the block hovered. */}
                  {p.onRemove && (
                    <button
                      aria-label={it.RefID ? `Send ${it.Text} back` : `Delete ${it.Text}`}
                      title={it.RefID ? 'back to tray' : 'delete'}
                      onClick={() => p.onRemove!(it)}
                      onPointerEnter={() => setHover(it.ID)}
                      onPointerLeave={() => setHover((h) => (h === it.ID ? null : h))}
                      className={`absolute z-50 h-4 w-4 items-center justify-center rounded-full border border-border bg-bg dark:bg-raised text-xs leading-none text-dim shadow hover:text-fg ${
                        hover === it.ID && !moving && !sizing ? 'flex' : 'hidden'
                      }`}
                      style={{ left: X(start + hours) - 10, top: PAD + bp.track[i] * TRACK - 6 }}
                    >
                      ×
                    </button>
                  )}
                  </Fragment>
                ))}
              </Lane>
            )
          })}

          {showNow && <NowLine left={GUT + X(nowH) - 1} hour={nowH} />}
          <PlanWindow left={GUT + X(p.win.start)} width={(p.win.end - p.win.start) * hw} />
        </div>

        {p.onAddRow && (
          <button onClick={p.onAddRow} className="mt-2 rounded-lg border border-border bg-bg dark:bg-raised px-2.5 py-1 text-sm hover:text-accent" style={{ marginLeft: GUT }}>
            + Add row
          </button>
        )}
      </div>
    </div>
  )
}

// gridLines marks every half hour inside a lane, at left(h).
function gridLines(from: number, to: number, left: (h: number) => number) {
  const out: ReactNode[] = []
  for (let h = from + 0.5; h < to; h += 0.5) {
    out.push(<div key={h} className={`pointer-events-none absolute inset-y-0 w-px bg-border ${h % 1 ? 'opacity-50' : ''}`} style={{ left: left(h) }} />)
  }
  return out
}

// NowLine marks the current time across the lanes.
function NowLine({ left, hour }: { left: number; hour: number }) {
  return (
    <div data-testid="now-line" className="pointer-events-none absolute inset-y-0 z-30 w-0.5" style={{ left, background: 'var(--waiting)' }}>
      <span className="absolute -top-0.5 left-1 whitespace-nowrap rounded bg-[var(--waiting)] px-1 font-mono text-2xs font-bold text-bg">{fmtHour(hour)}</span>
    </div>
  )
}

// PlanWindow edges and tints the plan's Start-End window across the lanes.
function PlanWindow({ left, width }: { left: number; width: number }) {
  return (
    <div
      className="pointer-events-none absolute inset-y-0 z-10 border-x-2 border-accent"
      style={{ left, width, background: 'color-mix(in srgb, var(--accent) 3%, transparent)' }}
    />
  )
}

// Lane is one track of blocks with its label in the gutter.
function Lane(props: { W: number; tracks: number; grid: ReactNode; label: ReactNode; over?: boolean; rest?: object; children: ReactNode }) {
  return (
    <div
      {...props.rest}
      className={`group/lane relative mt-2 rounded-lg border border-border bg-bg dark:bg-raised ${props.over ? 'outline-dashed outline-2 -outline-offset-2 outline-accent' : ''}`}
      style={{ marginLeft: GUT, width: props.W, height: PAD + props.tracks * TRACK }}
    >
      {props.grid}
      <div className="absolute top-0 flex h-full flex-col justify-center gap-0.5 pr-3" style={{ left: -GUT, width: GUT }}>
        {props.label}
      </div>
      {props.children}
    </div>
  )
}
