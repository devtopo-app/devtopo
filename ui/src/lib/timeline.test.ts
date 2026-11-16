import { describe, it, expect } from 'vitest'
import {
  snap,
  pack,
  hourWidth,
  hoursIn,
  atHour,
  dateKey,
  capacity,
  trays,
  newPlanDate,
  planName,
  fmtHour,
  fmtDur,
  kindOf,
  isDone,
  reviewURL,
  ago,
  codeHostName,
  finishedLabel,
  workingFor,
  dropStart,
} from './timeline'
import type { Plannable, PlanItem } from '../planner-api'

describe('snap', () => {
  it('rounds to 15 minutes', () => {
    expect(snap(9.1)).toBe(9)
    expect(snap(9.13)).toBe(9.25)
    expect(snap(9.6)).toBe(9.5)
  })
})

describe('pack', () => {
  it('puts overlapping intervals on separate tracks and reuses freed ones', () => {
    const { track, count } = pack([
      { start: 9, end: 10 },
      { start: 9.5, end: 11 },
      { start: 10, end: 10.5 },
      { start: 12, end: 13 },
    ])
    expect(track).toEqual([0, 1, 0, 0])
    expect(count).toBe(2)
  })

  it('has one track when empty', () => {
    expect(pack([])).toEqual({ track: [], count: 1 })
  })
})

describe('hourWidth', () => {
  it('fills the available width, with a minimum', () => {
    expect(hourWidth(1300, 7, 20)).toBe(100)
    expect(hourWidth(200, 7, 20)).toBe(56)
  })
})

describe('hours of a day', () => {
  it('converts between a timestamp and hours from local midnight', () => {
    const t = atHour('2026-10-29', 9.75)
    expect(t.getHours()).toBe(9)
    expect(t.getMinutes()).toBe(45)
    expect(hoursIn('2026-10-29', t.toISOString())).toBe(9.75)
    expect(dateKey(t)).toBe('2026-10-29')
  })
})

describe('capacity', () => {
  it('is planned time against the window minus merged meetings', () => {
    const c = capacity(
      { start: 9, end: 17 },
      [
        { start: 8, end: 9.5 }, // half an hour inside the window
        { start: 11, end: 12 },
        { start: 11.5, end: 12.5 }, // overlaps the one before
      ],
      [
        { start: 16, hours: 2 }, // only 1h inside the window
        { start: 10, hours: 1 },
      ],
      0.5,
    )
    expect(c.meetings).toBe(2)
    expect(c.free).toBe(6)
    expect(c.planned).toBe(2.5)
  })
})

