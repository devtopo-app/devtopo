import { describe, it, expect, vi, beforeEach, onTestFinished } from 'vitest'
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Planner } from './Planner'
import * as api from '../planner-api'
import type { Plan, Plannable } from '../planner-api'

vi.mock('../planner-api', () => ({
  fetchPlans: vi.fn(),
  fetchPlannable: vi.fn(),
  refreshConnectors: vi.fn(),
  createPlan: vi.fn(),
  updatePlan: vi.fn(),
  deletePlan: vi.fn(),
  addRow: vi.fn(),
  renameRow: vi.fn(),
  deleteRow: vi.fn(),
  addItem: vi.fn(),
  updateItem: vi.fn(),
  deleteItem: vi.fn(),
  markTicketDone: vi.fn(),
  markItemDone: vi.fn(),
  setReviewHidden: vi.fn(),
  setOwnPRDone: vi.fn(),
}))

const m = api as unknown as Record<keyof typeof api, ReturnType<typeof vi.fn>>

// A local-time ISO string, so the test reads the same in any time zone.
const at = (date: string, h: number) => new Date(`${date}T00:00`).getTime() + h * 3600 * 1000
const iso = (date: string, h: number) => new Date(at(date, h)).toISOString()

const plan: Plan = {
  ID: 'p1',
  Name: 'Thursday focus',
  WindowStart: iso('2026-10-29', 9),
  WindowEnd: iso('2026-10-29', 18),
  CreatedAt: '',
  Status: 'active',
  MakerHours: 8.5,
  MeetingHours: 0.5,
  Meetings: [{ Title: 'Standup', Start: iso('2026-10-29', 9), End: iso('2026-10-29', 9.5) }],
  Rows: [
    { ID: 'r1', PlanID: 'p1', Name: 'Work plan', Order: 0 },
    { ID: 'r2', PlanID: 'p1', Name: 'Private activities', Order: 1 },
  ],
  Items: [
    { ID: 'i1', PlanID: 'p1', RowID: 'r1', RefSource: 'linear', RefID: 'a', Text: 'Deploy the onboarding flow', Start: iso('2026-10-29', 9.5), IntendedHours: 3, RefState: 'In Progress', RefStage: 'started', RefRepo: 'ENG-142', RefURL: 'https://linear.app/acme/issue/ENG-142', RefProject: 'Onboarding', RefLabels: ['deploy'], DoneAt: null },
    { ID: 'i2', PlanID: 'p1', RowID: 'r2', RefSource: '', RefID: '', Text: 'Lunch + walk', Start: iso('2026-10-29', 12.5), IntendedHours: 1, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null },
    { ID: 'i3', PlanID: 'p1', RowID: '', RefSource: 'github', RefID: 'u3', Text: 'Run upload tests', Start: null, IntendedHours: 0.5, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null },
  ],
}

const plannable: Plannable[] = [
  { RefSource: 'linear', RefID: 'a', Text: 'Deploy the onboarding flow', Repo: 'ENG-142', URL: 'https://linear.app/acme/issue/ENG-142', Kind: 'task', Hours: 3, State: 'In Progress', Stage: 'started', Project: 'Onboarding', Labels: ['deploy'] },
  { RefSource: 'linear', RefID: 'b', Text: 'Cache search results', Repo: 'ENG-137', URL: 'https://linear.app/acme/issue/ENG-137', Kind: 'task', Hours: 2, State: 'In Review', Stage: 'started', Project: 'Onboarding', Labels: ['perf', 'cache', 'llm', 'cost'] },
  { RefSource: 'jira', RefID: 'OPS-218', Text: 'Upgrade the ingestion worker', Repo: 'OPS-218', URL: 'https://acme.atlassian.net/browse/OPS-218', Kind: 'task', Hours: 2, State: 'To Do', Stage: 'unstarted', Project: '', Labels: [] },
  { RefSource: 'github', RefID: 'u1', Text: 'Review the retry logic', Repo: 'api', URL: '', Kind: 'review_request', Hours: 0.5, State: '', Stage: '', Project: '', Labels: [] },
  { RefSource: 'github', RefID: 'u3', Text: 'Run upload tests', Repo: 'webapp', URL: '', Kind: 'review_request', Hours: 0.5, State: '', Stage: '', Project: '', Labels: [] },
]

beforeEach(() => {
  vi.clearAllMocks()
  m.fetchPlans.mockResolvedValue([plan])
  m.fetchPlannable.mockResolvedValue(plannable)
  m.refreshConnectors.mockResolvedValue(undefined)
  for (const k of ['createPlan', 'updatePlan', 'deletePlan', 'addRow', 'renameRow', 'deleteRow', 'addItem', 'updateItem', 'deleteItem'] as const) {
    m[k].mockResolvedValue({})
  }
})

const tray = (name: RegExp) => screen.getByRole('region', { name })

