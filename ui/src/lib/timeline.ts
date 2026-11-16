import type { Plannable, PlanItem } from '../planner-api'

// Pure timeline math for the Planner. Times on the timeline are hours from the
// plan day's local midnight (9.5 = 9:30); dates are local "YYYY-MM-DD" keys.

export const SNAP = 0.25

export const snap = (h: number) => Math.round(h / SNAP) * SNAP
export const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v))

// pack gives each interval the first sub-track that is free at its start
// (greedy interval packing), so overlapping blocks never hide each other.
// track[i] belongs to intervals[i]; count is at least 1.
export function pack(intervals: { start: number; end: number }[]): { track: number[]; count: number } {
  const ends: number[] = []
  const track: number[] = new Array(intervals.length)
  const order = intervals.map((_, i) => i).sort((a, b) => intervals[a].start - intervals[b].start || intervals[b].end - intervals[a].end)
  for (const i of order) {
    let t = ends.findIndex((e) => e <= intervals[i].start + 1e-9)
    if (t < 0) t = ends.push(0) - 1
    ends[t] = intervals[i].end
    track[i] = t
  }
  return { track, count: Math.max(1, ends.length) }
}

// dropStart is where a task of the given hours starts when dropped with the pointer
// at hour: centred on it, on a quarter hour, and wholly inside the view. The drop
// preview and the drop both use it, so the task lands where it showed.
export const dropStart = (hour: number, hours: number, view: { from: number; to: number }) =>
  clamp(snap(hour - hours / 2), view.from, view.to - hours)

// hourWidth spreads the view's hours across the available pixels, never
// narrower than a readable minimum (the timeline scrolls then).
export const hourWidth = (available: number, from: number, to: number) => Math.max(56, available / (to - from))

export function dateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const midnight = (date: string) => new Date(`${date}T00:00`)

export const atHour = (date: string, h: number) => new Date(midnight(date).getTime() + h * 3600 * 1000)

export const hoursIn = (date: string, iso: string) => (new Date(iso).getTime() - midnight(date).getTime()) / 3600 / 1000

// capacity is the plan's planned time (blocks inside the window plus anytime
// items) against its free time (the window minus merged meetings in it).
export function capacity(
  window: { start: number; end: number },
  meetings: { start: number; end: number }[],
  blocks: { start: number; hours: number }[],
  anytimeHours: number,
) {
  const overlap = (s: number, e: number) => Math.max(0, Math.min(window.end, e) - Math.max(window.start, s))
  let met = 0
  let reach = window.start
  for (const m of [...meetings].sort((a, b) => a.start - b.start)) {
    met += overlap(Math.max(m.start, reach), m.end)
    reach = Math.max(reach, m.end)
  }
  const hours = window.end - window.start
  return {
    hours,
    meetings: met,
    free: Math.max(0, hours - met),
    planned: blocks.reduce((s, b) => s + overlap(b.start, b.start + b.hours), 0) + anytimeHours,
  }
}

// trays splits what can be planned into reviews and tickets, leaving out what
// the plan has placed or kept for anytime; anytime lists those kept ones.
// hiddenReviews are the open reviews the user hid, apart from the rest, and
// yourPRs and doneYourPRs the user's own PRs that are open in the tray, by whether
// they are marked done. plannedReviews holds own PRs in the plan too.
// plannedTickets are the ticket items the plan holds, in tray order; one no
// longer plannable (finished) comes last.
export function trays(plannable: Plannable[], items: PlanItem[]) {
  const key = (source: string, id: string) => `${source}/${id}`
  const used = new Set(items.filter((i) => i.RefID).map((i) => key(i.RefSource, i.RefID)))
  const open = plannable.filter((p) => !used.has(key(p.RefSource, p.RefID)))
  const at = new Map(plannable.map((p, i) => [key(p.RefSource, p.RefID), i]))
  const rank = (it: PlanItem) => at.get(key(it.RefSource, it.RefID)) ?? plannable.length
  return {
    reviews: open.filter((p) => p.Kind === 'review_request' && !p.Hidden),
    hiddenReviews: open.filter((p) => p.Kind === 'review_request' && p.Hidden),
    plannedReviews: items.filter((it) => kindOf(it) === 'review' || kindOf(it) === 'yourpr'),
    yourPRs: open.filter((p) => p.Kind === 'own_pr' && !p.Done),
    doneYourPRs: open.filter((p) => p.Kind === 'own_pr' && p.Done),
    tickets: open.filter((p) => p.Kind !== 'review_request' && p.Kind !== 'own_pr'),
    plannedTickets: items
      .filter((it) => kindOf(it) === 'ticket')
      .sort((a, b) => rank(a) - rank(b)),
    anytime: items.filter((i) => i.Start === null),
  }
}

