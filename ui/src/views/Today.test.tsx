import { describe, it, expect, vi, beforeEach, afterEach, onTestFinished } from 'vitest'
import { render, screen, waitFor, within, fireEvent, createEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Today } from './Today'
import * as api from '../api'
import * as plannerApi from '../planner-api'
import type { CardData, LaneData, TodayView, YourPR } from '../api'
import type { Plan, PlanItem } from '../planner-api'

vi.mock('../api', async (orig) => {
  const actual = await orig<typeof api>()
  return { ...actual, fetchToday: vi.fn() }
})
vi.mock('../planner-api', async (orig) => {
  const actual = await orig<typeof plannerApi>()
  return { ...actual, fetchPlans: vi.fn(), addItem: vi.fn(), updateItem: vi.fn(), deleteItem: vi.fn() }
})

const fetchToday = api.fetchToday as unknown as ReturnType<typeof vi.fn>
const fetchPlans = plannerApi.fetchPlans as unknown as ReturnType<typeof vi.fn>
const addItem = plannerApi.addItem as unknown as ReturnType<typeof vi.fn>
const updateItem = plannerApi.updateItem as unknown as ReturnType<typeof vi.fn>
const deleteItem = plannerApi.deleteItem as unknown as ReturnType<typeof vi.fn>
// The done endpoints go through the real helpers to a mocked fetch.
const fetchMock = vi.fn()

function card(id: string, project: string, status: string): CardData {
  return {
    ID: id, Project: project, Branch: 'main', Status: status,
    AIHours: 1, WallHours: 1, Prompts: 1, LastActivity: '', TodayPoints: null, TodayAsOf: '', Signals: null,
    Host: '', Live: false, CanFocus: true,
  }
}
function lane(title: string, status: string, cards: CardData[]): LaneData {
  return { Title: title, Status: status, Cards: cards }
}

// A local-time ISO string, so the test reads the same in any time zone.
const iso = (date: string, h: number) => new Date(new Date(`${date}T00:00`).getTime() + h * 3600 * 1000).toISOString()

function item(id: string, text: string, start: number | null, hours: number, rowId = 'r1'): PlanItem {
  return { ID: id, PlanID: 'p', RowID: rowId, RefSource: '', RefID: '', Text: text, Start: start === null ? null : iso('2026-10-29', start), IntendedHours: hours, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null }
}
const ticket = (it: PlanItem, key: string, refID: string, stage = 'started'): PlanItem =>
  ({ ...it, RefSource: 'linear', RefID: refID, RefRepo: key, RefState: stage === 'completed' ? 'Done' : 'In Progress', RefStage: stage })
function plan(id: string, date: string, items: PlanItem[]): Plan {
  return {
    ID: id, Name: `Plan ${id}`, WindowStart: iso(date, 9), WindowEnd: iso(date, 18), CreatedAt: '', Status: 'active',
    MakerHours: 0, MeetingHours: 0, Meetings: [{ Title: 'Standup', Start: iso(date, 9), End: iso(date, 9.5) }], Items: items,
    Rows: [{ ID: 'r1', PlanID: id, Name: 'Work plan', Order: 0 }],
  }
}

