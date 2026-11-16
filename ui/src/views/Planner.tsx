import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  fetchPlans,
  fetchPlannable,
  refreshConnectors,
  createPlan,
  updatePlan,
  deletePlan,
  addRow,
  renameRow,
  deleteRow,
  addItem,
  updateItem,
  deleteItem,
  markItemDone,
  markTicketDone,
  type Plan,
  type PlanItem,
  type Plannable,
  type ItemBody,
  setReviewHidden,
  setOwnPRDone,
} from '../planner-api'
import { ago, atHour, capacity, codeHostName, dateKey, dayLabel, dropStart, fmtHour, fmtNum, hoursIn, isDone, kindOf, newPlanDate, planName, reviewURL, trays, type Kind } from '../lib/timeline'
import { Legend, Tag, Timeline, dropTarget, targetKey, ticketColor, tone, track, type Preview, type Span } from '../components/Timeline'
import { MarkDone, Menu, tracker } from '../components/Menu'
import { ReviewBadge, reviewLabel } from '../components/ReviewBadge'

const dateOf = (p: Plan) => dateKey(new Date(p.WindowStart))
const windowOf = (p: Plan): Span => ({ start: hoursIn(dateOf(p), p.WindowStart), end: hoursIn(dateOf(p), p.WindowEnd) })

