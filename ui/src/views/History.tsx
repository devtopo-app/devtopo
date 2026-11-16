import { useEffect, useState } from 'react'
import { fetchHistoryDay, fetchHistoryMonth, fetchHistoryPoints, type HistoryDay, type HistoryMonthDay } from '../api'
import { Points } from '../components/Points'
import { defaultTitle } from '../components/Card'
import { dateKey, fmtHour, fmtNum, hoursIn } from '../lib/timeline'
import { Tile } from './Today'

const box = 'min-w-0 rounded-[10px] border border-border bg-bg dark:bg-raised px-3 py-2.5'
const head = 'mb-2 text-xs font-bold uppercase tracking-wider text-dim'

// level sizes an active day's dot by its AI time: under 1h, 1-3h, 3h and more.
const level = (h: number) => (h >= 3 ? 3 : h >= 1 ? 2 : 1)
const dotSize = { 1: 6, 2: 8, 3: 10 }

const noon = (date: string) => new Date(`${date}T12:00`)
const monthTitle = (month: string) => noon(`${month}-01`).toLocaleDateString('en-GB', { month: 'long', year: 'numeric' })
const addMonths = (month: string, n: number) => {
  const d = noon(`${month}-01`)
  d.setMonth(d.getMonth() + n)
  return dateKey(d).slice(0, 7)
}

// Calendar is the month grid, Monday first: a dot on each active day, today in
// the accent, the selected day outlined, a date input and the month's totals.
function Calendar(props: { month: string; days: HistoryMonthDay[]; selected: string; onMonth: (m: string) => void; onPick: (d: string) => void }) {
  const byDate = new Map(props.days.map((d) => [d.Date, d]))
  const first = noon(`${props.month}-01`)
  const lead = (first.getDay() + 6) % 7
  const count = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate()
  const cells = Array.from({ length: Math.ceil((lead + count) / 7) * 7 }, (_, i) => new Date(first.getFullYear(), first.getMonth(), 1 - lead + i, 12))
  const today = dateKey(new Date())
  const ai = props.days.reduce((a, d) => a + d.AIHours, 0)
  const tickets = props.days.reduce((a, d) => a + d.Tickets, 0)
  const nav = 'h-7 w-7 rounded-lg border border-border bg-bg dark:bg-raised text-fg hover:border-accent'
  return (
    <aside aria-label="Pick a day" className="rounded-[14px] border border-border bg-panel p-3 shadow-sm">
      <div className="mb-2.5 flex items-center gap-1.5">
        <button type="button" aria-label="Previous month" className={nav} onClick={() => props.onMonth(addMonths(props.month, -1))}>
          ‹
        </button>
        <b className="flex-1 text-center text-sm">{monthTitle(props.month)}</b>
        <button type="button" aria-label="Next month" className={nav} onClick={() => props.onMonth(addMonths(props.month, 1))}>
          ›
        </button>
      </div>
      <div className="grid grid-cols-7 gap-1">
        {['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'].map((d) => (
          <div key={d} className="pb-0.5 text-center text-2xs font-bold tracking-wider text-dim">
            {d}
          </div>
        ))}
        {cells.map((d) => {
          const k = dateKey(d)
          if (k.slice(0, 7) !== props.month) {
            return (
              <div key={k} aria-hidden className="flex aspect-square items-center justify-center text-xs text-dim opacity-40">
                {d.getDate()}
              </div>
            )
          }
          const data = byDate.get(k)
          const label = `${d.getDate()} ${monthTitle(props.month)}${data ? `, ${data.AIHours.toFixed(1)} AI hours` : ', no activity'}`
          return (
            <button
              key={k}
              type="button"
              aria-label={label}
              aria-pressed={k === props.selected}
              onClick={() => props.onPick(k)}
              className={`flex aspect-square flex-col items-center justify-center gap-[3px] rounded-[9px] border bg-bg dark:bg-raised text-xs hover:border-accent ${
                k === props.selected ? 'border-accent font-extrabold shadow-[inset_0_0_0_1px_var(--accent)]' : 'border-transparent'
              }`}
            >
              <span className={k === today ? 'font-extrabold text-accent' : ''}>{d.getDate()}</span>
              {data ? (
                <i data-level={level(data.AIHours)} className="rounded-full bg-accent" style={{ width: dotSize[level(data.AIHours)], height: dotSize[level(data.AIHours)] }} />
              ) : (
                <i className="h-1.5" />
              )}
            </button>
          )
        })}
      </div>
      <label className="mt-3 flex items-center gap-2 text-xs text-dim">
        Go to
        <input
          type="date"
          value={props.selected}
          onChange={(e) => e.target.value > '2000' && props.onPick(e.target.value)}
          className="min-w-0 flex-1 rounded-lg border border-border bg-bg dark:bg-raised px-2 py-1 text-fg"
        />
      </label>
      <div className="mt-2.5 flex gap-3 text-2xs text-dim">
        {([1, 2, 3] as const).map((l) => (
          <span key={l} className="inline-flex items-center gap-1">
            <i className="rounded-full bg-accent" style={{ width: dotSize[l], height: dotSize[l] }} />
            {['< 1h', '1-3h', '3h+ AI time'][l - 1]}
          </span>
        ))}
      </div>
      <div aria-label="Month totals" className="mt-3 grid gap-0.5 border-t border-border pt-2.5 text-xs text-dim">
        {props.days.length ? (
          <>
            <span>
              <b className="text-fg">{props.days.length}</b> active days
            </span>
            <span>
              <b className="text-fg">{ai.toFixed(1)}h</b> AI time · <b className="text-fg">{tickets}</b> tickets finished
            </span>
          </>
        ) : (
          <span>No activity this month.</span>
        )}
      </div>
    </aside>
  )
}