const base: TodayView = {
  Running: lane('Running', 'running', [card('r', 'proj-run', 'running')]),
  Waiting: lane('Waiting on you', 'waiting', [card('w', 'proj-wait', 'waiting')]),
  Idle: lane('Idle', 'idle', []),
  DayAIHours: 2.7, DaySpanHours: 3, DaySessions: 2, StateOfNow: '1 running', AsOf: '',
  Reviews: [{ Title: 'Please review me', Repo: 'r', URL: 'u', UpdatedAt: '' }], ContribPRs: 0,
  FinishedToday: [], PromptsToday: 58, ReviewsDone: [], StartedToday: [],
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-10-29T11:00'))
  fetchToday.mockResolvedValue(base)
  fetchPlans.mockResolvedValue([])
  addItem.mockResolvedValue({})
  updateItem.mockResolvedValue(undefined)
  deleteItem.mockResolvedValue(undefined)
  fetchMock.mockImplementation(async () => new Response('{}', { status: 200 }))
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const tile = (name: string) => within(screen.getByRole('group', { name }))
// Calendar blocks and chips are clicked with fireEvent: dragging prevents their
// pointerdown, and user-event then drops the click a browser still sends.

describe('Today', () => {
  it('renders the running, waiting and idle lanes with counts and project names, and no done lane', async () => {
    render(<Today />)
    await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
    expect(screen.getByText('proj-wait')).toBeInTheDocument()
    expect(screen.getByText(/Running \(1\)/)).toBeInTheDocument()
    expect(screen.getByText(/Waiting on you \(1\)/)).toBeInTheDocument()
    expect(screen.getByText(/Idle \(0\)/)).toBeInTheDocument()
    expect(screen.queryByText(/Done today/)).toBeNull()
  })

  it('shows the day in numbers: sessions, prompts and what got finished', async () => {
    const done = (it: PlanItem, h: number) => ({ ...it, DoneAt: iso('2026-10-29', h) })
    fetchToday.mockResolvedValue({
      ...base,
      FinishedToday: [
        { Source: 'jira', ID: 'OPS-7', Key: 'OPS-7', Title: 'Rotate the keys', URL: '', Project: '', CompletedAt: iso('2026-10-29', 10) },
      ],
      ReviewsDone: [{ Title: 'Data example service', Repo: 'x', URL: '', SubmittedAt: iso('2026-10-29', 9) }],
    })
    fetchPlans.mockResolvedValue([
      plan('t', '2026-10-29', [
        done(item('a', 'Get the spray', null, 1), 10),
        done({ ...item('b', 'Read the categories', null, 0.5), RefSource: 'github', RefID: 'u1' }, 10),
        { ...item('c', 'Done yesterday', null, 1), DoneAt: iso('2026-10-28', 15) },
      ]),
    ])
    render(<Today />)
    await waitFor(() => expect(tile('Other tasks finished').getByText('1')).toBeInTheDocument())
    expect(tile('AI hours').getByText('2.7')).toBeInTheDocument()
    expect(tile('AI hours').getByText('across 2 sessions')).toBeInTheDocument()
    for (const gone of ['Running', 'Waiting on you', 'Idle', 'Done']) {
      expect(screen.queryByRole('group', { name: gone })).toBeNull()
    }
    expect(tile('Prompts sent').getByText('58')).toBeInTheDocument()
    expect(tile('Tickets finished').getByText('1')).toBeInTheDocument()
    expect(tile('PR reviews finished').getByText('2')).toBeInTheDocument()
    // The five tiles share one row.
    const row = screen.getByRole('group', { name: 'AI hours' }).parentElement!
    expect(within(row).getAllByRole('group')).toHaveLength(5)
  })

  describe('modules', () => {
    const order = (c: HTMLElement) => [...c.querySelectorAll('[data-module]')].map((m) => m.getAttribute('data-module'))
    beforeEach(() => localStorage.clear())

    it('shows the numbers, the sessions, the plan, your PRs and what got finished, in that order', async () => {
      const { container } = render(<Today />)
      await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
      expect(order(container)).toEqual(['numbers', 'sessions', 'plan', 'yourprs', 'finished'])
    })

    it('moves a module to where it is dropped, and keeps that order', async () => {
      const { container, unmount } = render(<Today />)
      await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
      const sessions = container.querySelector('[data-module="sessions"]')!
      const numbers = container.querySelector('[data-module="numbers"]')!
      fireEvent.dragStart(screen.getByRole('button', { name: 'Move Sessions' }))
      fireEvent.dragOver(numbers)
      fireEvent.drop(numbers)
      expect(order(container)).toEqual(['sessions', 'numbers', 'plan', 'yourprs', 'finished'])
      expect(sessions).toBeInTheDocument()
      unmount()

      const again = render(<Today />)
      await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
      expect(order(again.container)).toEqual(['sessions', 'numbers', 'plan', 'yourprs', 'finished'])
    })
  })

  it('counts a review once when it is marked done here and also submitted on GitHub', async () => {
    const url = 'https://github.com/x/pull/9'
    fetchToday.mockResolvedValue({ ...base, ReviewsDone: [{ Title: 'Read the categories', Repo: 'x', URL: url, SubmittedAt: iso('2026-10-29', 9) }] })
    fetchPlans.mockResolvedValue([
      plan('t', '2026-10-29', [{ ...item('b', 'Read the categories', null, 0.5), RefSource: 'github', RefID: url, DoneAt: iso('2026-10-29', 10) }]),
    ])
    render(<Today />)
    await waitFor(() => expect(tile('PR reviews finished').getByText('1')).toBeInTheDocument())
  })

  it("draws today's plan on a calendar with the meetings and what is done, without a capacity bar", async () => {
    fetchPlans.mockResolvedValue([
      plan('t', '2026-10-29', [
        item('a', 'Lunch + walk', 13, 1),
        ticket(item('c', 'Deploy the onboarding flow', 9.5, 3), 'ENG-1', 'lin-1', 'completed'),
        item('b', 'Read the inbox', null, 0.5),
      ]),
      plan('x', '2026-10-30', [item('e', 'Tomorrow thing', 10, 1)]),
    ])
    render(<Today />)
    const card = within(await screen.findByRole('region', { name: "Today's plan" }))
    await waitFor(() => expect(card.getByText('Standup')).toBeInTheDocument())
    expect(card.queryByRole('tab')).toBeNull()
    expect(card.getByRole('button', { name: /Lunch \+ walk/ })).toBeInTheDocument()
    expect(card.getByRole('button', { name: /ENG-1 Deploy the onboarding flow/ })).toHaveTextContent('✓')
    expect(card.getByTestId('now-line')).toBeInTheDocument()
    expect(card.queryByTestId('plan-progress')).toBeNull() // no done/planned/free bar on Today
    expect(card.queryByText(/planned/)).toBeNull()
    expect(card.getByText(/Picked for today, no time yet/)).toBeInTheDocument()
    expect(card.getByRole('button', { name: /Read the inbox/ })).toBeInTheDocument()
    expect(card.getByRole('link', { name: /Edit in Planner/ })).toHaveAttribute('href', '#/planner?plan=t')
    expect(screen.queryByText('Tomorrow thing')).toBeNull()
  })

  it('shows plan tabs only when today has more than one plan, and switches between them', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Lunch + walk', 13, 1)]), plan('u', '2026-10-29', [item('b', 'Afternoon thing', 15, 1)])])
    render(<Today />)
    const tabs = await screen.findAllByRole('tab')
    expect(tabs.map((t) => t.textContent)).toEqual([expect.stringContaining('Plan t'), expect.stringContaining('Plan u')])
    await user.click(tabs[1])
    expect(screen.getByRole('tab', { name: /Plan u/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('button', { name: /Afternoon thing/ })).toBeInTheDocument()
    expect(screen.queryByText('Lunch + walk')).toBeNull()
    expect(screen.getByRole('link', { name: /Edit in Planner/ })).toHaveAttribute('href', '#/planner?plan=u')
  })

  it('does not frame the item happening now, in the calendar or the list', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Get the spray', 10.5, 1)])]) // on at 11:00
    render(<Today />)
    const block = await screen.findByRole('button', { name: /Get the spray/ })
    expect(block.className).not.toMatch(/outline/)
    await user.click(screen.getByRole('button', { name: 'List' }))
    const row = within(screen.getByRole('list', { name: 'Plan items' })).getByRole('button', { name: /Get the spray/ })
    expect(row.className).not.toMatch(/inset_0_0_0_2px/)
  })

  it('lists the plan in time order, then what has no time yet', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([
      plan('t', '2026-10-29', [item('a', 'Lunch + walk', 13, 1), item('b', 'Read the inbox', null, 0.5), item('c', 'Deploy the onboarding flow', 9.5, 3)]),
    ])
    render(<Today />)
    await user.click(await screen.findByRole('button', { name: 'List' }))
    expect(screen.getByRole('button', { name: 'List' })).toHaveAttribute('aria-pressed', 'true')
    const list = within(screen.getByRole('list', { name: 'Plan items' }))
    expect(list.getAllByRole('button').map((b) => b.textContent)).toEqual([
      expect.stringContaining('Deploy the onboarding flow'),
      expect.stringContaining('Lunch + walk'),
      expect.stringContaining('Read the inbox'),
    ])
    expect(list.getByText('Scheduled · 2')).toBeInTheDocument()
    expect(list.getByText('No time yet · 1')).toBeInTheDocument()
    expect(list.getByText('9:30-12:30')).toBeInTheDocument()
    expect(screen.queryByText(/Picked for today/)).toBeNull()
  })

  it('marks an own activity done through the items endpoint, then refetches', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Lunch + walk', 13, 1)])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Lunch \+ walk/ }))
    const menu = within(screen.getByRole('dialog'))
    expect(menu.getByText('13:00-14:00 · Work plan')).toBeInTheDocument()
    expect(menu.queryByText(/in Linear/)).toBeNull()
    const calls = fetchPlans.mock.calls.length
    await user.click(menu.getByRole('button', { name: 'Mark done' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/items/a/done', expect.objectContaining({ method: 'POST', body: JSON.stringify({ done: true }) }))
    await waitFor(() => expect(fetchPlans.mock.calls.length).toBeGreaterThan(calls))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('marks a ticket done through the tickets endpoint and says it moves in the tracker', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [ticket(item('c', 'Deploy the onboarding flow', 14, 2), 'ENG-1', 'lin-1')])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Deploy the onboarding flow/ }))
    const menu = within(screen.getByRole('dialog'))
    expect(menu.getByText('Also moves ENG-1 to Done in Linear.')).toBeInTheDocument()
    await user.click(menu.getByRole('button', { name: 'Mark done' }))
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/tickets/done',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ source: 'linear', id: 'lin-1', done: true }) }),
    )
  })

  it('opens a planned review on GitHub from the menu, and can still mark it done', async () => {
    const url = 'https://github.com/o/r/pull/9'
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [{ ...item('b', 'Read the categories', 14, 0.5), RefSource: 'github', RefID: url }])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Read the categories/ }))
    const menu = within(screen.getByRole('dialog'))
    expect(menu.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', url)
    expect(menu.getByRole('button', { name: /done/i })).toBeInTheDocument()
  })

  it('opens a ticket in its tracker from the menu, like the planner', async () => {
    const t = { ...ticket(item('c', 'Deploy the onboarding flow', 14, 2), 'ENG-1', 'lin-1'), RefURL: 'https://linear.app/x/issue/ENG-1' }
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [t])])
    fetchToday.mockResolvedValue({
      ...base,
      StartedToday: [{ Source: 'jira', ID: 'OPS-231', Key: 'OPS-231', Title: 'Rotate the staging password', URL: 'https://x.atlassian.net/browse/OPS-231', Project: '', State: 'In Progress', Stage: 'started', StartedAt: iso('2026-10-29', 10.5), Hours: 2 }],
    })
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Deploy the onboarding flow/ }))
    const link = within(screen.getByRole('dialog')).getByRole('link', { name: 'Open in Linear' })
    expect(link).toHaveAttribute('href', 'https://linear.app/x/issue/ENG-1')
    expect(link).toHaveAttribute('target', '_blank')
    fireEvent.keyDown(document, { key: 'Escape' })

    const strip = within(screen.getByRole('region', { name: /Started today, not in your plan/ }))
    fireEvent.click(strip.getByRole('button', { name: /Rotate the staging password/ }))
    expect(within(screen.getByRole('dialog')).getByRole('link', { name: 'Open in Jira' })).toHaveAttribute('href', 'https://x.atlassian.net/browse/OPS-231')
  })

  it('offers to mark a finished ticket not done', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [ticket(item('c', 'Deploy the onboarding flow', 14, 2), 'ENG-1', 'lin-1', 'completed')])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Deploy the onboarding flow/ }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Mark not done' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/tickets/done', expect.objectContaining({ body: JSON.stringify({ source: 'linear', id: 'lin-1', done: false }) }))
  })

  it('shows a short error in the menu when the tracker refuses', async () => {
    const user = userEvent.setup()
    fetchMock.mockImplementation(async () => new Response('linear refused the move', { status: 502 }))
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [ticket(item('c', 'Deploy the onboarding flow', 14, 2), 'ENG-1', 'lin-1')])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Deploy the onboarding flow/ }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Mark done' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('linear refused the move')
  })

  it('closes the menu on Escape and on a click outside it', async () => {
    const user = userEvent.setup()
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Lunch + walk', 13, 1)])])
    render(<Today />)
    const block = await screen.findByRole('button', { name: /Lunch \+ walk/ })
    fireEvent.click(block)
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(block)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(screen.getByText('proj-run'))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  describe('editing the calendar', () => {
    // jsdom has no layout: the timeline reads 56px an hour from 7:00, and only the
    // row named here sits under the pointer, 1300px wide for 7:00-20:00 (100px an
    // hour). Mouse events under pointer names keep the coordinates.
    const layout = (row: string) => {
      const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
        return this.dataset.row === row ? new DOMRect(0, 0, 1300, 100) : new DOMRect(-500, -500, 1, 1)
      })
      onTestFinished(() => box.mockRestore())
    }
    const ev = (type: string, x: number, y = 50) => new MouseEvent(type, { bubbles: true, clientX: x, clientY: y })
    const drag = (el: Element, from: number, to: number) => {
      fireEvent(el, ev('pointerdown', from))
      fireEvent(document, ev('pointermove', to))
      fireEvent(document, ev('pointerup', to))
    }
    const twoRows = (items: PlanItem[]) => {
      const p = plan('t', '2026-10-29', items)
      return { ...p, Rows: [...p.Rows!, { ID: 'r2', PlanID: 't', Name: 'Errands', Order: 1 }] }
    }

    // DEV-15: the preview is the task's own length, and the task lands where it shows.
    it('shows a chip its own length on the row, and drops it right there', async () => {
      fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Write the report', null, 2)])])
      render(<Today />)
      const chip = await screen.findByRole('button', { name: /Write the report/ })
      layout('r1')
      fireEvent(chip, ev('pointerdown', 10))
      fireEvent(document, ev('pointermove', 650)) // 13:30
      const preview = within(screen.getByTestId('row-r1')).getByTestId('drop-preview')
      expect(preview).toHaveTextContent('12:30-14:30')
      expect(screen.queryByTestId('drag-ghost')).toBeNull()
      fireEvent(document, ev('pointerup', 650))
      expect(updateItem).toHaveBeenCalledWith('a', { rowId: 'r1', text: 'Write the report', start: iso('2026-10-29', 12.5), intendedHours: 2 })
      expect(screen.queryByTestId('drop-preview')).toBeNull()
    })

    it('moves a block to another time and row, saves it to the plan and refetches', async () => {
      fetchPlans.mockResolvedValue([twoRows([item('a', 'Lunch + walk', 13, 1)])])
      render(<Today />)
      const block = await screen.findByRole('button', { name: /Lunch \+ walk/ })
      layout('r2')
      const calls = fetchPlans.mock.calls.length
      drag(block, 100, 212) // 112px right is two hours later
      expect(updateItem).toHaveBeenCalledWith('a', { rowId: 'r2', text: 'Lunch + walk', start: iso('2026-10-29', 15), intendedHours: 1 })
      await waitFor(() => expect(fetchPlans.mock.calls.length).toBeGreaterThan(calls))
      fireEvent.click(block) // the click that ends a drag does not open the menu
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('resizes a block by its right edge', async () => {
      fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Lunch + walk', 13, 1)])])
      render(<Today />)
      const block = await screen.findByRole('button', { name: /Lunch \+ walk/ })
      layout('r1')
      drag(block.querySelector('[data-grab]')!, 400, 476) // 476px is 15:30 at 56px an hour from 7:00
      expect(updateItem).toHaveBeenCalledWith('a', { rowId: 'r1', text: 'Lunch + walk', start: iso('2026-10-29', 13), intendedHours: 2.5 })
      fireEvent.click(block)
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('gives a task picked without a time a start by dropping it on a row', async () => {
      fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('b', 'Read the inbox', null, 0.5)])])
      render(<Today />)
      const chip = await screen.findByRole('button', { name: /Read the inbox/ })
      layout('r1')
      drag(chip, 100, 1025) // 17:15 under the pointer, the chip centred on it
      expect(updateItem).toHaveBeenCalledWith('b', { rowId: 'r1', text: 'Read the inbox', start: iso('2026-10-29', 17), intendedHours: 0.5 })
      fireEvent.click(chip)
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('adds a ticket started today to the plan at the time it is dropped on', async () => {
      fetchToday.mockResolvedValue({
        ...base,
        StartedToday: [{ Source: 'jira', ID: 'OPS-231', Key: 'OPS-231', Title: 'Rotate the staging password', URL: '', Project: '', State: 'In Progress', Stage: 'started', StartedAt: iso('2026-10-29', 10.5), Hours: 2 }],
      })
      fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [])])
      render(<Today />)
      const chip = await screen.findByRole('button', { name: /Rotate the staging password/ })
      layout('r1')
      drag(chip, 100, 1025)
      expect(addItem).toHaveBeenCalledWith('t', { rowId: 'r1', refSource: 'jira', refId: 'OPS-231', text: 'Rotate the staging password', start: iso('2026-10-29', 16.25), intendedHours: 2 })
    })

    it('shows none of the planner-only controls', async () => {
      fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [item('a', 'Lunch + walk', 13, 1)])])
      render(<Today />)
      const card = within(await screen.findByRole('region', { name: "Today's plan" }))
      await card.findByText('Lunch + walk')
      for (const t of ['Start', 'End', '+ Add row']) expect(card.queryByText(t)).toBeNull()
      expect(card.queryByRole('textbox')).toBeNull()
      expect(card.queryByRole('button', { name: /Remove row|Send|Delete/ })).toBeNull()
      expect(card.getByText('Work plan')).toBeInTheDocument()
      fireEvent.doubleClick(card.getByTestId('row-r1'), { clientX: 0 })
      expect(addItem).not.toHaveBeenCalled()
    })
  })

  it('adds a ticket started today outside the plan to the selected plan', async () => {
    const user = userEvent.setup()
    fetchToday.mockResolvedValue({
      ...base,
      StartedToday: [{ Source: 'jira', ID: 'OPS-231', Key: 'OPS-231', Title: 'Rotate the staging password', URL: '', Project: '', State: 'In Progress', Stage: 'started', StartedAt: iso('2026-10-29', 10.5), Hours: 2 }],
    })
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [])])
    render(<Today />)
    const strip = within(await screen.findByRole('region', { name: /Started today, not in your plan/ }))
    expect(strip.getByText('since 10:30')).toBeInTheDocument()
    await user.click(strip.getByRole('button', { name: '+ Add to plan' }))
    expect(addItem).toHaveBeenCalledWith('t', { rowId: '', refSource: 'jira', refId: 'OPS-231', text: 'Rotate the staging password', start: null, intendedHours: 2 })
    fireEvent.click(strip.getByRole('button', { name: /Rotate the staging password/ }))
    expect(within(screen.getByRole('dialog')).getByText('Also moves OPS-231 to Done in Jira.')).toBeInTheDocument()
  })

  it('lists what got finished today in two columns: tickets and other tasks', async () => {
    fetchToday.mockResolvedValue({
      ...base,
      FinishedToday: [
        { Source: 'jira', ID: 'OPS-7', Key: 'OPS-7', Title: 'Rotate the keys', URL: 'https://example.atlassian.net/browse/OPS-7', Project: 'Platform', CompletedAt: iso('2026-10-29', 10.5) },
        { Source: 'linear', ID: 'l1', Key: 'ENG-1', Title: 'Fix the bug', URL: 'https://linear.app/x/issue/ENG-1', Project: '', CompletedAt: iso('2026-10-29', 9) },
      ],
      ReviewsDone: [{ Title: 'Data example service', Repo: 'x', URL: 'https://github.com/x/pull/1', SubmittedAt: iso('2026-10-29', 8.5) }],
    })
    fetchPlans.mockResolvedValue([plan('t', '2026-10-29', [{ ...item('a', 'Get the spray', null, 1), DoneAt: iso('2026-10-29', 10.25) }])])
    render(<Today />)
    const section = within(await screen.findByRole('region', { name: 'Finished today' }))
    await waitFor(() => expect(section.getByText('Finished today (4)')).toBeInTheDocument())
    const tickets = within(section.getByRole('group', { name: 'Tickets' }))
    const link = tickets.getByRole('link', { name: 'Rotate the keys' })
    expect(link).toHaveAttribute('href', 'https://example.atlassian.net/browse/OPS-7')
    expect(link).toHaveAttribute('target', '_blank')
    for (const t of ['OPS-7', 'ENG-1']) expect(tickets.getByText(t)).toBeInTheDocument()
    // Newest first still, but with no times.
    expect(tickets.getAllByRole('link').map((e) => e.textContent)).toEqual(['Rotate the keys', 'Fix the bug'])
    expect(tickets.queryByText('10:30')).toBeNull()
    expect(tickets.queryByText('9:00')).toBeNull()
    const other = within(section.getByRole('group', { name: 'Other tasks' }))
    expect(other.getAllByText(/Get the spray|Data example service/).map((e) => e.textContent)).toEqual(['Get the spray', 'Data example service'])
    expect(other.getByText('PR review')).toBeInTheDocument()
    expect(other.getByText('Work plan')).toBeInTheDocument()
    expect(other.queryByText('10:15')).toBeNull()
    expect(other.queryByText('8:30')).toBeNull()
  })

  it('no longer lists the reviews requested of you', async () => {
    render(<Today />)
    await screen.findByText('proj-run')
    expect(screen.queryByText(/Reviews requested of you/)).toBeNull()
    expect(screen.queryByText('Please review me')).toBeNull()
  })

  it('says there is no plan for today and links to the planner', async () => {
    fetchPlans.mockResolvedValue([plan('x', '2026-10-30', [])])
    render(<Today />)
    await waitFor(() => expect(screen.getByText(/No plan for today\./)).toBeInTheDocument())
    expect(screen.getByRole('link', { name: /planner/i })).toHaveAttribute('href', '#/planner')
  })
})