describe('trays', () => {
  const p = (RefSource: string, RefID: string, Kind: string): Plannable => ({
    RefSource, RefID, Kind, Text: RefID, Repo: '', URL: '', Hours: 1, State: '', Stage: '', Project: '', Labels: [],
  })
  const placed = (RefSource: string, RefID: string, Start: string | null): PlanItem => ({
    ID: RefID, PlanID: 'p', RowID: '', RefSource, RefID, Text: RefID, Start, IntendedHours: 1, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null,
  })

  it('sets hidden reviews apart, and leaves out one the plan already uses', () => {
    const t = trays(
      [{ ...p('github', 'r1', 'review_request'), Hidden: true }, p('github', 'r2', 'review_request'), { ...p('github', 'r3', 'review_request'), Hidden: true }],
      [placed('github', 'r3', '2026-10-29T09:00:00Z')],
    )
    expect(t.reviews.map((x) => x.RefID)).toEqual(['r2'])
    expect(t.hiddenReviews.map((x) => x.RefID)).toEqual(['r1'])
  })

  it('splits reviews from tickets and leaves out what the plan has placed or kept for anytime', () => {
    const t = trays(
      [p('github', 'r1', 'review_request'), p('github', 'r2', 'review_request'), p('linear', 't1', 'task'), p('jira', 't2', 'task'), p('jira', 't3', 'task')],
      [placed('github', 'r2', '2026-10-29T09:00:00Z'), placed('jira', 't2', null), placed('', '', '2026-10-29T10:00:00Z')],
    )
    expect(t.reviews.map((x) => x.RefID)).toEqual(['r1'])
    expect(t.tickets.map((x) => x.RefID)).toEqual(['t1', 't3'])
    expect(t.anytime.map((x) => x.RefID)).toEqual(['t2'])
  })

  it('keeps the ticket items the plan holds, in tray order', () => {
    const t = trays(
      [p('linear', 't1', 'task'), p('jira', 't2', 'task'), p('jira', 't3', 'task')],
      [placed('jira', 't3', '2026-10-29T09:00:00Z'), placed('jira', 't2', null)],
    )
    expect(t.plannedTickets.map((x) => x.ID)).toEqual(['t2', 't3'])
    expect(t.tickets.map((x) => x.RefID)).toEqual(['t1'])
  })

  it('keeps a planned ticket that is no longer plannable, after the others', () => {
    // a finished ticket is not offered as plannable, but stays in the plan
    const t = trays([p('linear', 't1', 'task')], [placed('jira', 'done', null), placed('linear', 't1', null), placed('github', 'r1', null), placed('', '', null)])
    expect(t.plannedTickets.map((x) => x.ID)).toEqual(['t1', 'done'])
  })
})

describe('new plans', () => {
  it('starts today, or the day after the last plan', () => {
    const today = new Date(2026, 9, 29, 15, 0)
    expect(newPlanDate([], today)).toBe('2026-10-29')
    expect(newPlanDate(['2026-10-29', '2026-10-31', '2026-10-30'], today)).toBe('2026-11-01')
  })

  it('is named after its date', () => {
    expect(planName('2026-10-29')).toBe('Plan for Thu 29 Oct')
  })
})

describe('formatting', () => {
  it('shows hours as a clock time and a duration', () => {
    expect(fmtHour(9.5)).toBe('9:30')
    expect(fmtDur(1.25)).toBe('1h 15m')
    expect(fmtDur(0.5)).toBe('30m')
    expect(fmtDur(2)).toBe('2h')
  })
})

describe('kindOf', () => {
  it('tells a review, a ticket and an own activity apart by the ref', () => {
    const it = (RefSource: string, RefID: string) =>
      ({ ID: 'i', PlanID: 'p', RowID: 'r', RefSource, RefID, Text: '', Start: null, IntendedHours: 1 }) as PlanItem
    expect(kindOf(it('github', 'u1'))).toBe('review')
    expect(kindOf(it('jira', 'OPS-1'))).toBe('ticket')
    expect(kindOf(it('', ''))).toBe('own')
  })
})

describe('own PRs', () => {
  const item = (over: Partial<PlanItem>) =>
    ({ ID: 'i', PlanID: 'p', RowID: '', RefSource: 'github', RefID: 'own:https://x/pull/1', Text: 'Mine', Start: null, IntendedHours: 1, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null, ...over }) as PlanItem
  const own = (RefID: string, over: Partial<Plannable> = {}) =>
    ({ RefSource: 'github', RefID, Text: RefID, Repo: 'o/r', URL: '', Kind: 'own_pr', Hours: 0.5, State: 'commented', Stage: '', Project: '', Labels: [], ...over }) as Plannable

  it('reads as its own kind, apart from reviews', () => {
    expect(kindOf(item({}))).toBe('yourpr')
    expect(kindOf(item({ RefID: 'https://x/pull/1' }))).toBe('review')
    expect(kindOf(item({ RefSource: 'linear' }))).toBe('ticket')
  })

  it('is done by its stage, not by DoneAt', () => {
    expect(isDone(item({ RefStage: 'completed' }))).toBe(true)
    expect(isDone(item({}))).toBe(false)
  })

  it('opens from its id once it is no longer synced', () => {
    expect(reviewURL(item({}))).toBe('https://x/pull/1')
    expect(reviewURL(item({ RefURL: 'https://y' }))).toBe('https://y')
  })

  it('has its own tray lists, and never lands among tickets or reviews', () => {
    const t = trays([own('a'), own('b', { Done: true }), own('c'), own('d', { Done: true })], [item({ ID: 'c', RefID: 'c' }), item({ ID: 'd', RefID: 'd' })])
    expect(t.yourPRs.map((x) => x.RefID)).toEqual(['a'])
    expect(t.doneYourPRs.map((x) => x.RefID)).toEqual(['b'])
    expect(t.reviews).toEqual([])
    expect(t.hiddenReviews).toEqual([])
    expect(t.tickets).toEqual([])
  })

  it('stays with the planned reviews once planned', () => {
    const t = trays([], [item({})])
    expect(t.plannedReviews.map((x) => x.ID)).toEqual(['i'])
    expect(t.plannedTickets).toEqual([])
  })
})