describe('Planner', () => {
  it('refreshes connectors when the planner opens, then refetches', async () => {
    render(<Planner />)
    await waitFor(() => expect(m.refreshConnectors).toHaveBeenCalled())
    await waitFor(() => expect(m.fetchPlans).toHaveBeenCalledTimes(2))
  })

  it('opens on the plan the link names', async () => {
    m.fetchPlans.mockResolvedValue([plan, { ...plan, ID: 'p2', Name: 'Afternoon focus' }])
    window.location.hash = '#/planner?plan=p2'
    onTestFinished(() => {
      window.location.hash = ''
    })
    render(<Planner />)
    await waitFor(() => expect(screen.getByRole('tab', { name: /Afternoon focus/ })).toHaveAttribute('aria-selected', 'true'))
  })

  it('shows a calm empty state with a way to start', async () => {
    m.fetchPlans.mockResolvedValue([])
    render(<Planner />)
    await waitFor(() => expect(screen.getByText(/Nothing planned yet/)).toBeInTheDocument())
    expect(screen.getByRole('button', { name: /\+ New plan/ })).toBeInTheDocument()
  })

  it('renders the plan tab, the calendar, the rows and their blocks', async () => {
    render(<Planner />)
    await waitFor(() => expect(screen.getByRole('tab', { name: /Thursday focus/ })).toBeInTheDocument())
    expect(screen.getByText('Standup')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Work plan')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Private activities')).toBeInTheDocument()
    expect(screen.getAllByText('Lunch + walk')).toHaveLength(2) // on its row and in Activities
    expect(screen.getByText('Start')).toBeInTheDocument()
    expect(screen.getByText('End')).toBeInTheDocument()
    // 3h + 1h placed + 0.5h anytime, of 9h window - 0.5h meetings
    expect(screen.getByText(/4\.5h/)).toBeInTheDocument()
    expect(screen.getByText(/8\.5h/)).toBeInTheDocument()
  })

  describe('the Reviews tray', () => {
    const review = (RefID: string, over: Partial<Plannable> = {}): Plannable => ({ RefSource: 'github', RefID, Text: `PR ${RefID}`, Repo: 'api', URL: '', Kind: 'review_request', Hours: 1, State: '', Stage: '', Project: '', Labels: [], ...over })
    const reviewTray = async (items: Plannable[]) => {
      m.fetchPlannable.mockResolvedValue(items)
      render(<Planner />)
      await waitFor(() => expect(within(tray(/Reviews/)).getByText(/^\d+ waiting, \d+ planned$/)).toBeInTheDocument())
      await waitFor(() => expect(m.fetchPlannable).toHaveBeenCalled())
      return within(tray(/Reviews/))
    }
    const press = (el: HTMLElement) => {
      fireEvent(el, new MouseEvent('pointerdown', { bubbles: true, clientX: 10, clientY: 10 }))
      fireEvent(document, new MouseEvent('pointerup', { bubbles: true, clientX: 10, clientY: 10 }))
      fireEvent.click(el)
    }

    it('opens a clicked review with a link to GitHub and no way to mark it done', async () => {
      const t = await reviewTray([review('a', { RefID: 'https://github.com/o/r/pull/1', URL: 'https://github.com/o/r/pull/1', Text: 'PR a' })])
      press(await t.findByText('PR a'))
      const menu = within(screen.getByRole('dialog', { name: 'PR a' }))
      const link = menu.getByRole('link', { name: 'Open on GitHub' })
      expect(link).toHaveAttribute('href', 'https://github.com/o/r/pull/1')
      expect(link).toHaveAttribute('target', '_blank')
      expect(menu.queryByRole('button', { name: /done/i })).toBeNull()
    })

    it('opens a hidden review the same way', async () => {
      const t = await reviewTray([review('b', { RefID: 'https://github.com/o/r/pull/2', URL: 'https://github.com/o/r/pull/2', Text: 'PR b', Hidden: true })])
      await userEvent.click(await t.findByRole('button', { name: 'Show hidden (1)' }))
      fireEvent.click(t.getByText('PR b'))
      expect(within(screen.getByRole('dialog', { name: 'PR b' })).getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', 'https://github.com/o/r/pull/2')
    })

    it('does not open the window from the Hide button', async () => {
      m.setReviewHidden.mockResolvedValue(undefined)
      const t = await reviewTray([review('a')])
      await userEvent.click(await t.findByRole('button', { name: 'Hide PR a' }))
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('links a review already in the plan by its id when its address is gone', async () => {
      const url = 'https://github.com/o/r/pull/3'
      m.fetchPlans.mockResolvedValue([{ ...plan, Items: [...(plan.Items ?? []), { ...plan.Items![0], ID: 'kept', RowID: '', RefSource: 'github', RefID: url, RefURL: '', Text: 'Kept review', Start: null, IntendedHours: 1 }] }])
      await reviewTray([])
      press(await screen.findByText('Kept review'))
      expect(within(screen.getByRole('dialog', { name: 'Kept review' })).getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', url)
    })

    it('shows the lines of a sized review and none for an unsized one', async () => {
      const t = await reviewTray([review('big', { Additions: 120, Deletions: 30, Sized: true }), review('unknown')])
      expect(await t.findByText('+120')).toHaveClass('text-done')
      expect(t.getByText('-30')).toHaveClass('text-danger')
      expect(t.getAllByText(/^\+\d+$/)).toHaveLength(1)
      expect(t.getByText('PRs waiting on you, estimated from their size - pull the edge to resize.')).toBeInTheDocument()
    })

    it('hides a review with its button', async () => {
      m.setReviewHidden.mockResolvedValue(undefined)
      const t = await reviewTray([review('a', { RefID: 'https://x/pull/1', Text: 'PR a' })])
      await userEvent.click(await t.findByRole('button', { name: 'Hide PR a' }))
      expect(m.setReviewHidden).toHaveBeenCalledWith('https://x/pull/1', true)
      await waitFor(() => expect(m.fetchPlannable.mock.calls.length).toBeGreaterThan(1))
    })

    it('offers no Show hidden when nothing is hidden', async () => {
      const t = await reviewTray([review('a')])
      await t.findByText('PR a')
      expect(t.queryByText(/hidden/)).toBeNull()
    })

    it('counts only what is waiting, and opens the hidden ones on request', async () => {
      m.setReviewHidden.mockResolvedValue(undefined)
      const t = await reviewTray([review('a'), review('b', { Hidden: true }), review('c', { Hidden: true })])
      expect(await t.findByText(/^1 waiting,/)).toBeInTheDocument()
      expect(t.queryByText('PR b')).toBeNull()
      await userEvent.click(t.getByRole('button', { name: 'Show hidden (2)' }))
      expect(t.getByText('PR b')).toBeInTheDocument()
      expect(t.getByRole('button', { name: 'Hide hidden (2)' })).toBeInTheDocument()
      await userEvent.click(t.getByRole('button', { name: 'Unhide PR b' }))
      expect(m.setReviewHidden).toHaveBeenCalledWith('b', false)
    })

    it('does not let a hidden review be dragged', async () => {
      const t = await reviewTray([review('b', { Hidden: true })])
      await userEvent.click(await t.findByRole('button', { name: 'Show hidden (1)' }))
      expect(t.getByText('PR b').closest('.cursor-grab')).toBeNull()
    })

    it('says all hidden when every review is hidden', async () => {
      const t = await reviewTray([review('b', { Hidden: true })])
      expect(await t.findByText('all hidden')).toBeInTheDocument()
      expect(t.queryByText('all placed in this plan')).toBeNull()
    })
  })

  it('keeps reviews and tickets in their own trays and leaves out what is placed', async () => {
    render(<Planner />)
    await waitFor(() => expect(tray(/Reviews/)).toBeInTheDocument())
    const reviews = within(tray(/Reviews/))
    const tickets = within(tray(/Tickets/))
    const activities = within(tray(/Activities/))
    expect(reviews.getByText('Review the retry logic')).toBeInTheDocument()
    expect(reviews.queryByText('Upgrade the ingestion worker')).toBeNull()
    expect(tickets.getByText('Upgrade the ingestion worker')).toBeInTheDocument()
    expect(tickets.getByText('Cache search results')).toBeInTheDocument()
    expect(within(reviews.getByRole('group', { name: /Not planned/ })).getByText('Review the retry logic')).toBeInTheDocument()
    expect(within(reviews.getByRole('group', { name: /In this plan/ })).getByText('Run upload tests')).toBeInTheDocument() // kept for anytime
    expect(activities.queryByText('Run upload tests')).toBeNull()
  })

  it('lists own activities by row name instead of an anytime tray', async () => {
    render(<Planner />)
    const activities = within(await screen.findByRole('region', { name: /Activities/ }))
    expect(screen.queryByRole('region', { name: /Anytime this day/ })).toBeNull()
    const priv = within(activities.getByRole('group', { name: /Private activities/ }))
    expect(priv.getByText('Lunch + walk')).toBeInTheDocument()
    expect(priv.getByText(/12:30-13:30/)).toBeInTheDocument()
    expect(within(activities.getByRole('group', { name: /Work plan/ })).queryByText('Deploy the onboarding flow')).toBeNull() // tickets stay in Tickets
  })

  it('adds a task without a time to a row with its plus', async () => {
    render(<Planner />)
    await userEvent.click(await screen.findByRole('button', { name: 'Add a task to Private activities' }))
    await userEvent.type(screen.getByLabelText('New task in Private activities'), 'Buy gifts{Enter}')
    expect(m.addItem).toHaveBeenCalledWith('p1', { rowId: 'r2', text: 'Buy gifts', start: null, intendedHours: 1 })
  })

  it('shows the tickets in this plan first, marked with where they sit', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    const inPlan = tickets.getByRole('group', { name: /In this plan/ })
    const notPlanned = tickets.getByRole('group', { name: /Not planned/ })
    expect(within(inPlan).getByText('Deploy the onboarding flow')).toBeInTheDocument()
    expect(within(inPlan).getByText(/9:30-12:30/)).toBeInTheDocument()
    expect(within(notPlanned).queryByText('Deploy the onboarding flow')).toBeNull()
    expect(within(notPlanned).getByText('Cache search results')).toBeInTheDocument()
    expect(inPlan.compareDocumentPosition(notPlanned) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(tickets.getByText(/2 open, 1 planned/)).toBeInTheDocument()
  })

  // DEV-18: a ticket card shows its status in its colour, with the status and
  // labels at the end of its second line, after where it is from.
  it('colours a ticket card by its status, its status and labels on the right', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    const card = (name: string) => tickets.getByText(name).closest<HTMLElement>('[data-card]')!
    expect(card('Deploy the onboarding flow').style.borderLeftColor).toBe('var(--waiting)')
    expect(card('Cache search results').style.borderLeftColor).toBe('var(--done)')
    const review = within(card('Cache search results'))
    const pill = review.getByText('In Review')
    expect(pill).toHaveClass('text-2xs')
    expect(pill.style.background).toContain('var(--done)')
    // Its other labels stay plain grey.
    expect(review.getByText('perf').style.background).toBe('')
    expect(review.getByText('perf')).toHaveClass('border-border', 'text-2xs')
    expect(review.getByText(/ENG-137/).compareDocumentPosition(pill) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(pill.parentElement).toHaveClass('ml-auto')
    // On the calendar a ticket keeps the ticket colour, whatever its status.
    const block = screen.getAllByText('Deploy the onboarding flow').map((e) => e.closest<HTMLElement>('.blk')).find(Boolean)!
    expect(block.style.borderLeftColor).toBe('var(--accent)')
  })

  it('keeps a planned ticket finished since in the plan group, marked Done or Canceled', async () => {
    const finished = (ID: string, Text: string, RefStage: string) =>
      ({ ID, PlanID: 'p1', RowID: '', RefSource: 'jira', RefID: ID, Text, Start: null, IntendedHours: 1, RefState: 'Closed', RefStage, RefRepo: ID, RefURL: '', RefProject: 'Platform', RefLabels: ['sec'], DoneAt: null })
    m.fetchPlans.mockResolvedValue([{ ...plan, Items: [...plan.Items!, finished('OPS-7', 'Rotate the keys', 'completed'), finished('OPS-8', 'Drop the old queue', 'canceled')] }])
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    const inPlan = within(tickets.getByRole('group', { name: /In this plan/ }))
    const notPlanned = within(tickets.getByRole('group', { name: /Not planned/ }))
    expect(inPlan.getByText('Rotate the keys')).toHaveClass('line-through')
    expect(inPlan.getByText('Done')).toBeInTheDocument()
    expect(inPlan.getByText('OPS-7 · Platform · jira')).toBeInTheDocument() // no longer plannable, still described
    expect(inPlan.getAllByText('sec')).toHaveLength(2)
    expect(inPlan.getByText('Canceled')).toBeInTheDocument()
    expect(notPlanned.queryByText('Rotate the keys')).toBeNull()
    expect(tickets.getByText(/2 open, 3 planned/)).toBeInTheDocument()
  })

  it('shows each ticket status as a pill', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    expect(tickets.getByText('In Progress')).toBeInTheDocument()
    expect(tickets.getByText('In Review')).toBeInTheDocument()
    expect(tickets.getByText('To Do')).toBeInTheDocument()
  })

  it('shows a ticket project and up to three label pills, planned or not', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    const inPlan = within(tickets.getByRole('group', { name: /In this plan/ }))
    const notPlanned = within(tickets.getByRole('group', { name: /Not planned/ }))
    expect(inPlan.getByText(/ENG-142 · Onboarding · linear/)).toBeInTheDocument()
    expect(inPlan.getByText('deploy')).toBeInTheDocument()
    for (const l of ['perf', 'cache', 'llm', '+1']) expect(notPlanned.getByText(l)).toBeInTheDocument()
    expect(notPlanned.queryByText('cost')).toBeNull()
  })

  it('drags an open ticket into the plan but not one already in it', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    // jsdom has no layout or PointerEvent: give every element the same box, so
    // (50, 50) lands on the first row, and send mouse events under pointer
    // names to keep the coordinates.
    const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 100, 100))
    onTestFinished(() => box.mockRestore())
    const ev = (type: string, xy: number) => new MouseEvent(type, { bubbles: true, clientX: xy, clientY: xy })
    const drag = (text: string) => {
      fireEvent(tickets.getByText(text), ev('pointerdown', 300))
      fireEvent(document, ev('pointermove', 50))
      fireEvent(document, ev('pointerup', 50))
    }
    drag('Deploy the onboarding flow')
    expect(m.addItem).not.toHaveBeenCalled()
    drag('Cache search results')
    expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refId: 'b' }))
    expect(screen.queryByRole('dialog')).toBeNull() // a drag is not a click
  })

  it('plans a ticket for anytime by dropping it on In this plan', async () => {
    m.fetchPlans.mockResolvedValue([{ ...plan, Items: [] }])
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    const inPlan = tickets.getByRole('group', { name: /In this plan/ }) // shown even when empty
    // Only the In this plan group sits under the pointer; rows and the
    // Activities tray are elsewhere.
    const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this === inPlan ? new DOMRect(0, 0, 100, 100) : new DOMRect(-500, -500, 1, 1)
    })
    onTestFinished(() => box.mockRestore())
    const ev = (type: string, xy: number) => new MouseEvent(type, { bubbles: true, clientX: xy, clientY: xy })
    fireEvent(tickets.getByText('Cache search results'), ev('pointerdown', 300))
    fireEvent(document, ev('pointermove', 50))
    fireEvent(document, ev('pointerup', 50))
    expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refId: 'b', start: null }))
  })

  describe('dropping on a group', () => {
    // Only the given element sits under the pointer.
    const onlyUnderPointer = (el: () => HTMLElement) => {
      const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
        return this === el() ? new DOMRect(0, 0, 100, 100) : new DOMRect(-500, -500, 1, 1)
      })
      onTestFinished(() => box.mockRestore())
    }
    const dragOntoIt = (from: HTMLElement) => {
      const ev = (type: string, xy: number) => new MouseEvent(type, { bubbles: true, clientX: xy, clientY: xy })
      fireEvent(from, ev('pointerdown', 300))
      fireEvent(document, ev('pointermove', 50))
      fireEvent(document, ev('pointerup', 50))
    }
    const anytimeActivity = { ...plan.Items![1], ID: 'i4', Text: 'Call the bank', Start: null }

    it('plans a review without a time on In this plan', async () => {
      render(<Planner />)
      const reviews = within(await screen.findByRole('region', { name: /Reviews/ }))
      onlyUnderPointer(() => reviews.getByRole('group', { name: /In this plan/ }))
      dragOntoIt(reviews.getByText('Review the retry logic'))
      expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refId: 'u1', start: null }))
    })

    it('takes a planned review back out on Not planned', async () => {
      render(<Planner />)
      const reviews = within(await screen.findByRole('region', { name: /Reviews/ }))
      onlyUnderPointer(() => reviews.getByRole('group', { name: /Not planned/ }))
      dragOntoIt(reviews.getByText('Run upload tests'))
      expect(m.deleteItem).toHaveBeenCalledWith('i3')
    })

    it('gives a planned review a time on the timeline, without adding it twice', async () => {
      render(<Planner />)
      const reviews = within(await screen.findByRole('region', { name: /Reviews/ }))
      await waitFor(() => expect(screen.getByTestId('row-r1')).toBeInTheDocument())
      onlyUnderPointer(() => screen.getByTestId('row-r1'))
      dragOntoIt(reviews.getByText('Run upload tests'))
      expect(m.updateItem).toHaveBeenCalledWith('i3', expect.objectContaining({ rowId: 'r1', start: expect.any(String) }))
      expect(m.addItem).not.toHaveBeenCalled()
    })

    // DEV-15: the preview is the task's own length, and the task lands where it shows.
    it('shows a task its own length on the row, and drops it right there', async () => {
      render(<Planner />)
      const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
      await waitFor(() => expect(screen.getByTestId('row-r1')).toBeInTheDocument())
      onlyUnderPointer(() => screen.getByTestId('row-r1'))
      const ev = (type: string, xy: number) => new MouseEvent(type, { bubbles: true, clientX: xy, clientY: xy })
      const card = tickets.getByText('Cache search results') // 2h
      fireEvent(card, ev('pointerdown', 300))
      fireEvent(document, ev('pointermove', 50)) // half of 7:00-20:00 is 13:30
      const preview = within(screen.getByTestId('row-r1')).getByTestId('drop-preview')
      expect(preview).toHaveTextContent('Cache search results')
      expect(preview).toHaveTextContent('12:30-14:30')
      expect(screen.queryByTestId('drag-ghost')).toBeNull()
      fireEvent(document, ev('pointerup', 50))
      expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refId: 'b', rowId: 'r1', start: iso('2026-10-29', 12.5), intendedHours: 2 }))
      expect(screen.queryByTestId('drop-preview')).toBeNull()
    })

    it('floats a card off the calendar, with no preview on a row', async () => {
      render(<Planner />)
      const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
      onlyUnderPointer(() => document.body)
      const ev = (type: string, xy: number) => new MouseEvent(type, { bubbles: true, clientX: xy, clientY: xy })
      fireEvent(tickets.getByText('Cache search results'), ev('pointerdown', 300))
      fireEvent(document, ev('pointermove', 50))
      expect(screen.getByTestId('drag-ghost')).toHaveTextContent('Cache search results')
      expect(screen.queryByTestId('drop-preview')).toBeNull()
      fireEvent(document, ev('pointerup', 50))
    })

    it('moves an activity without a time to another row', async () => {
      m.fetchPlans.mockResolvedValue([{ ...plan, Items: [anytimeActivity] }])
      render(<Planner />)
      const activities = within(await screen.findByRole('region', { name: /Activities/ }))
      onlyUnderPointer(() => activities.getByRole('group', { name: /Work plan/ }))
      dragOntoIt(activities.getByText('Call the bank'))
      expect(m.updateItem).toHaveBeenCalledWith('i4', { rowId: 'r1', text: 'Call the bank', start: null, intendedHours: 1 })
    })

    it('moves an activity with a time to another row, keeping the time', async () => {
      render(<Planner />)
      const activities = within(await screen.findByRole('region', { name: /Activities/ }))
      onlyUnderPointer(() => activities.getByRole('group', { name: /Work plan/ }))
      dragOntoIt(within(activities.getByRole('group', { name: /Private activities/ })).getByText('Lunch + walk'))
      expect(m.updateItem).toHaveBeenCalledWith('i2', { rowId: 'r1', text: 'Lunch + walk', start: iso('2026-10-29', 12.5), intendedHours: 1 })
    })

    it('moves a block from the timeline to another row in the list, keeping the time', async () => {
      render(<Planner />)
      const activities = within(await screen.findByRole('region', { name: /Activities/ }))
      const block = within(await screen.findByTestId('row-r2')).getByText('Lunch + walk')
      onlyUnderPointer(() => activities.getByRole('group', { name: /Work plan/ }))
      dragOntoIt(block)
      expect(m.updateItem).toHaveBeenCalledWith('i2', { rowId: 'r1', text: 'Lunch + walk', start: iso('2026-10-29', 12.5), intendedHours: 1 })
    })

    it('leaves an activity alone when dropped on its own row', async () => {
      m.fetchPlans.mockResolvedValue([{ ...plan, Items: [anytimeActivity] }])
      render(<Planner />)
      const activities = within(await screen.findByRole('region', { name: /Activities/ }))
      onlyUnderPointer(() => activities.getByRole('group', { name: /Private activities/ }))
      dragOntoIt(activities.getByText('Call the bank'))
      expect(m.updateItem).not.toHaveBeenCalled()
    })

    it('treats a ticket dropped on an activity row as planned without a time', async () => {
      render(<Planner />)
      const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
      const activities = within(screen.getByRole('region', { name: /Activities/ }))
      onlyUnderPointer(() => activities.getByRole('group', { name: /Work plan/ }))
      dragOntoIt(tickets.getByText('Cache search results'))
      expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refId: 'b', rowId: '', start: null }))
    })
  })

  it('takes a ticket out of the plan by dropping it on Not planned', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    const notPlanned = tickets.getByRole('group', { name: /Not planned/ })
    const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this === notPlanned ? new DOMRect(0, 0, 100, 100) : new DOMRect(-500, -500, 1, 1)
    })
    onTestFinished(() => box.mockRestore())
    const ev = (type: string, xy: number) => new MouseEvent(type, { bubbles: true, clientX: xy, clientY: xy })
    fireEvent(within(tickets.getByRole('group', { name: /In this plan/ })).getByText('Deploy the onboarding flow'), ev('pointerdown', 300))
    fireEvent(document, ev('pointermove', 50))
    fireEvent(document, ev('pointerup', 50))
    expect(m.deleteItem).toHaveBeenCalledWith('i1')
    expect(m.addItem).not.toHaveBeenCalled()
  })

  it('adds a row', async () => {
    render(<Planner />)
    await userEvent.click(await screen.findByRole('button', { name: /\+ Add row/ }))
    expect(m.addRow).toHaveBeenCalledWith('p1', 'New row')
  })

  it('renames a row when its name loses focus', async () => {
    render(<Planner />)
    const input = await screen.findByDisplayValue('Private activities')
    await userEvent.clear(input)
    await userEvent.type(input, 'Life{Enter}')
    expect(m.renameRow).toHaveBeenCalledWith('r2', 'Life')
  })

  it('removes a row', async () => {
    render(<Planner />)
    await screen.findByDisplayValue('Work plan')
    await userEvent.click(screen.getAllByRole('button', { name: /remove row/i })[1])
    expect(m.deleteRow).toHaveBeenCalledWith('r2')
  })

  it('creates a new plan for the day after the last one', async () => {
    render(<Planner />)
    await userEvent.click(await screen.findByRole('button', { name: /\+ New plan/ }))
    expect(m.createPlan).toHaveBeenCalledWith('Plan for Fri 30 Oct', iso('2026-10-30', 9), iso('2026-10-30', 18))
  })

  it('renames the plan by clicking the name in its tab', async () => {
    render(<Planner />)
    await userEvent.click(await screen.findByRole('tab', { name: /Thursday focus/ }))
    const input = screen.getByRole('textbox', { name: 'Plan name' })
    await userEvent.clear(input)
    await userEvent.type(input, 'Deep work{Enter}')
    expect(m.updatePlan).toHaveBeenCalledTimes(1)
    expect(m.updatePlan).toHaveBeenCalledWith('p1', 'Deep work', plan.WindowStart, plan.WindowEnd)
  })

  it('selects another plan on a click on its tab without renaming it', async () => {
    m.fetchPlans.mockResolvedValue([plan, { ...plan, ID: 'p2', Name: 'Afternoon focus' }])
    render(<Planner />)
    await userEvent.click(await screen.findByRole('tab', { name: /Afternoon focus/ }))
    expect(screen.getByRole('tab', { name: /Afternoon focus/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByRole('textbox', { name: 'Plan name' })).toBeNull()
  })

  it('keeps the plan name when its rename is cancelled with Escape or left empty', async () => {
    render(<Planner />)
    await userEvent.click(await screen.findByRole('tab', { name: /Thursday focus/ }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Plan name' }), ' later{Escape}')
    expect(screen.queryByRole('textbox', { name: 'Plan name' })).toBeNull()
    await userEvent.click(screen.getByRole('tab', { name: /Thursday focus/ }))
    await userEvent.clear(screen.getByRole('textbox', { name: 'Plan name' }))
    fireEvent.blur(screen.getByRole('textbox', { name: 'Plan name' }))
    expect(screen.getByRole('tab', { name: /Thursday focus/ })).toBeInTheDocument()
    expect(m.updatePlan).not.toHaveBeenCalled()
  })

  it('deletes a plan only on the second click', async () => {
    render(<Planner />)
    await userEvent.click(await screen.findByRole('button', { name: 'Delete plan' }))
    expect(m.deletePlan).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: /Click again to delete/ }))
    expect(m.deletePlan).toHaveBeenCalledWith('p1')
  })

  it('sends a block back to its tray', async () => {
    render(<Planner />)
    await screen.findByDisplayValue('Private activities')
    await userEvent.click(screen.getByRole('button', { name: /send Deploy the onboarding flow back/i }))
    expect(m.deleteItem).toHaveBeenCalledWith('i1')
  })

  it('adds an own activity on a double-click on empty row space', async () => {
    m.addItem.mockResolvedValue({ ID: 'new' })
    render(<Planner />)
    await screen.findByDisplayValue('Private activities')
    fireEvent.doubleClick(screen.getByTestId('row-r2'), { clientX: 0 })
    expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ rowId: 'r2', text: 'New activity', intendedHours: 1 }))
  })
})