describe('Your PRs on Today', () => {
  const url = 'https://x/pull/1'
  const pr = (over: Partial<YourPR> = {}): YourPR => ({
    Source: 'github', RefID: `own:${url}`, Title: 'Retry uploads', Repo: 'o/r', URL: url, ReviewState: 'changes_requested', Reviewer: 'alex',
    ReviewedAt: new Date('2026-10-29T09:00').toISOString(), Hours: 0.5, Done: false, ...over,
  })
  const planned = (over: Partial<PlanItem> = {}): PlanItem =>
    ({ ...item('i9', 'Retry uploads', 14, 0.5), RefSource: 'github', RefID: `own:${url}`, RefState: 'approved', RefURL: url, ...over })
  const section = async () => within(await screen.findByRole('region', { name: 'Your PRs' }))
  const withPRs = (YourPRs: YourPR[] | null) => fetchToday.mockResolvedValue({ ...base, YourPRs })
  const posted = () => {
    const call = fetchMock.mock.calls.find(([u]) => u === '/api/own-prs/done')
    return call && JSON.parse(call[1].body)
  }
  beforeEach(() => localStorage.clear())

  it('lists them with their badge, reviewer and age', async () => {
    withPRs([pr(), pr({ RefID: 'own:b', URL: 'b', Title: 'Fix contrast', ReviewState: 'approved', Reviewer: 'sam' })])
    render(<Today />)
    const s = await section()
    expect(s.getByText('Your PRs · reviewed, need you (2)')).toBeInTheDocument()
    expect(s.getByText('Retry uploads')).toBeInTheDocument()
    expect(s.getByText('Changes requested')).toBeInTheDocument()
    expect(s.getByText('o/r · github · alex · 2h ago')).toBeInTheDocument()
    expect(s.getByText('Approved')).toBeInTheDocument()
  })

  it.each([[null], [[]]])('says all clear with %j', async (prs) => {
    withPRs(prs)
    render(<Today />)
    expect((await section()).getByText('Nothing needs you right now.')).toBeInTheDocument()
  })

  it('adds one to the first of today\'s plans, without a time', async () => {
    withPRs([pr()])
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', []), plan('p2', '2026-10-29', [])])
    render(<Today />)
    const s = await section()
    await userEvent.click(await s.findByRole('button', { name: 'Add Retry uploads to plan' }))
    expect(addItem).toHaveBeenCalledWith('p1', { rowId: '', refSource: 'github', refId: `own:${url}`, text: 'Retry uploads', start: null, intendedHours: 0.5 })
  })

  it('offers no second add for one already in today\'s plan', async () => {
    withPRs([pr()])
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', []), plan('p2', '2026-10-29', [planned()])])
    render(<Today />)
    const s = await section()
    expect(await s.findByText('In plan')).toBeInTheDocument()
    expect(s.queryByRole('button', { name: /Add .* to plan/ })).toBeNull()
  })

  it('has no add button without a plan today, but still opens the menu', async () => {
    withPRs([pr()])
    render(<Today />)
    const s = await section()
    expect(s.queryByRole('button', { name: /Add .* to plan/ })).toBeNull()
    await userEvent.click(s.getByRole('button', { name: /Retry uploads/ }))
    expect(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', url)
  })

  it('marks one done from its menu', async () => {
    withPRs([pr()])
    render(<Today />)
    const s = await section()
    await userEvent.click(s.getByRole('button', { name: /Retry uploads/ }))
    await userEvent.click(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('button', { name: 'Mark done' }))
    await waitFor(() => expect(posted()).toEqual({ URL: url, Done: true }))
  })

  it('keeps done ones behind Show done, with Mark not done', async () => {
    withPRs([pr({ Done: true })])
    render(<Today />)
    const s = await section()
    expect(s.queryByText('Retry uploads')).toBeNull()
    expect(s.getByText('Your PRs · reviewed, need you (0)')).toBeInTheDocument()
    await userEvent.click(s.getByRole('button', { name: 'Show done (1)' }))
    await userEvent.click(s.getByRole('button', { name: /Retry uploads/ }))
    await userEvent.click(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('button', { name: 'Mark not done' }))
    await waitFor(() => expect(posted()).toEqual({ URL: url, Done: false }))
  })

  it('shows a planned one that closed as done on the calendar, with no way to undo that', async () => {
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [planned({ RefState: '', RefURL: '', RefStage: 'completed' })])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Retry uploads/ }))
    const menu = within(screen.getByRole('dialog', { name: 'Retry uploads' }))
    expect(menu.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', url)
    expect(menu.getByText('No longer open on GitHub.')).toBeInTheDocument()
    expect(menu.queryByRole('button', { name: /done/i })).toBeNull()
  })

  it('marks a planned one through its PR, not its plan item', async () => {
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [planned()])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Retry uploads/ }))
    await userEvent.click(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('button', { name: 'Mark done' }))
    await waitFor(() => expect(posted()).toEqual({ URL: url, Done: true }))
    expect(fetchMock.mock.calls.some(([u]) => String(u).startsWith('/api/items/'))).toBe(false)
  })

  it('leaves a planned one out of the day in numbers and Finished today', async () => {
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [planned({ RefStage: 'completed' })])])
    render(<Today />)
    await screen.findByRole('button', { name: /Retry uploads/ })
    expect(tile('PR reviews finished').getByText('0')).toBeInTheDocument()
    expect(screen.getByText(/Finished today \(0\)/)).toBeInTheDocument()
  })

  it('names the source of each, and adds and opens it from there', async () => {
    withPRs([pr({ Source: 'gitlab' })])
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [])])
    render(<Today />)
    const s = await section()
    expect(s.getByText('o/r · gitlab · alex · 2h ago')).toBeInTheDocument()
    await userEvent.click(await s.findByRole('button', { name: 'Add Retry uploads to plan' }))
    expect(addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refSource: 'gitlab' }))
    await userEvent.click(s.getByRole('button', { name: /Retry uploads · |^Changes requested/ }))
    expect(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('link', { name: 'Open on GitLab' })).toHaveAttribute('href', url)
  })

  it('says where a planned one on another host closed', async () => {
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [planned({ RefSource: 'azure', RefState: '', RefURL: '', RefStage: 'completed' })])])
    render(<Today />)
    fireEvent.click(await screen.findByRole('button', { name: /Retry uploads/ }))
    const menu = within(screen.getByRole('dialog', { name: 'Retry uploads' }))
    expect(menu.getByRole('link', { name: 'Open on Azure DevOps' })).toHaveAttribute('href', url)
    expect(menu.getByText('No longer open on Azure DevOps.')).toBeInTheDocument()
  })

  it('takes one out of the plan from its row, and offers to add it back', async () => {
    withPRs([pr()])
    const inPlan = plan('p1', '2026-10-29', [planned()])
    fetchPlans.mockResolvedValue([inPlan])
    render(<Today />)
    const s = await section()
    fetchPlans.mockResolvedValue([{ ...inPlan, Items: [] }])
    await userEvent.click(await s.findByRole('button', { name: 'Remove Retry uploads from plan' }))
    expect(deleteItem).toHaveBeenCalledWith('i9')
    expect(await s.findByRole('button', { name: 'Add Retry uploads to plan' })).toBeInTheDocument()
    expect(s.queryByText('In plan')).toBeNull()
  })

  it('finds the item in whichever of today\'s plans holds it', async () => {
    withPRs([pr()])
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', []), plan('p2', '2026-10-29', [planned()])])
    render(<Today />)
    await userEvent.click(await (await section()).findByRole('button', { name: 'Remove Retry uploads from plan' }))
    expect(deleteItem).toHaveBeenCalledWith('i9')
  })

  it('has no remove button for one not in the plan', async () => {
    withPRs([pr()])
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [])])
    render(<Today />)
    const s = await section()
    await s.findByRole('button', { name: 'Add Retry uploads to plan' })
    expect(s.queryByRole('button', { name: /^Remove/ })).toBeNull()
  })

  describe('the saved section order', () => {
    const order = (c: HTMLElement) => [...c.querySelectorAll('[data-module]')].map((m) => m.getAttribute('data-module'))
    const key = 'devtopo.today.order'

    it('keeps an order saved before this section, and slots it in', async () => {
      localStorage.setItem(key, JSON.stringify(['sessions', 'numbers', 'plan', 'finished']))
      const { container } = render(<Today />)
      await section()
      expect(order(container)).toEqual(['sessions', 'numbers', 'plan', 'yourprs', 'finished'])
    })

    it('slots several missing sections into their default places', async () => {
      localStorage.setItem(key, JSON.stringify(['plan', 'finished', 'sessions']))
      const { container } = render(<Today />)
      await section()
      expect(order(container)).toEqual(['numbers', 'plan', 'yourprs', 'finished', 'sessions'])
    })

    it('keeps a full order as it is, and drops a name it no longer knows', async () => {
      localStorage.setItem(key, JSON.stringify(['plan', 'gone', 'yourprs', 'numbers', 'sessions', 'finished']))
      const { container } = render(<Today />)
      await section()
      expect(order(container)).toEqual(['plan', 'yourprs', 'numbers', 'sessions', 'finished'])
    })

    it('falls back to the default on a broken value', async () => {
      localStorage.setItem(key, 'not json')
      const { container } = render(<Today />)
      await section()
      expect(order(container)).toEqual(['numbers', 'sessions', 'plan', 'yourprs', 'finished'])
    })
  })
})