export type Kind = 'review' | 'yourpr' | 'ticket' | 'own'

const ownPRPrefix = 'own:'

const codeHosts: Record<string, string> = { github: 'GitHub', gitlab: 'GitLab', azure: 'Azure DevOps' }

// codeHostName names a source that holds PRs; "" for one that doesn't.
export const codeHostName = (source: string) => codeHosts[source] ?? ''

// kindOf reads a placed item's kind from its ref: a code host's refs are reviews,
// or the user's own PRs by their prefix; any other ref is a ticket, and no ref is
// the user's own activity.
export const kindOf = (it: PlanItem): Kind => {
  if (!it.RefID) return 'own'
  if (!codeHostName(it.RefSource)) return 'ticket'
  return it.RefID.startsWith(ownPRPrefix) ? 'yourpr' : 'review'
}

// isDone: a ticket is done when its tracker says so, and an own PR when the
// server works that out; anything else when marked done here.
export const isDone = (it: PlanItem) => {
  const kind = kindOf(it)
  return kind === 'ticket' || kind === 'yourpr' ? it.RefStage === 'completed' : it.DoneAt !== null
}

// reviewURL is where a planned PR opens: its stored address, else its id, which
// is the PR's address too, after any prefix. "" when neither is one.
export const reviewURL = (it: PlanItem): string => {
  if (it.RefURL) return it.RefURL
  const id = it.RefID.startsWith(ownPRPrefix) ? it.RefID.slice(ownPRPrefix.length) : it.RefID
  return id.startsWith('https://') ? id : ''
}

// workingFor says how long a task has run since it started, "" with no start, which
// the Go zero time also is. Seconds show up to an hour.
export function workingFor(iso: string | undefined, now: Date): string {
  const t = iso ? new Date(iso) : null
  if (!t || isNaN(t.getTime()) || t.getFullYear() < 2) return ''
  const seconds = Math.max(0, Math.floor((now.getTime() - t.getTime()) / 1000))
  const two = (n: number) => String(n).padStart(2, '0')
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${two(seconds % 60)}s`
  return `${Math.floor(seconds / 3600)}h ${two(Math.floor(seconds / 60) % 60)}m`
}

// finishedLabel says when an agent finished: in minutes below an hour, then the
// time of day, with the day when it wasn't today. "" for no time, which the Go zero
// time also is.
export function finishedLabel(iso: string | undefined, now: Date): string {
  const t = iso ? new Date(iso) : null
  if (!t || isNaN(t.getTime()) || t.getFullYear() < 2) return ''
  const minutes = Math.floor((now.getTime() - t.getTime()) / 60000)
  if (minutes < 1) return 'finished just now'
  if (minutes < 60) return `finished ${minutes} ${minutes === 1 ? 'minute' : 'minutes'} ago`
  const clock = `${String(t.getHours()).padStart(2, '0')}:${String(t.getMinutes()).padStart(2, '0')}`
  if (dateKey(t) === dateKey(now)) return `finished at ${clock}`
  return `finished ${t.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })} at ${clock}`
}

// ago says how long before now a time was, in its largest whole unit.
export function ago(iso: string, now: Date): string {
  const minutes = Math.floor((now.getTime() - new Date(iso).getTime()) / 60000)
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${minutes}m ago`
  if (minutes < 24 * 60) return `${Math.floor(minutes / 60)}h ago`
  return `${Math.floor(minutes / (24 * 60))}d ago`
}

// newPlanDate is today when there are no plans, else the day after the last.
export function newPlanDate(dates: string[], today: Date): string {
  if (!dates.length) return dateKey(today)
  const d = midnight([...dates].sort().pop()!)
  d.setDate(d.getDate() + 1)
  return dateKey(d)
}

export const dayLabel = (date: string) =>
  midnight(date).toLocaleDateString('en-GB', { weekday: 'short', day: 'numeric', month: 'short' }).replace(',', '')

export const planName = (date: string) => `Plan for ${dayLabel(date)}`

// wholeMinutes splits hours into whole hours and minutes, rounding first so that a
// time just before the hour rolls over rather than reading as :60.
function wholeMinutes(h: number): [number, number] {
  const minutes = Math.round(h * 60)
  return [Math.floor(minutes / 60), ((minutes % 60) + 60) % 60]
}

export function fmtHour(h: number): string {
  const [H, M] = wholeMinutes(h)
  return `${H}:${String(M).padStart(2, '0')}`
}

export function fmtDur(h: number): string {
  const [H, M] = wholeMinutes(h)
  return [H && `${H}h`, M && `${M}m`].filter(Boolean).join(' ') || '0h'
}

export const fmtNum = (h: number) => String(Math.round(h * 100) / 100)
