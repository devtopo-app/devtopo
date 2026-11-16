import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { History } from './History'

const hours = Array.from({ length: 24 }, (_, h) => (h === 9 ? 1.5 : h === 15 ? 0.5 : 0))

const day21 = {
  Date: '2026-10-21',
  First: '2026-10-21T09:00:00',
  Last: '2026-10-21T16:00:00',
  AIHours: 2,
  Prompts: 3,
  Hours: hours,
  Sessions: [
    { ID: 'a', Name: 'Bug hunt', Agent: 'claude', Project: 'github.com/x/devtopo', Branch: 'main', AIHours: 1.5, Prompts: 2, Points: [], Summarizing: true },
    { ID: 'b', Name: '', Agent: 'codex', Project: 'github.com/x/tools', Branch: 'feat/x', AIHours: 0.5, Prompts: 1, Points: ['Wrote the reader'], Summarizing: false },
  ],
  Tickets: [{ Source: 'linear', Kind: 'task', Key: 'ENG-1', Title: 'Fix it', URL: 'u1', At: '2026-10-21T11:00:00' }],
  Reviews: 2,
  OtherTasks: 1,
  MeetingHours: null,
}

const empty = (date: string) => ({ Date: date, First: '0001-01-01T00:00:00Z', Last: '0001-01-01T00:00:00Z', AIHours: 0, Prompts: 0, Hours: Array(24).fill(0), Sessions: [], Tickets: [], Reviews: 0, OtherTasks: 0, MeetingHours: null })

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let fetchMock: Mock<(url: string, init?: RequestInit) => Promise<any>>
// The past-day points request waits on this gate, so a test sees the
// "Summarizing..." in between however busy the machine is.
let pointsGate: Promise<void>
let answerPoints: () => void