describe('Remove from plan in the calendar menu', () => {
  beforeEach(() => localStorage.clear())
  const open = async (text: string) => {
    fireEvent.click(await screen.findByRole('button', { name: new RegExp(text) }))
    return within(screen.getByRole('dialog', { name: text }))
  }

  it.each([
    ['a ticket', () => ticket(item('t', 'Fix the thing', 10, 1), 'ENG-1', 'a')],
    ['a review', () => ({ ...item('t', 'Fix the thing', 10, 1), RefSource: 'gitlab', RefID: 'https://x/1' })],
    ['an own PR', () => ({ ...item('t', 'Fix the thing', 10, 1), RefSource: 'github', RefID: 'own:https://x/1', RefURL: 'https://x/1' })],
    ['an activity', () => item('t', 'Fix the thing', 10, 1)],
  ])('takes %s out of the plan', async (_name, make) => {
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [make()])])
    render(<Today />)
    const menu = await open('Fix the thing')
    const calls = fetchPlans.mock.calls.length
    await userEvent.click(menu.getByRole('button', { name: 'Remove from plan' }))
    expect(deleteItem).toHaveBeenCalledWith('t')
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(fetchPlans.mock.calls.length).toBeGreaterThan(calls))
  })

  it('never moves a ticket in its tracker', async () => {
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [ticket(item('t', 'Fix the thing', 10, 1), 'ENG-1', 'a')])])
    render(<Today />)
    const menu = await open('Fix the thing')
    await userEvent.click(menu.getByRole('button', { name: 'Remove from plan' }))
    expect(fetchMock.mock.calls.some(([u]) => u === '/api/tickets/done')).toBe(false)
  })

  it('says when it could not remove one, and keeps the menu open', async () => {
    deleteItem.mockRejectedValue(new Error('nope'))
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [item('t', 'Fix the thing', 10, 1)])])
    render(<Today />)
    const menu = await open('Fix the thing')
    await userEvent.click(menu.getByRole('button', { name: 'Remove from plan' }))
    expect(await menu.findByRole('alert')).toHaveTextContent('nope')
  })

  it('is not offered for a ticket that is not in the plan', async () => {
    fetchToday.mockResolvedValue({ ...base, StartedToday: [{ Source: 'jira', ID: 'OPS-1', Key: 'OPS-1', Title: 'Started one', URL: '', Project: '', State: 'In Progress', Stage: 'started', StartedAt: iso('2026-10-29', 10.5), Hours: 2 }] })
    fetchPlans.mockResolvedValue([plan('p1', '2026-10-29', [])])
    render(<Today />)
    const menu = await open('Started one')
    expect(menu.queryByRole('button', { name: 'Remove from plan' })).toBeNull()
  })
})