describe('ago', () => {
  const now = new Date('2026-10-29T12:00:00Z')
  it('says how long ago', () => {
    expect(ago('2026-10-29T11:59:40Z', now)).toBe('just now')
    expect(ago('2026-10-29T11:55:00Z', now)).toBe('5m ago')
    expect(ago('2026-10-29T10:00:00Z', now)).toBe('2h ago')
    expect(ago('2026-10-28T12:00:00Z', now)).toBe('1d ago')
  })
})

describe('code hosts', () => {
  const item = (RefSource: string, RefID: string, over: Partial<PlanItem> = {}) =>
    ({ ID: 'i', PlanID: 'p', RowID: '', RefSource, RefID, Text: 'x', Start: null, IntendedHours: 1, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null, ...over }) as PlanItem

  it.each(['github', 'gitlab', 'azure'])('reads %s refs as reviews and own PRs', (source) => {
    expect(kindOf(item(source, 'https://x/1'))).toBe('review')
    expect(kindOf(item(source, 'own:https://x/1'))).toBe('yourpr')
  })

  it('keeps tickets as tickets, even with an id that looks like an own PR', () => {
    expect(kindOf(item('linear', 'own:1'))).toBe('ticket')
    expect(kindOf(item('jira', 'OPS-1'))).toBe('ticket')
  })

  it('reads done the same way on every host', () => {
    expect(isDone(item('gitlab', 'https://x/1', { DoneAt: '2026-10-29T09:00:00Z' }))).toBe(true)
    expect(isDone(item('azure', 'own:https://x/1', { RefStage: 'completed' }))).toBe(true)
  })

  it('opens a closed own PR from its id on every host', () => {
    expect(reviewURL(item('azure', 'own:https://dev.azure.com/a/P/_git/r/pullrequest/9'))).toBe('https://dev.azure.com/a/P/_git/r/pullrequest/9')
  })

  it('names the hosts', () => {
    expect(codeHostName('github')).toBe('GitHub')
    expect(codeHostName('gitlab')).toBe('GitLab')
    expect(codeHostName('azure')).toBe('Azure DevOps')
  })
})

describe('finishedLabel', () => {
  const now = new Date(2026, 10, 5, 15, 30, 0)
  const before = (minutes: number) => new Date(now.getTime() - minutes * 60000).toISOString()

  it('says how many minutes ago below an hour', () => {
    expect(finishedLabel(before(2), now)).toBe('finished 2 minutes ago')
    expect(finishedLabel(before(1), now)).toBe('finished 1 minute ago')
    expect(finishedLabel(before(59), now)).toBe('finished 59 minutes ago')
    expect(finishedLabel(new Date(now.getTime() - 59.9 * 60000).toISOString(), now)).toBe('finished 59 minutes ago')
  })

  it('says just now under a minute, or when the clocks disagree', () => {
    expect(finishedLabel(before(0.5), now)).toBe('finished just now')
    expect(finishedLabel(before(-1), now)).toBe('finished just now')
  })

  it('gives the time of day from an hour on', () => {
    expect(finishedLabel(before(60), now)).toBe('finished at 14:30')
    expect(finishedLabel(new Date(2026, 10, 5, 9, 5).toISOString(), now)).toBe('finished at 09:05')
  })

  it('adds the day when it was not today', () => {
    expect(finishedLabel(new Date(2026, 10, 4, 23, 50).toISOString(), now)).toBe('finished 4 Nov at 23:50')
  })

  it('says nothing without a time', () => {
    expect(finishedLabel('', now)).toBe('')
    expect(finishedLabel(undefined, now)).toBe('')
    expect(finishedLabel('0001-01-01T00:00:00Z', now)).toBe('')
  })
})