describe('Planner ticket menu', () => {
  // A click is a pointer down and up on the same spot, with no move between.
  // A press and release without a move, then the click a browser sends after it
  // (timeline blocks open their menu on that click).
  const click = (el: HTMLElement) => {
    fireEvent(el, new MouseEvent('pointerdown', { bubbles: true, clientX: 10, clientY: 10 }))
    fireEvent(document, new MouseEvent('pointerup', { bubbles: true, clientX: 10, clientY: 10 }))
    fireEvent.click(el)
  }
  const notPlanned = async () => within(within(await screen.findByRole('region', { name: /Tickets/ })).getByRole('group', { name: /Not planned/ }))

  it('opens on a click on a tray ticket, with its details and a link to it', async () => {
    render(<Planner />)
    click((await notPlanned()).getByText('Cache search results'))
    const menu = within(screen.getByRole('dialog', { name: 'Cache search results' }))
    expect(menu.getByText('ENG-137 Cache search results')).toBeInTheDocument()
    expect(menu.getByText('In Review')).toBeInTheDocument()
    expect(menu.getByText('Onboarding')).toBeInTheDocument()
    const link = menu.getByRole('link', { name: 'Open in Linear' })
    expect(link).toHaveAttribute('href', 'https://linear.app/acme/issue/ENG-137')
    expect(link).toHaveAttribute('target', '_blank')
    expect(menu.getByText('Also moves ENG-137 to Done in Linear.')).toBeInTheDocument()
  })

  it('names Jira for a Jira ticket', async () => {
    render(<Planner />)
    click((await notPlanned()).getByText('Upgrade the ingestion worker'))
    const menu = within(screen.getByRole('dialog'))
    expect(menu.getByRole('link', { name: 'Open in Jira' })).toHaveAttribute('href', 'https://acme.atlassian.net/browse/OPS-218')
    expect(menu.getByText('Also moves OPS-218 to Done in Jira.')).toBeInTheDocument()
  })

  it('opens on a click on a ticket block on the timeline', async () => {
    render(<Planner />)
    await screen.findByDisplayValue('Work plan')
    click(screen.getByRole('button', { name: /ENG-142 Deploy the onboarding flow 9:30/ }))
    const menu = within(screen.getByRole('dialog', { name: 'Deploy the onboarding flow' }))
    expect(menu.getByRole('link', { name: 'Open in Linear' })).toHaveAttribute('href', 'https://linear.app/acme/issue/ENG-142')
  })

  it('opens on a click on a planned ticket in the tray', async () => {
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    click(within(tickets.getByRole('group', { name: /In this plan/ })).getByText('Deploy the onboarding flow'))
    expect(screen.getByRole('dialog', { name: 'Deploy the onboarding flow' })).toBeInTheDocument()
  })

  describe('on an own activity', () => {
    const lunch = plan.Items![1]
    const doneAt = iso('2026-10-29', 13)
    const withItems = (...Items: Plan['Items'] & object) => m.fetchPlans.mockResolvedValue([{ ...plan, Items }])
    const activities = async () => within(await screen.findByRole('region', { name: /Activities/ }))
    // cardOf is a tray card's hours box: a click there opens the menu, where a click
    // on the name renames.
    const cardOf = (t: { getByText: (text: string) => HTMLElement }, name: string) => (t.getByText(name).closest('[data-card]') as HTMLElement).firstElementChild as HTMLElement

    // DEV-16: delete any activity from the tray, and rename it by clicking its name.
    it('deletes an activity from the tray, also one with a time', async () => {
      withItems(lunch, { ...lunch, ID: 'i4', Text: 'Call the bank', Start: null })
      m.deleteItem.mockResolvedValue(undefined)
      render(<Planner />)
      const t = await activities()
      await userEvent.click(t.getByRole('button', { name: 'Remove Lunch + walk from the day' }))
      expect(m.deleteItem).toHaveBeenCalledWith('i2')
      await userEvent.click(t.getByRole('button', { name: 'Remove Call the bank from the day' }))
      expect(m.deleteItem).toHaveBeenCalledWith('i4')
    })

    it('renames an activity on a click on its name', async () => {
      render(<Planner />)
      const t = await activities()
      click(t.getByText('Lunch + walk'))
      expect(screen.queryByRole('dialog')).toBeNull()
      const input = t.getByRole('textbox', { name: 'Rename Lunch + walk' })
      expect(input).toHaveValue('Lunch + walk')
      await userEvent.clear(input)
      await userEvent.type(input, 'Lunch with Sam{Enter}')
      expect(m.updateItem).toHaveBeenCalledWith('i2', expect.objectContaining({ text: 'Lunch with Sam', rowId: 'r2', intendedHours: 1 }))
      expect(t.queryByRole('textbox', { name: /Rename/ })).toBeNull()
    })

    it('saves a rename when you click away, and keeps the name on Escape or when empty', async () => {
      render(<Planner />)
      const t = await activities()
      click(t.getByText('Lunch + walk'))
      await userEvent.type(t.getByRole('textbox', { name: 'Rename Lunch + walk' }), ' together{Escape}')
      expect(m.updateItem).not.toHaveBeenCalled()
      expect(t.getByText('Lunch + walk')).toBeInTheDocument()
      click(t.getByText('Lunch + walk'))
      await userEvent.clear(t.getByRole('textbox', { name: 'Rename Lunch + walk' }))
      await userEvent.keyboard('{Enter}')
      expect(m.updateItem).not.toHaveBeenCalled()
      click(t.getByText('Lunch + walk'))
      await userEvent.type(t.getByRole('textbox', { name: 'Rename Lunch + walk' }), ' together')
      fireEvent.blur(t.getByRole('textbox', { name: 'Rename Lunch + walk' }))
      expect(m.updateItem).toHaveBeenCalledWith('i2', expect.objectContaining({ text: 'Lunch + walk together' }))
    })

    it('opens on a click on its block, with no tracker to open or move it in', async () => {
      render(<Planner />)
      await screen.findByDisplayValue('Work plan')
      click(screen.getByRole('button', { name: /Lunch \+ walk \d/ }))
      const menu = within(screen.getByRole('dialog', { name: 'Lunch + walk' }))
      expect(menu.getByRole('button', { name: 'Mark done' })).toBeInTheDocument()
      expect(menu.queryByRole('link')).toBeNull()
      expect(menu.queryByText(/Also moves/)).toBeNull()
    })

    it('opens on a click on its tray card, with a time or without', async () => {
      withItems(lunch, { ...lunch, ID: 'i4', Text: 'Call the bank', Start: null })
      render(<Planner />)
      const t = await activities()
      click(cardOf(t, 'Lunch + walk'))
      expect(screen.getByRole('dialog', { name: 'Lunch + walk' })).toBeInTheDocument()
      await userEvent.keyboard('{Escape}')
      click(cardOf(t, 'Call the bank'))
      expect(screen.getByRole('dialog', { name: 'Call the bank' })).toBeInTheDocument()
    })

    it('marks it done, then refetches and closes', async () => {
      m.markItemDone.mockResolvedValue(undefined)
      render(<Planner />)
      await screen.findByDisplayValue('Work plan')
      await waitFor(() => expect(m.fetchPlans).toHaveBeenCalledTimes(2))
      click(screen.getByRole('button', { name: /Lunch \+ walk \d/ }))
      await userEvent.click(screen.getByRole('button', { name: 'Mark done' }))
      expect(m.markItemDone).toHaveBeenCalledWith('i2', true)
      await waitFor(() => expect(m.fetchPlans).toHaveBeenCalledTimes(3))
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('shows one marked done on the timeline and in the tray', async () => {
      withItems({ ...lunch, DoneAt: doneAt })
      render(<Planner />)
      const t = await activities()
      expect(t.getByText('Lunch + walk')).toHaveClass('line-through')
      expect(t.getByText('Done')).toBeInTheDocument()
      expect(within(screen.getByTestId('row-r2')).getByText(/✓ Lunch \+ walk/)).toHaveClass('line-through')
    })

    it('does not show one that is not done as done', async () => {
      render(<Planner />)
      const t = await activities()
      expect(t.getByText('Lunch + walk')).not.toHaveClass('line-through')
      expect(t.queryByText('Done')).toBeNull()
      expect(within(screen.getByTestId('row-r2')).queryByText(/✓/)).toBeNull()
    })

    it('offers Mark not done on one marked done', async () => {
      m.markItemDone.mockResolvedValue(undefined)
      withItems({ ...lunch, DoneAt: doneAt })
      render(<Planner />)
      click(cardOf(await activities(), 'Lunch + walk'))
      await userEvent.click(screen.getByRole('button', { name: 'Mark not done' }))
      expect(m.markItemDone).toHaveBeenCalledWith('i2', false)
    })

    it('shows why it could not save, and stays open', async () => {
      m.markItemDone.mockRejectedValue(new Error('disk full'))
      render(<Planner />)
      click(cardOf(await activities(), 'Lunch + walk'))
      await userEvent.click(screen.getByRole('button', { name: 'Mark done' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('Could not save: disk full')
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('still renames on a double-click, which closes the menu', async () => {
      render(<Planner />)
      await screen.findByDisplayValue('Work plan')
      const block = screen.getByRole('button', { name: /Lunch \+ walk \d/ })
      click(block)
      click(block)
      fireEvent.doubleClick(block)
      expect(screen.getByLabelText('Activity name')).toBeInTheDocument()
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('does not open while its name is being edited', async () => {
      render(<Planner />)
      await screen.findByDisplayValue('Work plan')
      fireEvent.doubleClick(screen.getByRole('button', { name: /Lunch \+ walk \d/ }))
      fireEvent.click(screen.getByLabelText('Activity name'))
      expect(screen.queryByRole('dialog')).toBeNull()
    })
  })

  describe('on a planned review', () => {
    const pr = { ...plan.Items![2], RefURL: 'https://github.com/o/r/pull/3' }
    const planned = async () => within(within(await screen.findByRole('region', { name: /Reviews/ })).getByRole('group', { name: /In this plan/ }))

    it('marks it done next to its link to GitHub', async () => {
      m.markItemDone.mockResolvedValue(undefined)
      m.fetchPlans.mockResolvedValue([{ ...plan, Items: [pr] }])
      render(<Planner />)
      click((await planned()).getByText('Run upload tests'))
      const menu = within(screen.getByRole('dialog', { name: 'Run upload tests' }))
      expect(menu.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', 'https://github.com/o/r/pull/3')
      await userEvent.click(menu.getByRole('button', { name: 'Mark done' }))
      expect(m.markItemDone).toHaveBeenCalledWith('i3', true)
    })

    it('shows one marked done on the timeline and in the tray, and can undo it', async () => {
      m.markItemDone.mockResolvedValue(undefined)
      m.fetchPlans.mockResolvedValue([{ ...plan, Items: [{ ...pr, RowID: 'r1', Start: iso('2026-10-29', 10), DoneAt: iso('2026-10-29', 11) }] }])
      render(<Planner />)
      const t = await planned()
      expect(t.getByText('Run upload tests')).toHaveClass('line-through')
      expect(t.getByText('Done')).toBeInTheDocument()
      expect(within(screen.getByTestId('row-r1')).getByText(/✓ Run upload tests/)).toBeInTheDocument()
      click(t.getByText('Run upload tests'))
      await userEvent.click(screen.getByRole('button', { name: 'Mark not done' }))
      expect(m.markItemDone).toHaveBeenCalledWith('i3', false)
    })
  })

  it('marks the ticket done in its tracker, then refetches and closes', async () => {
    m.markTicketDone.mockResolvedValue(undefined)
    render(<Planner />)
    click((await notPlanned()).getByText('Cache search results'))
    await waitFor(() => expect(m.fetchPlannable).toHaveBeenCalledTimes(2))
    await userEvent.click(screen.getByRole('button', { name: 'Mark done' }))
    expect(m.markTicketDone).toHaveBeenCalledWith('linear', 'b', true)
    await waitFor(() => expect(m.fetchPlannable).toHaveBeenCalledTimes(3))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('offers Mark not done on a finished planned ticket', async () => {
    m.markTicketDone.mockResolvedValue(undefined)
    m.fetchPlans.mockResolvedValue([{ ...plan, Items: [{ ...plan.Items![0], RefState: 'Done', RefStage: 'completed' }] }])
    render(<Planner />)
    const tickets = within(await screen.findByRole('region', { name: /Tickets/ }))
    click(within(tickets.getByRole('group', { name: /In this plan/ })).getByText('Deploy the onboarding flow'))
    await userEvent.click(screen.getByRole('button', { name: 'Mark not done' }))
    expect(m.markTicketDone).toHaveBeenCalledWith('linear', 'a', false)
  })

  it('shows why it could not save, and stays open', async () => {
    m.markTicketDone.mockRejectedValue(new Error('linear: no done state'))
    render(<Planner />)
    click((await notPlanned()).getByText('Cache search results'))
    await userEvent.click(screen.getByRole('button', { name: 'Mark done' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not save: linear: no done state')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('closes on Escape', async () => {
    render(<Planner />)
    click((await notPlanned()).getByText('Cache search results'))
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

describe('Planner date change', () => {
  it('moves the plan with one request; the server moves its blocks', async () => {
    render(<Planner />)
    fireEvent.change(await screen.findByLabelText('Plan date'), { target: { value: '2026-10-31' } })
    expect(m.updatePlan).toHaveBeenCalledTimes(1)
    expect(m.updatePlan).toHaveBeenCalledWith('p1', 'Thursday focus', iso('2026-10-31', 9), iso('2026-10-31', 18))
    expect(m.updateItem).not.toHaveBeenCalled()
  })

  it('renames a plan that still has its automatic name to the new day', async () => {
    m.fetchPlans.mockResolvedValue([{ ...plan, Name: 'Plan for Thu 29 Oct' }])
    render(<Planner />)
    fireEvent.change(await screen.findByLabelText('Plan date'), { target: { value: '2026-10-28' } })
    expect(m.updatePlan).toHaveBeenCalledTimes(1)
    expect(m.updatePlan).toHaveBeenCalledWith('p1', 'Plan for Wed 28 Oct', iso('2026-10-28', 9), iso('2026-10-28', 18))
  })

  it('ignores the half-typed years a date input emits while typing', async () => {
    render(<Planner />)
    fireEvent.change(await screen.findByLabelText('Plan date'), { target: { value: '0002-10-31' } })
    expect(m.updatePlan).not.toHaveBeenCalled()
  })
})

describe('your PRs in the Reviews tray', () => {
  const url = 'https://x/pull/1'
  const own = (over: Partial<Plannable> = {}): Plannable => ({ RefSource: 'github', RefID: `own:${url}`, Text: 'Retry uploads', Repo: 'o/r', URL: url, Kind: 'own_pr', Hours: 0.5, State: 'changes_requested', Stage: '', Project: '', Labels: [], Reviewer: 'alex', ReviewedAt: new Date(Date.now() - 2 * 3600 * 1000).toISOString(), ...over })
  const planned = { ...plan.Items![2], ID: 'i9', RefID: `own:${url}`, Text: 'Retry uploads', RefState: 'approved', RefURL: url, RefRepo: 'o/r' }
  const open = async (items: Plannable[], planItems = plan.Items) => {
    m.fetchPlans.mockResolvedValue([{ ...plan, Items: planItems }])
    m.fetchPlannable.mockResolvedValue(items)
    m.setOwnPRDone.mockResolvedValue(undefined)
    render(<Planner />)
    await waitFor(() => expect(m.fetchPlannable).toHaveBeenCalled())
    return within(await screen.findByRole('region', { name: /Reviews/ }))
  }
  const press = (el: HTMLElement) => {
    fireEvent(el, new MouseEvent('pointerdown', { bubbles: true, clientX: 10, clientY: 10 }))
    fireEvent(document, new MouseEvent('pointerup', { bubbles: true, clientX: 10, clientY: 10 }))
    fireEvent.click(el)
  }

  it('lists them in their own group with their badge, reviewer and age', async () => {
    const t = await open([own()])
    const group = within(await t.findByRole('group', { name: /Your PRs/ }))
    expect(await group.findByText('Retry uploads')).toBeInTheDocument()
    expect(group.getByText('Changes requested')).toBeInTheDocument()
    expect(group.getByText('o/r · github · alex · 2h ago')).toBeInTheDocument()
    expect(within(t.getByRole('group', { name: /Not planned/ })).queryByText('Retry uploads')).toBeNull()
    expect(screen.queryByRole('button', { name: /^Hide Retry uploads/ })).toBeNull()
  })

  it('counts them, and only when there are some', async () => {
    const t = await open([own()])
    expect(await t.findByText('0 waiting, 1 planned, 1 on your PRs')).toBeInTheDocument()
  })

  it('keeps the count as it was without any', async () => {
    const t = await open([])
    expect(await t.findByText('0 waiting, 1 planned')).toBeInTheDocument()
  })

  it('plans one when dragged onto In this plan', async () => {
    const t = await open([own()])
    const card = await t.findByText('Retry uploads')
    const box = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this === t.getByRole('group', { name: /In this plan/ }) ? new DOMRect(0, 0, 100, 100) : new DOMRect(-500, -500, 1, 1)
    })
    onTestFinished(() => box.mockRestore())
    fireEvent(card, new MouseEvent('pointerdown', { bubbles: true, clientX: 300, clientY: 300 }))
    fireEvent(document, new MouseEvent('pointermove', { bubbles: true, clientX: 50, clientY: 50 }))
    fireEvent(document, new MouseEvent('pointerup', { bubbles: true, clientX: 50, clientY: 50 }))
    await waitFor(() => expect(m.addItem).toHaveBeenCalledWith('p1', expect.objectContaining({ refSource: 'github', refId: `own:${url}`, text: 'Retry uploads', start: null, intendedHours: 0.5 })))
  })

  it('marks an unplanned one done from its menu', async () => {
    const t = await open([own()])
    press(await t.findByText('Retry uploads'))
    const menu = within(screen.getByRole('dialog', { name: 'Retry uploads' }))
    expect(menu.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', url)
    await userEvent.click(menu.getByRole('button', { name: 'Mark done' }))
    expect(m.setOwnPRDone).toHaveBeenCalledWith(url, true)
  })

  it('keeps done ones behind Show done, with Mark not done', async () => {
    const t = await open([own({ Done: true })])
    expect(t.queryByText('Retry uploads')).toBeNull()
    await userEvent.click(await t.findByRole('button', { name: 'Show done (1)' }))
    press(await t.findByText('Retry uploads'))
    await userEvent.click(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('button', { name: 'Mark not done' }))
    expect(m.setOwnPRDone).toHaveBeenCalledWith(url, false)
  })

  it('marks a planned one through its PR, not its plan item', async () => {
    const t = await open([own()], [planned])
    press(await within(t.getByRole('group', { name: /In this plan/ })).findByText('Retry uploads'))
    await userEvent.click(within(screen.getByRole('dialog', { name: 'Retry uploads' })).getByRole('button', { name: 'Mark done' }))
    expect(m.setOwnPRDone).toHaveBeenCalledWith(url, true)
    expect(m.markItemDone).not.toHaveBeenCalled()
  })

  it('shows a planned one that closed as done, with no way to undo that', async () => {
    const t = await open([], [{ ...planned, RefState: '', RefURL: '', RefStage: 'completed' }])
    press(await within(t.getByRole('group', { name: /In this plan/ })).findByText('Retry uploads'))
    const menu = within(screen.getByRole('dialog', { name: 'Retry uploads' }))
    expect(menu.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', url)
    expect(menu.getByText('No longer open on GitHub.')).toBeInTheDocument()
    expect(menu.queryByRole('button', { name: /done/i })).toBeNull()
  })
})

describe('reviews from other code hosts', () => {
  const url = 'https://gitlab.com/g/p/-/merge_requests/1'
  const review = (over: Partial<Plannable> = {}): Plannable => ({ RefSource: 'gitlab', RefID: url, Text: 'Theirs', Repo: 'g/p', URL: url, Kind: 'review_request', Hours: 0.5, State: '', Stage: '', Project: '', Labels: [], ...over })
  const open = async (items: Plannable[], planItems = plan.Items) => {
    m.fetchPlans.mockResolvedValue([{ ...plan, Items: planItems }])
    m.fetchPlannable.mockResolvedValue(items)
    render(<Planner />)
    await waitFor(() => expect(m.fetchPlannable).toHaveBeenCalled())
    return within(await screen.findByRole('region', { name: /Reviews/ }))
  }
  const press = (el: HTMLElement) => {
    fireEvent(el, new MouseEvent('pointerdown', { bubbles: true, clientX: 10, clientY: 10 }))
    fireEvent(document, new MouseEvent('pointerup', { bubbles: true, clientX: 10, clientY: 10 }))
    fireEvent.click(el)
  }

  it('lists them among the reviews, naming their source', async () => {
    const t = await open([review()])
    expect(within(await t.findByRole('group', { name: /Not planned/ })).getByText('g/p · gitlab')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: /Tickets/ })).queryByText('Theirs')).toBeNull()
  })

  it.each([['gitlab', 'Open on GitLab'], ['azure', 'Open on Azure DevOps'], ['github', 'Open on GitHub']])('opens a %s one with %s', async (source, label) => {
    const t = await open([review({ RefSource: source })])
    press(await t.findByText('Theirs'))
    const menu = within(screen.getByRole('dialog', { name: 'Theirs' }))
    expect(menu.getByRole('link', { name: label })).toHaveAttribute('href', url)
    expect(menu.queryByRole('button', { name: /done/i })).toBeNull()
  })

  it('names the source of a planned own PR, and where it closed', async () => {
    const closed = { ...plan.Items![2], ID: 'i9', RefSource: 'azure', RefID: 'own:https://dev.azure.com/a/P/_git/r/pullrequest/9', Text: 'Mine', RefState: '', RefURL: '', RefStage: 'completed' }
    const t = await open([], [closed])
    expect(await within(t.getByRole('group', { name: /In this plan/ })).findByText('azure')).toBeInTheDocument()
    press(await within(t.getByRole('group', { name: /In this plan/ })).findByText('Mine'))
    const menu = within(screen.getByRole('dialog', { name: 'Mine' }))
    expect(menu.getByRole('link', { name: 'Open on Azure DevOps' })).toBeInTheDocument()
    expect(menu.getByText('No longer open on Azure DevOps.')).toBeInTheDocument()
  })
})