describe('Your PRs as cards', () => {
  const pr = (id: string, Title: string): YourPR => ({
    Source: 'github', RefID: `own:https://x/${id}`, Title, Repo: 'o/r', URL: `https://x/${id}`, ReviewState: 'approved', Reviewer: 'alex',
    ReviewedAt: new Date('2026-10-29T09:00').toISOString(), Hours: 0.25, Done: false,
  })
  beforeEach(() => localStorage.clear())

  it('gives each PR its own card, like a session card, not rows of one block', async () => {
    fetchToday.mockResolvedValue({ ...base, YourPRs: [pr('1', 'First PR'), pr('2', 'Second PR')] })
    render(<Today />)
    const section = within(await screen.findByRole('region', { name: 'Your PRs' }))
    const cards = section.getAllByRole('listitem')
    expect(cards).toHaveLength(2)
    for (const c of cards) expect(c).toHaveClass('rounded-lg', 'border', 'bg-panel')
    // The list holding them is no box of its own.
    expect(cards[0].parentElement).not.toHaveClass('border')
    // Each card keeps what its row had.
    const first = within(cards[0])
    expect(first.getByText('Approved')).toBeInTheDocument()
    expect(first.getByText('First PR')).toBeInTheDocument()
    expect(first.getByText('o/r · github · alex · 2h ago')).toBeInTheDocument()
  })
})