describe('workingFor', () => {
  const now = new Date(2026, 10, 5, 15, 30, 0)
  const before = (seconds: number) => new Date(now.getTime() - seconds * 1000).toISOString()

  it('counts seconds, then minutes and seconds, then hours and minutes', () => {
    expect(workingFor(before(45), now)).toBe('45s')
    expect(workingFor(before(60), now)).toBe('1m 00s')
    expect(workingFor(before(12 * 60 + 5), now)).toBe('12m 05s')
    expect(workingFor(before(3599), now)).toBe('59m 59s')
    expect(workingFor(before(3600), now)).toBe('1h 00m')
    expect(workingFor(before(2 * 3600 + 5 * 60 + 30), now)).toBe('2h 05m')
  })

  it('stays at nothing when the clocks disagree or there is no start', () => {
    expect(workingFor(before(-5), now)).toBe('0s')
    expect(workingFor('', now)).toBe('')
    expect(workingFor(undefined, now)).toBe('')
    expect(workingFor('0001-01-01T00:00:00Z', now)).toBe('')
  })
})

// A time just before the hour used to round to 8:60.
describe('times just before the hour', () => {
  it('roll over to the next hour', () => {
    expect(fmtHour(8 + 59.5 / 60)).toBe('9:00')
    expect(fmtHour(8 + 59.9 / 60)).toBe('9:00')
    expect(fmtHour(23 + 59.9 / 60)).toBe('24:00')
    expect(fmtDur(1 + 59.9 / 60)).toBe('2h')
    expect(fmtDur(59.9 / 60)).toBe('1h')
  })

  it('stay put when they round down', () => {
    expect(fmtHour(8 + 59.4 / 60)).toBe('8:59')
    expect(fmtDur(1 + 59.4 / 60)).toBe('1h 59m')
  })

  it('never show as :60 at any second of a day', () => {
    for (let s = 0; s < 86400; s++) expect(fmtHour(s / 3600)).not.toMatch(/:60/)
  })

  it('never reach a session as :60 either', () => {
    const now = new Date(2026, 10, 5, 23, 0, 0)
    expect(finishedLabel(new Date(2026, 10, 5, 8, 59, 59, 900).toISOString(), now)).toBe('finished at 08:59')
    expect(workingFor(new Date(now.getTime() - (3600 + 59 * 60 + 59.9) * 1000).toISOString(), now)).toBe('1h 59m')
    expect(workingFor(new Date(now.getTime() - 59.9 * 1000).toISOString(), now)).toBe('59s')
  })
})

describe('dropStart', () => {
  const view = { from: 7, to: 20 }
  it('centres the task on the pointer, on a quarter hour', () => {
    expect(dropStart(13.5, 2, view)).toBe(12.5)
    expect(dropStart(13.6, 1, view)).toBe(13)
    expect(dropStart(13.2, 0.5, view)).toBe(13)
    expect(dropStart(13.13, 0.25, view)).toBe(13)
  })
  it('keeps the whole task inside the view', () => {
    expect(dropStart(7.2, 2, view)).toBe(7)
    expect(dropStart(19.9, 2, view)).toBe(18)
  })
})