export function Planner() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [plannable, setPlannable] = useState<Plannable[]>([])
  // Today's "Edit in Planner" link names the plan to open: #/planner?plan=<id>.
  const [currentID, setCurrentID] = useState(() => new URLSearchParams(window.location.hash.split('?')[1]).get('plan') ?? '')
  const [view, setView] = useState({ from: 7, to: 20 })
  const [draftWin, setDraftWin] = useState<Span | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [armed, setArmed] = useState(false)
  const [over, setOver] = useState<string | null>(null)
  const [ghost, setGhost] = useState<{ x: number; y: number; text: string; kind: Kind } | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [focusRow, setFocusRow] = useState<string | null>(null)
  const [adding, setAdding] = useState<string | null>(null)
  const [menu, setMenu] = useState<Picked | null>(null)
  const [showHidden, setShowHidden] = useState(false)
  const [showDone, setShowDone] = useState(false)
  // renamingItem is the activity whose name is being edited in the tray.
  const [renamingItem, setRenamingItem] = useState<string | null>(null)

  const refresh = async () => {
    setPlans(await fetchPlans())
    setPlannable(await fetchPlannable())
  }
  useEffect(() => {
    // Show whatever we already have at once, then sync the connectors and
    // refetch so the trays and capacity are fresh on every open.
    refresh().catch(() => {})
    refreshConnectors()
      .then(refresh)
      .catch(() => {})
  }, [])

  const plan = plans.find((p) => p.ID === currentID) ?? plans[0]
  useEffect(() => {
    setArmed(false)
  }, [plan?.ID, plan?.Name])

  const run = (p: Promise<unknown>) => p.then(refresh).catch(() => {})

  const newPlan = () => {
    const date = newPlanDate(plans.map(dateOf), new Date())
    run(
      createPlan(planName(date), atHour(date, 9).toISOString(), atHour(date, 18).toISOString()).then((p) => setCurrentID(p.ID)),
    )
  }

  const tabs = (
    <nav role="tablist" className="mb-3 flex flex-wrap items-stretch gap-1.5">
      {plans.map((p) => {
        const cls = `max-w-[260px] rounded-lg border px-3 py-1.5 text-left ${p === plan ? 'border-accent bg-bg dark:bg-raised shadow-[inset_0_-3px_0_var(--accent)]' : 'border-border bg-panel'}`
        const when = (
          <span className="block font-mono text-xs text-dim">
            {dayLabel(dateOf(p))} · {fmtHour(windowOf(p).start)}-{fmtHour(windowOf(p).end)}
          </span>
        )
        // A click on the selected tab renames its plan; an input cannot sit
        // inside a button, so the tab is a plain element while it is edited.
        return renaming && p === plan ? (
          <div key={p.ID} role="tab" aria-selected className={cls}>
            <input
              autoFocus
              aria-label="Plan name"
              defaultValue={p.Name}
              onBlur={(e) => {
                const n = e.target.value.trim()
                setRenaming(false)
                if (n && n !== p.Name) run(updatePlan(p.ID, n, p.WindowStart, p.WindowEnd))
              }}
              onKeyDown={(e) => {
                if (e.key === 'Escape') e.currentTarget.value = p.Name
                if (e.key === 'Enter' || e.key === 'Escape') e.currentTarget.blur()
              }}
              className="block w-full min-w-0 rounded border border-border bg-bg dark:bg-raised px-1 text-sm font-semibold focus:outline-none"
            />
            {when}
          </div>
        ) : (
          <button key={p.ID} role="tab" aria-selected={p === plan} onClick={() => (p === plan ? setRenaming(true) : setCurrentID(p.ID))} className={cls}>
            <b className="block truncate text-sm font-semibold">{p.Name}</b>
            {when}
          </button>
        )
      })}
      <button onClick={newPlan} className="flex items-center rounded-lg border border-dashed border-border bg-panel px-3 py-1.5 font-semibold text-accent">
        + New plan
      </button>
    </nav>
  )

  if (!plan) {
    return (
      <div>
        <h1 className="mb-3 text-xl font-semibold">Planner</h1>
        {tabs}
        <p className="text-dim">Nothing planned yet. Start a new plan to lay out a day.</p>
      </div>
    )
  }

  const date = dateOf(plan)
  const win = draftWin ?? windowOf(plan)
  const items = plan.Items ?? []
  const iso = (d: string, h: number) => atHour(d, h).toISOString()
  const body = (it: PlanItem, patch: Partial<ItemBody>): ItemBody => ({
    rowId: it.RowID,
    text: it.Text,
    start: it.Start,
    intendedHours: it.IntendedHours,
    ...patch,
  })
  const savePlan = (n: string, d: string, w: Span) => run(updatePlan(plan.ID, n, iso(d, w.start), iso(d, w.end)))

  // Moving the plan to another day: the server takes its placed blocks along.
  // A date input emits half-typed years ("0002-...") while typing; skip those.
  // A plan still named after its day is renamed after the new one.
  const moveDate = (d: string) => d >= '2000' && savePlan(plan.Name === planName(date) ? planName(d) : plan.Name, d, win)

  const t = trays(plannable, items)
  // Activities: the user's own tasks by row (one whose row is gone shows under
  // the first).
  const rows = plan.Rows ?? []
  const rowOf = (it: PlanItem) => (rows.some((r) => r.ID === it.RowID) ? it.RowID : rows[0]?.ID)
  const own = items.filter((it) => !it.RefID)
  // moveOwn moves an activity to a row, and to a time when given one. Dropped on
  // an anytime zone (no row) or on its own row without a time, it stays put.
  const moveOwn = (it: PlanItem) => (rowId: string, start: string | null) =>
    !rowId || (start === null && rowOf(it) === rowId) ? undefined : updateItem(it.ID, body(it, start === null ? { rowId } : { rowId, start }))
  const cap = capacity(
    win,
    (plan.Meetings ?? []).map((m) => ({ start: hoursIn(date, m.Start), end: hoursIn(date, m.End) })),
    items.filter((it) => it.Start).map((it) => ({ start: hoursIn(date, it.Start!), hours: it.IntendedHours })),
    t.anytime.reduce((s, it) => s + it.IntendedHours, 0),
  )

  // Anything clicked in a tray or on the timeline opens its menu. An activity or
  // a planned review is done here, on its item; a ticket is done in its tracker.
  const pickOpen = (p: Plannable, rect: DOMRect) =>
    setMenu({ source: p.RefSource, id: p.RefID, key: p.Repo, title: p.Text, state: p.State, stage: p.Stage, project: p.Project, url: p.URL, rect })
  // pickOwn opens one of the user's PRs from the tray, which is marked done on the
  // PR, as it is once planned.
  const pickOwn = (p: Plannable, rect: DOMRect) =>
    setMenu({ source: p.RefSource, id: p.RefID, key: p.Repo, title: p.Text, state: p.Done ? 'Done' : reviewLabel(p.State), stage: p.Done ? 'completed' : '', project: '', url: p.URL, ownPR: { url: p.URL, open: true }, rect })
  const pickItem = (it: PlanItem, rect: DOMRect) => {
    const kind = kindOf(it)
    setMenu({
      source: it.RefSource,
      id: it.RefID,
      key: it.RefRepo,
      title: it.Text,
      ...(kind === 'ticket' ? { state: it.RefState, stage: it.RefStage } : { ...markedDone(it), itemID: it.ID }),
      ...(kind === 'yourpr' && { state: isDone(it) ? 'Done' : reviewLabel(it.RefState), ownPR: { url: reviewURL(it), open: !!it.RefURL } }),
      project: it.RefProject,
      url: kind === 'review' || kind === 'yourpr' ? reviewURL(it) : it.RefURL,
      rect,
    })
  }

  // dragChip drags a tray card onto a row (placing it at the pointer), onto an
  // anytime drop zone (start null), or onto Not planned (unplan, if given). A
  // press without a move is a click: it picks the card, if pick is given, saying
  // which part of the card was clicked.
  const dragChip = (
    e: React.PointerEvent,
    text: string,
    kind: Kind,
    hours: number,
    place: (rowId: string, start: string | null) => Promise<unknown> | void,
    unplan?: () => Promise<unknown>,
    pick?: (rect: DOMRect, target: HTMLElement) => void,
  ) => {
    e.preventDefault()
    const rect = e.currentTarget.getBoundingClientRect()
    const clicked = e.target as HTMLElement
    let moved = false
    track(
      (ev) => {
        if (!moved && Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 5) return
        moved = true
        const target = dropTarget(ev.clientX, ev.clientY, view)
        // Over a row the card shows at its own length where it will land; elsewhere it floats.
        const onRow = target && typeof target === 'object' && 'rowId' in target
        setPreview(onRow ? { rowId: target.rowId, start: dropStart(target.hour, hours, view), hours, kind, text } : null)
        setGhost(onRow ? null : { x: ev.clientX, y: ev.clientY, text, kind })
        setOver(targetKey(target))
      },
      (ev) => {
        setGhost(null)
        setPreview(null)
        setOver(null)
        if (!moved) return pick?.(rect, clicked)
        const target = dropTarget(ev.clientX, ev.clientY, view)
        const placed =
          target === 'anytime'
            ? place('', null)
            : target === 'unplan'
            ? unplan?.()
            : target && 'group' in target
            ? place(kind === 'own' ? target.group : '', null)
            : target && place(target.rowId, iso(date, dropStart(target.hour, hours, view)))
        if (placed) run(placed)
      },
    )
  }

  const hourOptions = (lo: number, hi: number) =>
    Array.from({ length: hi - lo + 1 }, (_, i) => (
      <option key={i} value={lo + i}>
        {fmtHour(lo + i)}
      </option>
    ))

  return (
    <div>
      <h1 className="mb-3 text-xl font-semibold">Planner</h1>
      {tabs}

      <div className="mb-3 flex flex-wrap items-center gap-x-3.5 gap-y-2.5 rounded-xl border border-border bg-panel px-3.5 py-2.5 shadow-sm">
        <input
          type="date"
          aria-label="Plan date"
          value={date}
          onChange={(e) => e.target.value && moveDate(e.target.value)}
          className="rounded-md border border-border bg-bg dark:bg-raised px-1.5 py-1"
        />
        <label className="flex items-center gap-1.5 text-xs text-dim">
          View
          <select aria-label="View from" value={view.from} onChange={(e) => setView({ ...view, from: +e.target.value })} className="rounded-md border border-border bg-bg dark:bg-raised px-1.5 py-1 text-fg">
            {hourOptions(0, 12)}
          </select>
          to
          <select aria-label="View to" value={view.to} onChange={(e) => setView({ ...view, to: +e.target.value })} className="rounded-md border border-border bg-bg dark:bg-raised px-1.5 py-1 text-fg">
            {hourOptions(14, 24)}
          </select>
        </label>
        <div className="flex min-w-0 flex-[1_1_360px] items-center gap-2.5">
          <div className="relative h-2.5 flex-[1_1_140px] overflow-hidden rounded-full border border-border bg-bg dark:bg-raised">
            <div className="absolute inset-y-0 left-0 bg-accent" style={{ width: `${cap.free ? Math.min(100, (cap.planned / cap.free) * 100) : 0}%` }} />
          </div>
          <span className="whitespace-nowrap text-sm text-dim">
            <b className="text-fg">{fmtNum(cap.planned)}h</b> planned of <b className="text-fg">{fmtNum(cap.free)}h</b> free ({fmtNum(cap.hours)}h window - {fmtNum(cap.meetings)}h meetings)
          </span>
        </div>
        <button
          onClick={() => (armed ? run(deletePlan(plan.ID)) : setArmed(true))}
          className="rounded-lg border border-border px-2.5 py-1 text-sm hover:border-[var(--waiting)] hover:text-[var(--waiting)]"
        >
          {armed ? 'Click again to delete' : 'Delete plan'}
        </button>
      </div>

      <section className="rounded-xl border border-border bg-panel px-3 pb-3 pt-2.5 shadow-sm">
        <Timeline
          date={date}
          view={view}
          win={win}
          rows={plan.Rows ?? []}
          items={items}
          meetings={plan.Meetings ?? []}
          over={over}
          editing={editing}
          focusRow={focusRow}
          onOver={setOver}
          onWindowDrag={setDraftWin}
          onWindowDrop={(w) => savePlan(plan.Name, date, w).then(() => setDraftWin(null))}
          onMove={(it, rowId, start) => run(updateItem(it.ID, body(it, { rowId, start: start === null ? null : iso(date, start) })))}
          onResize={(it, hours) => run(updateItem(it.ID, body(it, { intendedHours: hours })))}
          onRemove={(it) => run(deleteItem(it.ID))}
          onAddActivity={(rowId, start) =>
            run(addItem(plan.ID, { rowId, text: 'New activity', start: iso(date, start), intendedHours: 1 }).then((it) => setEditing(it.ID)))
          }
          isDone={isDone}
          preview={preview}
          // A double-click's first clicks open the menu; the rename takes over.
          onEdit={(id) => {
            setMenu(null)
            setEditing(id)
          }}
          onPick={(it, e) => pickItem(it, e.currentTarget.getBoundingClientRect())}
          onRename={(it, text) => {
            setEditing(null)
            if (text && text !== it.Text) run(updateItem(it.ID, body(it, { text })))
          }}
          onRenameRow={(row, n) => run(renameRow(row.ID, n))}
          onRemoveRow={(row) => run(deleteRow(row.ID))}
          onAddRow={() => run(addRow(plan.ID, 'New row').then((r) => setFocusRow(r.ID)))}
        />
      </section>

      <div className="mt-3 grid gap-3 lg:grid-cols-3">
        <Tray
          label="Reviews"
          color="var(--waiting)"
          count={`${t.reviews.length} waiting, ${t.plannedReviews.length} planned${t.yourPRs.length ? `, ${t.yourPRs.length} on your PRs` : ''}`}
          sub="PRs waiting on you, estimated from their size - pull the edge to resize."
          footer={
            <>
              {t.hiddenReviews.length > 0 && (
                <div className="mt-2">
                  <button type="button" onClick={() => setShowHidden((s) => !s)} className="text-xs text-dim hover:text-fg">
                    {showHidden ? 'Hide' : 'Show'} hidden ({t.hiddenReviews.length})
                  </button>
                  {showHidden && (
                    <div className="mt-2 flex flex-col gap-2">
                      {t.hiddenReviews.map((p) => (
                        <Chip key={p.RefID} kind="review" text={p.Text} meta={ticketMeta(p.Repo, p.RefSource)} hours={p.Hours} lines={linesOf(p)} action={{ label: 'Unhide', onClick: () => run(setReviewHidden(p.RefID, false)) }} onClick={(r) => pickOpen(p, r)} />
                      ))}
                    </div>
                  )}
                </div>
              )}
              {t.doneYourPRs.length > 0 && (
                <div className="mt-2">
                  <button type="button" onClick={() => setShowDone((s) => !s)} className="text-xs text-dim hover:text-fg">
                    {showDone ? 'Hide' : 'Show'} done ({t.doneYourPRs.length})
                  </button>
                  {showDone && (
                    <div className="mt-2 flex flex-col gap-2">
                      {t.doneYourPRs.map((p) => (
                        <Chip key={p.RefID} kind="yourpr" text={p.Text} meta={ownMeta(p)} hours={p.Hours} done onClick={(r) => pickOwn(p, r)} />
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          }
        >
          {[
            // Like tickets: In this plan keeps a review for the day without a
            // time, and Not planned takes one back out.
            <Group key="planned" label="In this plan" count={t.plannedReviews.length} anytime over={over === 'anytime'}>
              {t.plannedReviews.length === 0 && <p className="p-1 text-xs italic text-dim">drag a review here to plan it for today without a time</p>}
              {t.plannedReviews.map((item) => (
                <Chip key={item.ID} kind={kindOf(item)} text={item.Text} meta={ticketMeta(item.RefRepo, item.RefSource)} hours={item.IntendedHours} {...doneMark(item)}
                  badge={kindOf(item) === 'yourpr' && !isDone(item) && <ReviewBadge state={item.RefState} />}
                  planned={item.Start ? `${fmtHour(hoursIn(date, item.Start))}-${fmtHour(hoursIn(date, item.Start) + item.IntendedHours)}` : 'anytime'}
                  onPointerDown={(e) => dragChip(e, item.Text, kindOf(item), item.IntendedHours,
                    (rowId, start) => (start === null && !item.Start ? undefined : updateItem(item.ID, body(item, { rowId, start }))),
                    () => deleteItem(item.ID), (r) => pickItem(item, r))}
                />
              ))}
            </Group>,
            <Group key="open" label="Not planned" count={t.reviews.length} unplan over={over === 'unplan'}>
              {t.reviews.length === 0 && <p className="p-1 text-xs italic text-dim">{t.hiddenReviews.length > 0 ? 'all hidden' : 'drag a planned review here to take it out of the plan'}</p>}
              {t.reviews.map((p) => (
                <Chip key={p.RefID} kind="review" text={p.Text} meta={ticketMeta(p.Repo, p.RefSource)} hours={p.Hours} lines={linesOf(p)}
                  action={{ label: 'Hide', onClick: () => run(setReviewHidden(p.RefID, true)) }}
                  onPointerDown={(e) => dragChip(e, p.Text, 'review', p.Hours, (rowId, start) =>
                    addItem(plan.ID, { rowId, refSource: p.RefSource, refId: p.RefID, text: p.Text, start, intendedHours: p.Hours }), undefined, (r) => pickOpen(p, r))}
                />
              ))}
            </Group>,
            <Group key="yours" label="Your PRs" count={t.yourPRs.length} unplan over={over === 'unplan'}>
              {t.yourPRs.length === 0 && <p className="p-1 text-xs italic text-dim">none of your PRs have a review waiting on you</p>}
              {t.yourPRs.map((p) => (
                <Chip key={p.RefID} kind="yourpr" text={p.Text} meta={ownMeta(p)} hours={p.Hours} badge={<ReviewBadge state={p.State} />}
                  onPointerDown={(e) => dragChip(e, p.Text, 'yourpr', p.Hours, (rowId, start) =>
                    addItem(plan.ID, { rowId, refSource: p.RefSource, refId: p.RefID, text: p.Text, start, intendedHours: p.Hours }), undefined, (r) => pickOwn(p, r))}
                />
              ))}
            </Group>,
          ]}
        </Tray>
        <Tray label="Tickets" color="var(--accent)" count={`${t.tickets.length} open, ${t.plannedTickets.length} planned`} sub="Your Linear and Jira issues, in progress first. Block length = story points (1 pt = 1h).">
          {[
            // In this plan is a drop target: a ticket dropped here is planned for
            // the day without a time.
            <Group key="planned" label="In this plan" count={t.plannedTickets.length} anytime over={over === 'anytime'}>
              {t.plannedTickets.length === 0 && <p className="p-1 text-xs italic text-dim">drag a ticket here to plan it for today without a time</p>}
              {t.plannedTickets.map((item) => (
                  <Chip key={item.ID} kind="ticket" text={item.Text} meta={ticketMeta(item.RefRepo, item.RefProject, item.RefSource)} hours={item.IntendedHours} labels={item.RefLabels}
                    state={finished[item.RefStage] ?? item.RefState} stage={item.RefStage} done={item.RefStage in finished}
                    planned={item.Start ? `${fmtHour(hoursIn(date, item.Start))}-${fmtHour(hoursIn(date, item.Start) + item.IntendedHours)}` : 'anytime'}
                    // Moves the planned ticket rather than adding it twice: onto a
                    // row, to anytime, or back out of the plan via Not planned.
                    onPointerDown={(e) => dragChip(e, item.Text, 'ticket', item.IntendedHours,
                      (rowId, start) => (start === null && !item.Start ? undefined : updateItem(item.ID, body(item, { rowId, start }))),
                      () => deleteItem(item.ID), (r) => pickItem(item, r))}
                  />
                ))}
            </Group>,
            // Not planned takes a planned ticket back out of the plan.
            <Group key="open" label="Not planned" count={t.tickets.length} unplan over={over === 'unplan'}>
                {t.tickets.length === 0 && <p className="p-1 text-xs italic text-dim">drag a planned ticket here to take it out of the plan</p>}
                {t.tickets.map((p) => (
                  <Chip key={`${p.RefSource}/${p.RefID}`} kind="ticket" text={p.Text} meta={ticketMeta(p.Repo, p.Project, p.RefSource)} hours={p.Hours} state={p.State} stage={p.Stage} labels={p.Labels}
                    onPointerDown={(e) => dragChip(e, p.Text, 'ticket', p.Hours, (rowId, start) =>
                      addItem(plan.ID, { rowId, refSource: p.RefSource, refId: p.RefID, text: p.Text, start, intendedHours: p.Hours }), undefined, (r) => pickOpen(p, r))}
                  />
                ))}
            </Group>,
          ]}
        </Tray>
        <Tray label="Activities" anytime over={over === 'anytime'} count={`${own.length}`}
          sub="Your own tasks, by row. + adds one for the day without a time; drag it to another row to move it, or onto the timeline to give it a time. Click its name to rename it, or the rest of it to mark it done.">
          {[
            ...rows.map((row) => {
              const mine = own.filter((it) => rowOf(it) === row.ID)
              return (
                <Group key={row.ID} label={row.Name} count={mine.length} groupRow={row.ID} over={over === `group:${row.ID}`}
                  action={<button aria-label={`Add a task to ${row.Name}`} onClick={() => setAdding(row.ID)} className="rounded border border-border px-1.5 leading-4 text-accent hover:bg-bg dark:hover:bg-raised">+</button>}>
                  {mine.map((it) => (
                    <Chip key={it.ID} kind="own" text={it.Text} hours={it.IntendedHours} {...doneMark(it)}
                      planned={it.Start ? `${fmtHour(hoursIn(date, it.Start))}-${fmtHour(hoursIn(date, it.Start) + it.IntendedHours)}` : 'anytime'}
                      onRemove={() => run(deleteItem(it.ID))}
                      rename={
                        renamingItem === it.ID
                          ? {
                              onSave: (text) => {
                                setRenamingItem(null)
                                if (text && text !== it.Text) run(updateItem(it.ID, body(it, { text })))
                              },
                              onCancel: () => setRenamingItem(null),
                            }
                          : undefined
                      }
                      // A click on the name renames it; anywhere else opens its menu.
                      onPointerDown={(e) =>
                        dragChip(e, it.Text, 'own', it.IntendedHours, moveOwn(it), undefined, (r, target) =>
                          target.closest('[data-name]') ? setRenamingItem(it.ID) : pickItem(it, r),
                        )
                      } />
                  ))}
                  {adding === row.ID && (
                    <input
                      autoFocus
                      aria-label={`New task in ${row.Name}`}
                      placeholder="New task, Enter to add"
                      onBlur={() => setAdding(null)}
                      onKeyDown={(e) => {
                        const text = e.currentTarget.value.trim()
                        if (e.key === 'Enter' && text) run(addItem(plan.ID, { rowId: row.ID, text, start: null, intendedHours: 1 }))
                        if (e.key === 'Enter' || e.key === 'Escape') setAdding(null)
                      }}
                      className="rounded-lg border border-border bg-bg dark:bg-raised px-2.5 py-1.5 text-sm"
                    />
                  )}
                </Group>
              )
            }),
          ]}
        </Tray>
      </div>

      <Legend />

      {menu && (
        <TicketMenu
          key={menu.itemID ?? `${menu.source}/${menu.id}`}
          t={menu}
          onClose={() => setMenu(null)}
          onSaved={() => {
            setMenu(null)
            refresh().catch(() => {})
          }}
        />
      )}

      {ghost && (
        <div data-testid="drag-ghost" className="pointer-events-none fixed z-50 max-w-[260px] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-l-4 border-border px-2.5 py-1.5 text-sm shadow-xl" style={{ left: ghost.x, top: ghost.y, ...tone(ghost.kind) }}>
          {ghost.text}
        </div>
      )}
    </div>
  )
}

// linesOf is a review's size, when it's known.
const linesOf = (p: Plannable) => (p.Sized ? { added: p.Additions ?? 0, removed: p.Deletions ?? 0 } : undefined)

function Tray(props: { label: string; color?: string; count: string; sub: string; anytime?: boolean; over?: boolean; footer?: ReactNode; children: ReactNode[] }) {
  return (
    <section
      aria-label={props.label}
      data-anytime={props.anytime || undefined}
      className={`min-w-0 rounded-xl border border-border bg-panel p-3 shadow-sm ${props.over ? 'outline-dashed outline-2 -outline-offset-[3px] outline-accent' : ''}`}
    >
      <h2 className="mb-0.5 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider">
        {props.color && <span className="h-2 w-2 rounded-sm" style={{ background: props.color }} />}
        {props.label} <span className="font-normal normal-case tracking-normal text-dim">{props.count}</span>
      </h2>
      <p className="mb-2.5 text-xs text-dim">{props.sub}</p>
      <div className="flex min-h-10 flex-col gap-2">
        {props.children.length ? props.children : <p className="p-1 text-xs italic text-dim">{props.anytime ? 'drop a card here to keep it for the day without a time' : 'all placed in this plan'}</p>}
      </div>
      {props.footer}
    </section>
  )
}

function Group(props: { label: string; count: number; action?: ReactNode; anytime?: boolean; unplan?: boolean; groupRow?: string; over?: boolean; children: ReactNode }) {
  const drop = props.anytime || props.unplan
  return (
    <div
      role="group"
      aria-label={props.label}
      data-anytime={props.anytime || undefined}
      data-unplan={props.unplan || undefined}
      data-group-row={props.groupRow}
      className={`flex flex-col gap-2 rounded-lg ${drop ? 'border border-dashed border-border p-1.5' : ''} ${props.over ? 'outline-dashed outline-2 -outline-offset-2 outline-accent' : ''}`}
    >
      <h3 className="flex items-center gap-1.5 text-xs font-semibold text-dim">
        {props.label} <span className="font-normal">{props.count}</span>
        {props.action && <span className="ml-auto">{props.action}</span>}
      </h3>
      {props.children}
    </div>
  )
}

const ticketMeta = (...parts: string[]) => parts.filter(Boolean).join(' · ')

// ownMeta says where one of the user's PRs lives and who reviewed it when.
const ownMeta = (p: Plannable) => ticketMeta(p.Repo, p.RefSource, p.Reviewer ?? '', p.ReviewedAt ? ago(p.ReviewedAt, new Date()) : '')

// finished names the pill of a planned ticket finished since it was planned.
const finished: Record<string, string> = { completed: 'Done', canceled: 'Canceled' }

// markedDone is the state and stage of an activity or planned review, which
// have none of their own until marked done.
const markedDone = (it: PlanItem) => (isDone(it) ? { state: 'Done', stage: 'completed' } : { state: it.RefState, stage: '' })

// doneMark is what a tray card shows for one marked done.
const doneMark = (it: PlanItem) => (isDone(it) ? { ...markedDone(it), done: true } : {})

// Picked is what the menu acts on, open in a tray or in the plan. itemID is set
// on an activity or planned review, which are marked done on their plan item.
// ownPR is set on one of the user's PRs, marked done on the PR while it is open.
type Picked = { source: string; id: string; key: string; title: string; state: string; stage: string; project: string; url: string; itemID?: string; ownPR?: { url: string; open: boolean }; rect: DOMRect }

// TicketMenu opens a clicked ticket in its tracker, or marks it done or not
// done there, to clean out tickets already finished. An activity or planned
// review is marked done on its plan item, as on Today. A review not in the plan
// has no item to mark, so it only opens on its code host.
function TicketMenu({ t, onClose, onSaved }: { t: Picked; onClose: () => void; onSaved: () => void }) {
  const done = t.stage === 'completed'
  const host = codeHostName(t.source)
  const item = t.itemID
  return (
    <Menu label={t.title} rect={t.rect} onClose={onClose}>
      <b className="block text-sm leading-snug">
        {t.key && `${t.key} `}
        {t.title}
      </b>
      <div className="mb-2.5 mt-1 flex flex-wrap items-center gap-1.5 text-xs text-dim">
        {t.state && <Tag text={finished[t.stage] ?? t.state} color={ticketColor(t.stage, t.state)} />}
        {t.project && <span>{t.project}</span>}
      </div>
      {t.url && (
        <a href={t.url} target="_blank" rel="noreferrer" className="mb-1.5 block rounded-lg border border-border px-2.5 py-1.5 text-center text-sm font-bold text-accent">
          {host ? `Open on ${host}` : `Open in ${tracker(t.source)}`}
        </a>
      )}
      {t.ownPR ? (
        t.ownPR.open ? (
          <MarkDone done={done} ticket={null} mark={() => setOwnPRDone(t.ownPR!.url, !done)} onSaved={onSaved} />
        ) : (
          <p className="text-xs text-dim">No longer open on {host}.</p>
        )
      ) : item ? (
        <MarkDone done={done} ticket={null} mark={() => markItemDone(item, !done)} onSaved={onSaved} />
      ) : (
        !host && <MarkDone done={done} ticket={{ source: t.source, key: t.key }} mark={() => markTicketDone(t.source, t.id, !done)} onSaved={onSaved} />
      )}
    </Menu>
  )
}

// RenameInput edits a name in place: Enter or leaving it saves, Escape keeps the old one.
function RenameInput({ text, onSave, onCancel }: { text: string; onSave: (text: string) => void; onCancel: () => void }) {
  const ended = useRef(false)
  const end = (fn: () => void) => {
    if (ended.current) return
    ended.current = true
    fn()
  }
  return (
    <input
      autoFocus
      aria-label={`Rename ${text}`}
      defaultValue={text}
      onFocus={(e) => e.currentTarget.select()}
      onBlur={(e) => {
        const v = e.currentTarget.value.trim()
        end(() => onSave(v))
      }}
      onKeyDown={(e) => {
        const v = e.currentTarget.value.trim()
        if (e.key === 'Enter') end(() => onSave(v))
        if (e.key === 'Escape') end(onCancel)
      }}
      className="w-full rounded border border-border bg-bg dark:bg-raised px-1 text-sm"
    />
  )
}

// A chip without onPointerDown is not draggable: a ticket already in the plan
// shows where it sits (planned) instead, since dragging it would add it twice.
function Chip(props: {
  kind: Kind
  text: string
  meta?: string
  hours: number
  state?: string
  stage?: string
  badge?: ReactNode
  labels?: string[]
  planned?: string
  done?: boolean
  lines?: { added: number; removed: number }
  action?: { label: string; onClick: () => void }
  onClick?: (rect: DOMRect) => void // for a card that can't be dragged, whose drag can't pick it
  onPointerDown?: (e: React.PointerEvent) => void
  onRemove?: () => void
  // rename, when given, edits the name in place.
  rename?: { onSave: (text: string) => void; onCancel: () => void }
}) {
  const labels = props.labels ?? []
  // A ticket takes its status colour; anything else its kind's.
  const look = props.kind === 'ticket' && props.state ? tone('ticket', ticketColor(props.done ? 'completed' : (props.stage ?? ''), props.state)) : tone(props.kind)
  const color = look.borderLeftColor as string
  return (
    <div
      data-card
      onPointerDown={(e) => !(e.target as HTMLElement).closest('button, input') && props.onPointerDown?.(e)}
      onClick={(e) => !(e.target as HTMLElement).closest('button, input') && props.onClick?.(e.currentTarget.getBoundingClientRect())}
      className={`flex select-none items-center gap-2.5 rounded-lg border border-l-4 border-border bg-bg dark:bg-raised px-2.5 py-1.5 shadow-sm ${props.onPointerDown ? 'cursor-grab hover:shadow-md' : props.onClick ? 'cursor-pointer hover:shadow-md' : ''}`}
      style={{ borderLeftColor: color, touchAction: 'none' }}
    >
      <div className="flex h-9 w-10 flex-none flex-col items-center justify-center rounded-md border border-border font-mono" style={{ background: look.background }}>
        <b className="text-sm leading-none">{fmtNum(props.hours)}</b>
        <span className="text-2xs text-fg opacity-70">h</span>
      </div>
      <div className="min-w-0 flex-1">
        {props.rename ? (
          <RenameInput text={props.text} {...props.rename} />
        ) : (
          <div data-name className={`line-clamp-2 text-sm leading-snug ${props.done ? 'text-dim line-through' : ''}`}>
            {props.text}
          </div>
        )}
        {(props.state || props.badge || props.meta || props.planned || props.lines || labels.length > 0) && (
          // Where it is from on the left, its status and labels on the right.
          <div className="flex min-w-0 flex-wrap items-center gap-1 text-xs text-dim">
            {props.planned && <span className="flex-none font-mono text-fg">✓ {props.planned}</span>}
            {props.meta && <span className="min-w-0 truncate">{props.meta}</span>}
            {props.lines && (
              <span className="flex-none font-mono">
                <span className="text-done">+{props.lines.added}</span> <span className="text-danger">-{props.lines.removed}</span>
              </span>
            )}
            <span className="ml-auto flex flex-none flex-wrap items-center justify-end gap-1">
              {props.state && <Tag text={props.state} color={color} />}
              {props.badge}
              {labels.slice(0, 3).map((l) => (
                <span key={l} className="flex-none whitespace-nowrap rounded-full border border-border px-1.5 text-2xs leading-4">
                  {l}
                </span>
              ))}
              {labels.length > 3 && <span className="flex-none text-2xs">+{labels.length - 3}</span>}
            </span>
          </div>
        )}
      </div>
      {props.action && (
        <button type="button" aria-label={`${props.action.label} ${props.text}`} onClick={props.action.onClick} className="flex-none text-xs text-dim hover:text-fg">
          {props.action.label}
        </button>
      )}
      {props.onRemove && (
        <button aria-label={`Remove ${props.text} from the day`} onClick={props.onRemove} className="text-dim hover:text-fg">
          ×
        </button>
      )}
    </div>
  )
}