describe('moving a section', () => {
  const order = (c: HTMLElement) => [...c.querySelectorAll('[data-module]')].map((m) => m.getAttribute('data-module'))
  const at = (c: HTMLElement, id: string) => c.querySelector(`[data-module="${id}"]`)!
  beforeEach(() => {
    localStorage.clear()
    // Every section is 100px high here, at 0-100: its middle is 50.
    const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 500, 100))
    onTestFinished(() => box.mockRestore())
  })
  const top = 10
  const bottom = 90
  // over drags over a section at a height; jsdom's drag events carry no pointer, so it is set here.
  const over = (el: Element, clientY: number) => {
    const ev = createEvent.dragOver(el)
    Object.defineProperty(ev, 'clientY', { value: clientY })
    fireEvent(el, ev)
  }

  it('makes room for it as it is dragged, so it shows where it will land', async () => {
    const { container } = render(<Today />)
    await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
    fireEvent.dragStart(screen.getByRole('button', { name: 'Move Sessions' }))
    expect(at(container, 'sessions')).toHaveAttribute('data-dragging', 'true')
    over(at(container, 'plan'), bottom)
    expect(order(container)).toEqual(['numbers', 'plan', 'sessions', 'yourprs', 'finished'])
    // Still in the bottom half of the plan: it stays put, with no flicking back.
    over(at(container, 'plan'), bottom)
    expect(order(container)).toEqual(['numbers', 'plan', 'sessions', 'yourprs', 'finished'])
    over(at(container, 'numbers'), top)
    expect(order(container)).toEqual(['sessions', 'numbers', 'plan', 'yourprs', 'finished'])
    // Nothing is kept until it is dropped.
    expect(localStorage.getItem('devtopo.today.order')).toBeNull()
    // No frame lights up on another section any more.
    expect(container.querySelector('.outline-dashed')).toBeNull()
    fireEvent.drop(at(container, 'sessions'))
    fireEvent.dragEnd(screen.getByRole('button', { name: 'Move Sessions' }))
    expect(order(container)).toEqual(['sessions', 'numbers', 'plan', 'yourprs', 'finished'])
    expect(JSON.parse(localStorage.getItem('devtopo.today.order')!)).toEqual(['sessions', 'numbers', 'plan', 'yourprs', 'finished'])
    expect(at(container, 'sessions')).not.toHaveAttribute('data-dragging')
  })

  it('goes back where it was when the drag is let go elsewhere', async () => {
    const { container } = render(<Today />)
    await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
    const handle = screen.getByRole('button', { name: 'Move Finished today' })
    fireEvent.dragStart(handle)
    over(at(container, 'numbers'), top)
    expect(order(container)[0]).toBe('finished')
    fireEvent.dragEnd(handle)
    expect(order(container)).toEqual(['numbers', 'sessions', 'plan', 'yourprs', 'finished'])
    expect(localStorage.getItem('devtopo.today.order')).toBeNull()
  })

  it('leaves the sections alone while a chip is dragged to the calendar', async () => {
    const { container } = render(<Today />)
    await waitFor(() => expect(screen.getByText('proj-run')).toBeInTheDocument())
    over(at(container, 'numbers'), top)
    expect(order(container)).toEqual(['numbers', 'sessions', 'plan', 'yourprs', 'finished'])
  })
})