// Hours is the day's AI time by hour, one bar per hour of the day.
function Hours({ hours }: { hours: number[] }) {
  const max = Math.max(...hours, 0.01)
  return (
    <section aria-label="When the agents worked" className={`${box} mb-3.5`}>
      <h3 className={head}>When the agents worked</h3>
      <div className="grid h-14 items-end gap-0.5" style={{ gridTemplateColumns: 'repeat(24, minmax(0, 1fr))' }}>
        {hours.map((h, i) => (
          <div
            key={i}
            title={`${i}:00-${i + 1}:00: ${Math.round(h * 60)} AI-min`}
            className="rounded-t-[3px]"
            style={{ height: `${Math.max(4, (h / max) * 100)}%`, background: h ? 'color-mix(in srgb, var(--accent) 65%, var(--bg))' : 'var(--panel)' }}
          />
        ))}
      </div>
      <div className="mt-1 grid font-mono text-2xs text-dim" style={{ gridTemplateColumns: 'repeat(24, minmax(0, 1fr))' }}>
        {hours.map((_, i) => (
          <span key={i} className={i % 2 ? 'invisible' : ''}>
            {i}
          </span>
        ))}
      </div>
    </section>
  )
}

// Day is the selected day's overview. A past session without points yet gets
// them made here, showing "Summarizing..." meanwhile. It asks for one session at
// a time, as the server makes them one at a time, and leaving the day cancels the
// ask: waiting asks would hold the browser's few connections to the daemon, and
// the next day picked could not load until they freed up.
function Day({ day }: { day: HistoryDay }) {
  const [points, setPoints] = useState<Record<string, string[] | 'loading'>>({})
  useEffect(() => {
    const ask = new AbortController()
    const waiting = day.Sessions.filter((s) => s.Summarizing)
    setPoints(Object.fromEntries(waiting.map((s) => [s.ID, 'loading'])))
    const makeAll = async () => {
      for (const s of waiting) {
        const p = await fetchHistoryPoints(day.Date, s.ID, ask.signal).catch(() => [])
        if (ask.signal.aborted) return
        setPoints((m) => ({ ...m, [s.ID]: p }))
      }
    }
    makeAll()
    return () => ask.abort()
  }, [day])

  const title = noon(day.Date).toLocaleDateString('en-GB', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).replace(',', '')
  const n = day.Sessions.length
  if (!n && !day.Tickets.length && !day.Reviews && !day.OtherTasks) {
    return (
      <div>
        <h2 className="mb-3 text-xl font-bold">{title}</h2>
        <div className="rounded-xl border border-dashed border-border bg-bg dark:bg-raised px-3 py-12 text-center text-dim">Nothing ran this day.</div>
      </div>
    )
  }
  const at = (iso: string) => fmtHour(hoursIn(day.Date, iso))
  return (
    <div>
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3.5 gap-y-1.5">
        <h2 className="text-xl font-bold">{title}</h2>
        <span className="text-dim">
          {n} {n === 1 ? 'session' : 'sessions'}
          {n > 0 && ` · active ${at(day.First)}-${at(day.Last)}`}
        </span>
      </div>
      <div className="mb-3.5 grid gap-2.5 [grid-template-columns:repeat(auto-fit,minmax(130px,1fr))]">
        <Tile label="AI hours" num={day.AIHours.toFixed(1)} unit="h" sub={`across ${n} sessions`} color="var(--accent)" />
        <Tile label="Prompts sent" num={day.Prompts} sub="to your coding agents" color="var(--accent)" />
        <Tile label="Tickets finished" num={day.Tickets.length} sub="Linear and Jira" color="var(--done)" />
        <Tile label="PR reviews" num={day.Reviews} sub="finished" color="var(--waiting)" />
        <Tile label="Other tasks" num={day.OtherTasks} sub="your own activities" color="var(--done)" />
        {day.MeetingHours !== null && <Tile label="Meetings" num={fmtNum(day.MeetingHours)} unit="h" sub="in your calendar" color="var(--idle)" />}
      </div>
      <Hours hours={day.Hours} />
      <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(300px,1fr))]">
        <section aria-label="What you achieved" className={box}>
          <h3 className={head}>What you achieved</h3>
          {n === 0 && <p className="text-xs italic text-dim">No sessions.</p>}
          {day.Sessions.map((s, i) => {
            const p = points[s.ID] ?? s.Points
            return (
              <div key={s.ID} className={i ? 'border-t border-border py-2' : 'pb-2'}>
                <div className="flex min-w-0 items-center gap-1.5 font-bold">
                  <span className="truncate">{s.Name || defaultTitle(s)}</span>
                  {s.Agent === 'codex' ? (
                    <span className="shrink-0 rounded border border-border px-1 text-2xs text-codex">Codex</span>
                  ) : (
                    <span className="shrink-0 rounded border border-border px-1 text-2xs text-claude">Claude</span>
                  )}
                </div>
                <div className="mb-1 mt-0.5 text-xs text-dim">
                  <span className="break-all">{s.Project}</span> &middot; <span className="rounded bg-raised px-1.5 text-tag">{s.Branch}</span> &middot;{' '}
                  <span className="text-aiH">{s.AIHours.toFixed(1)} AI-h</span> &middot; {s.Prompts} prompts
                </div>
                {p === 'loading' ? (
                  <p className="text-xs text-dim">Summarizing...</p>
                ) : (
                  <Points points={p} className="text-sm" />
                )}
              </div>
            )
          })}
        </section>
        <section aria-label={`Tickets finished · ${day.Tickets.length}`} className={`${box} self-start`}>
          <h3 className={head}>Tickets finished · {day.Tickets.length}</h3>
          {day.Tickets.length === 0 ? (
            <p className="text-xs italic text-dim">None.</p>
          ) : (
            <ul>
              {[...day.Tickets].reverse().map((t, i) => (
                <li key={i} className="grid grid-cols-[44px_auto_minmax(0,1fr)] items-baseline gap-2.5 py-0.5 text-sm">
                  <span className="font-mono text-xs text-dim">{at(t.At)}</span>
                  <span className="whitespace-nowrap font-mono text-xs text-dim">{t.Key}</span>
                  {t.URL ? (
                    <a href={t.URL} target="_blank" rel="noreferrer" className="truncate hover:underline">
                      {t.Title}
                    </a>
                  ) : (
                    <span className="truncate">{t.Title}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  )
}

// History looks back: a month calendar on the left, the picked day on the
// right. It opens on today.
export function History() {
  const [selected, setSelected] = useState(() => dateKey(new Date()))
  const [month, setMonth] = useState(() => selected.slice(0, 7))
  const [days, setDays] = useState<HistoryMonthDay[]>([])
  const [day, setDay] = useState<HistoryDay | null>(null)
  const [error, setError] = useState(false)

  useEffect(() => {
    let alive = true
    fetchHistoryMonth(month)
      .then((d) => alive && setDays(d))
      .catch(() => alive && setError(true))
    return () => {
      alive = false
    }
  }, [month])
  useEffect(() => {
    let alive = true
    fetchHistoryDay(selected)
      .then((d) => alive && setDay(d))
      .catch(() => alive && setError(true))
    return () => {
      alive = false
    }
  }, [selected])

  if (error) {
    return <p className="text-dim">Could not reach the daemon. Is `devtopo app` running?</p>
  }
  return (
    <div className="grid items-start gap-4 md:grid-cols-[320px_minmax(0,1fr)]">
      <Calendar
        month={month}
        days={days}
        selected={selected}
        onMonth={setMonth}
        onPick={(d) => {
          setSelected(d)
          setMonth(d.slice(0, 7))
        }}
      />
      {day ? <Day day={day} /> : <p className="text-dim">Loading the day...</p>}
    </div>
  )
}