beforeEach(() => {
  pointsGate = new Promise((r) => (answerPoints = r))
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-10-29T12:00:00'))
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const q = new URL(url, 'http://x').searchParams
    if (url.startsWith('/api/history/month')) {
      const m = q.get('m')
      return { ok: true, json: async () => (m === '2026-10' ? [{ Date: '2026-10-21', AIHours: 2, Tickets: 1 }, { Date: '2026-10-22', AIHours: 0.2, Tickets: 0 }, { Date: '2026-10-29', AIHours: 4, Tickets: 0 }] : []) }
    }
    if (url.startsWith('/api/history/day')) {
      const d = q.get('d')!
      if (d === '2026-10-29') return { ok: true, json: async () => ({ ...day21, Date: d, Sessions: [], Tickets: [], MeetingHours: 1.5 }) }
      return { ok: true, json: async () => (d === '2026-10-21' ? day21 : empty(d)) }
    }
    if (url.startsWith('/api/history/points') && init?.method === 'POST') {
      await pointsGate // a claude call takes a while: it answers when the test says
      return { ok: true, json: async () => ({ Points: ['Fixed the bug'] }) }
    }
    return { ok: false, status: 404, text: async () => '' }
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => vi.useRealTimers())

const dayButton = (n: number) => screen.getByRole('button', { name: new RegExp(`^${n} October`) })

describe('History', () => {
  it('opens on today in its month, with dots sized by AI time and month totals', async () => {
    render(<History />)
    expect(await screen.findByText('October 2026')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('heading', { name: /Thursday 29 October 2026/ })).toBeInTheDocument())
    expect(dayButton(29)).toHaveAttribute('aria-pressed', 'true')
    expect(dayButton(21)).toHaveAccessibleName(/2(\.0)? AI hours/)
    expect(dayButton(21).querySelector('[data-level="2"]')).not.toBeNull()
    expect(dayButton(22).querySelector('[data-level="1"]')).not.toBeNull()
    expect(dayButton(29).querySelector('[data-level="3"]')).not.toBeNull()
    expect(dayButton(23).querySelector('[data-level]')).toBeNull()
    const totals = screen.getByLabelText('Month totals')
    expect(totals).toHaveTextContent('3 active days')
    expect(totals).toHaveTextContent('6.2h AI time')
    expect(totals).toHaveTextContent('1 tickets finished')
    // today has calendar data: the meetings tile shows
    expect(screen.getByRole('group', { name: 'Meetings' })).toHaveTextContent('1.5')
  })

  it('shows a picked day: tiles, hours, sessions and tickets, and summarizes a past session once', async () => {
    render(<History />)
    await userEvent.click(await screen.findByRole('button', { name: /^21 October/ }))
    expect(await screen.findByRole('heading', { name: /Wednesday 21 October 2026/ })).toBeInTheDocument()
    expect(screen.getByText(/2 sessions · active 9:00-16:00/)).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'AI hours' })).toHaveTextContent('2.0')
    expect(screen.getByRole('group', { name: 'Prompts sent' })).toHaveTextContent('3')
    expect(screen.getByRole('group', { name: 'Tickets finished' })).toHaveTextContent('1')
    expect(screen.getByRole('group', { name: 'PR reviews' })).toHaveTextContent('2')
    expect(screen.getByRole('group', { name: 'Other tasks' })).toHaveTextContent('1')
    expect(screen.queryByRole('group', { name: 'Meetings' })).toBeNull()
    expect(screen.getByTitle('9:00-10:00: 90 AI-min')).toBeInTheDocument()

    const achieved = screen.getByRole('region', { name: 'What you achieved' })
    expect(within(achieved).getByText('Bug hunt')).toBeInTheDocument()
    expect(within(achieved).getAllByText('feat/x')).toHaveLength(2) // unnamed: titled by its branch, and its branch
    expect(within(achieved).getByText('Codex')).toBeInTheDocument()
    expect(within(achieved).getByText('Wrote the reader')).toBeInTheDocument()
    expect(within(achieved).getByText('Summarizing...')).toBeInTheDocument()
    answerPoints()
    expect(await within(achieved).findByText('Fixed the bug')).toBeInTheDocument()
    expect(within(achieved).queryByText('Summarizing...')).toBeNull()
    const asked = fetchMock.mock.calls.filter((c) => String(c[0]).startsWith('/api/history/points'))
    expect(asked).toHaveLength(1)
    expect(asked[0][0]).toBe('/api/history/points?d=2026-10-21&session=a')

    const tickets = screen.getByRole('region', { name: /Tickets finished/ })
    expect(within(tickets).getByText('ENG-1')).toBeInTheDocument()
    expect(within(tickets).getByRole('link', { name: 'Fix it' })).toHaveAttribute('href', 'u1')
  })

  describe('with several sessions to summarize', () => {
    const twoWaiting = {
      ...day21,
      Sessions: [day21.Sessions[0], { ...day21.Sessions[1], Points: [], Summarizing: true }],
    }
    const pointsAsks = () => fetchMock.mock.calls.filter((c) => String(c[0]).startsWith('/api/history/points'))

    beforeEach(() => {
      const base = fetchMock.getMockImplementation()!
      fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
        if (url === '/api/history/day?d=2026-10-21') return { ok: true, json: async () => twoWaiting }
        return base(url, init)
      })
    })

    // The server makes them one at a time. Asking for all at once would hold the
    // browser's few connections, and the next day could not load until they free up.
    it('asks for one session at a time', async () => {
      render(<History />)
      await userEvent.click(await screen.findByRole('button', { name: /^21 October/ }))
      await screen.findByRole('heading', { name: /Wednesday 21 October 2026/ })
      await waitFor(() => expect(pointsAsks()).toHaveLength(1))
      await new Promise((r) => setTimeout(r))
      expect(pointsAsks()).toHaveLength(1)
      answerPoints()
      await waitFor(() => expect(pointsAsks()).toHaveLength(2))
      expect(pointsAsks()[1][0]).toBe('/api/history/points?d=2026-10-21&session=b')
    })

    it('cancels the ask and asks no more when another day is picked', async () => {
      render(<History />)
      await userEvent.click(await screen.findByRole('button', { name: /^21 October/ }))
      await screen.findByRole('heading', { name: /Wednesday 21 October 2026/ })
      await waitFor(() => expect(pointsAsks()).toHaveLength(1))
      const signal = pointsAsks()[0][1]?.signal as AbortSignal
      expect(signal.aborted).toBe(false)

      await userEvent.click(dayButton(23))
      await screen.findByText('Nothing ran this day.')
      expect(signal.aborted).toBe(true)
      answerPoints()
      await new Promise((r) => setTimeout(r))
      expect(pointsAsks()).toHaveLength(1)
    })
  })

  it('says so for an empty day', async () => {
    render(<History />)
    await userEvent.click(await screen.findByRole('button', { name: /^23 October/ }))
    expect(await screen.findByText('Nothing ran this day.')).toBeInTheDocument()
  })

  it('goes to a typed date, moving the calendar to its month', async () => {
    render(<History />)
    await screen.findByText('October 2026')
    fireEvent.change(screen.getByLabelText('Go to'), { target: { value: '2026-09-15' } })
    expect(await screen.findByText('September 2026')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: /Tuesday 15 September 2026/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^15 September/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByLabelText('Month totals')).toHaveTextContent('No activity this month.')
  })

  it('steps months with the arrows', async () => {
    render(<History />)
    await screen.findByText('October 2026')
    await userEvent.click(screen.getByRole('button', { name: 'Next month' }))
    expect(await screen.findByText('November 2026')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Previous month' }))
    await userEvent.click(screen.getByRole('button', { name: 'Previous month' }))
    expect(await screen.findByText('September 2026')).toBeInTheDocument()
  })
})
