import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AskWindow } from './AskWindow'
import type { AskModel, AskResult } from '../ask-api'
import type { ChatTurn, Progress } from '../lib/ask'
import type { CardData } from '../api'

const mocks = vi.hoisted(() => ({ fetchAskModels: vi.fn(), fetchToday: vi.fn(), focusSession: vi.fn() }))
vi.mock('../ask-api', () => ({ fetchAskModels: mocks.fetchAskModels }))
vi.mock('../api', () => ({ fetchToday: mocks.fetchToday, focusSession: mocks.focusSession }))

const claudeModels: AskModel[] = [
  { ID: 'haiku', Tool: 'claude', Label: 'Claude Haiku' },
  { ID: 'sonnet', Tool: 'claude', Label: 'Claude Sonnet' },
]
const codexModel: AskModel = { ID: 'gpt-6-luna', Tool: 'codex', Label: 'Codex gpt-6-luna' }

const card = (over: Partial<CardData>): CardData =>
  ({ ID: 'a', Project: 'p', Branch: 'b', Status: 'idle', AIHours: 0, WallHours: 0, Prompts: 0, LastActivity: '', TodayPoints: null, TodayAsOf: '', Signals: null, Host: 'orca', Live: true, CanFocus: true, ...over }) as CardData

const lane = (cards: CardData[]) => ({ Title: '', Status: '', Cards: cards })
const today = (cards: CardData[]) => ({ Running: lane(cards), Waiting: lane([]), Idle: lane([]) })

const result = (over: Partial<AskResult> = {}): AskResult => ({
  Answer: 'You chose 15 minute snapping.',
  Sources: null,
  FromSummaries: false,
  Total: 14,
  Read: 10,
  Shortened: null,
  Failed: null,
  ...over,
})

const idle: Progress = { total: 0, checked: false, relevant: null, done: 0, combining: false }

function chatOf(over: { turns?: ChatTurn[]; running?: boolean; progress?: Progress | null } = {}) {
  return { turns: [], running: false, progress: null, ask: vi.fn(), stop: vi.fn(), ...over }
}

// show renders the window and waits until the models have loaded.
async function show(over: Parameters<typeof chatOf>[0] = {}) {
  const chat = chatOf(over)
  const onClose = vi.fn()
  render(<AskWindow chat={chat} onClose={onClose} />)
  await screen.findByLabelText('Question')
  return { chat, onClose }
}

const picker = () => screen.getByRole('button', { name: /^Model:/ })
const turnWith = (r: AskResult): ChatTurn[] => [{ question: 'q', notes: [], result: r }]
const source = (id: string, over = {}) => ({ SessionID: id, Label: id, Agent: 'claude', At: '', Why: '', ...over })

beforeEach(() => {
  localStorage.clear()
  mocks.fetchAskModels.mockReset().mockResolvedValue({ Models: claudeModels, Off: false })
  mocks.fetchToday.mockReset().mockResolvedValue(today([card({ ID: 'a' }), card({ ID: 'c', Live: false, Host: '' })]))
  mocks.focusSession.mockReset().mockResolvedValue(undefined)
})

describe('AskWindow', () => {
  it('shows the warning before anything is asked', async () => {
    await show()
    expect(screen.getByRole('dialog', { name: 'Ask about today' })).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByText('Runs through your own subscription and counts toward its usage limits.')).toBeInTheDocument()
    expect(screen.getByText("Ask what happened in today's sessions.")).toBeInTheDocument()
  })

  it('adds the other-tool line only when both tools are available', async () => {
    const line = 'The selected model reads every session, including those from the other tool.'
    const { unmount } = render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    await screen.findByLabelText('Question')
    expect(screen.queryByText(line)).toBeNull()
    unmount()
    mocks.fetchAskModels.mockResolvedValue({ Models: [...claudeModels, codexModel], Off: false })
    render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    expect(await screen.findByText(line)).toBeInTheDocument()
  })

  it('picker shows only the chosen model until clicked, then lists the served models', async () => {
    await show()
    const trigger = picker()
    expect(trigger).toHaveTextContent('Claude Haiku')
    expect(screen.queryByRole('listbox')).toBeNull()
    await userEvent.click(trigger)
    const options = within(screen.getByRole('listbox')).getAllByRole('option')
    expect(options.map((o) => o.getAttribute('aria-selected'))).toEqual(['true', 'false'])
    expect(options[0]).toHaveTextContent('Claude Haiku')
    expect(options[1]).toHaveTextContent('Claude Sonnet')
  })

  it('picker shows descriptions, and tool headings only with both tools', async () => {
    mocks.fetchAskModels.mockResolvedValue({ Models: [{ ...claudeModels[0], Description: 'Fastest' }, claudeModels[1]], Off: false })
    const first = render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    await screen.findByLabelText('Question')
    await userEvent.click(picker())
    expect(screen.getByText('Fastest')).toBeInTheDocument()
    expect(within(screen.getByRole('listbox')).queryByText('Claude')).toBeNull()
    first.unmount()
    mocks.fetchAskModels.mockResolvedValue({ Models: [...claudeModels, codexModel], Off: false })
    render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    await screen.findByLabelText('Question')
    await userEvent.click(picker())
    const list = within(screen.getByRole('listbox'))
    expect(list.getByText('Claude')).toBeInTheDocument()
    expect(list.getByText('Codex')).toBeInTheDocument()
  })

  it('starts on the model the server marks as default, not the first listed', async () => {
    mocks.fetchAskModels.mockResolvedValue({ Models: [{ ID: 'opus', Tool: 'claude', Label: 'Opus 5.5' }, { ID: 'haiku', Tool: 'claude', Label: 'Haiku 5.5', Default: true }], Off: false })
    const { chat } = await show()
    expect(picker()).toHaveTextContent('Haiku 5.5')
    await userEvent.type(screen.getByLabelText('Question'), 'hi{Enter}')
    expect(chat.ask).toHaveBeenCalledWith('hi', 'haiku')
  })

  it('remembers the chosen model and closes the panel', async () => {
    const { chat } = await show()
    await userEvent.click(picker())
    await userEvent.click(screen.getByRole('option', { name: /Claude Sonnet/ }))
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(picker()).toHaveTextContent('Claude Sonnet')
    expect(picker()).toHaveFocus()
    expect(localStorage.getItem('devtopo-ask-model')).toBe('sonnet')
    await userEvent.type(screen.getByLabelText('Question'), 'hi{Enter}')
    expect(chat.ask).toHaveBeenCalledWith('hi', 'sonnet')
  })

  it('Escape closes only the panel, and a second Escape closes the window', async () => {
    const { onClose } = await show()
    await userEvent.click(picker())
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalled()
  })

  it('a click outside closes the panel but not the window', async () => {
    const { onClose } = await show()
    await userEvent.click(picker())
    await userEvent.click(screen.getByLabelText('Question'))
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('arrow keys and Enter choose a model', async () => {
    await show()
    await userEvent.click(picker())
    expect(screen.getByRole('option', { name: /Claude Haiku/ })).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('option', { name: /Claude Sonnet/ })).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('option', { name: /Claude Haiku/ })).toHaveFocus()
    await userEvent.keyboard('{ArrowUp}{Enter}')
    expect(picker()).toHaveTextContent('Claude Sonnet')
  })

  it('starts on the remembered model', async () => {
    localStorage.setItem('devtopo-ask-model', 'sonnet')
    await show()
    expect(picker()).toHaveTextContent('Claude Sonnet')
  })

  it('falls back when the saved model is no longer available', async () => {
    localStorage.setItem('devtopo-ask-model', 'gone')
    const { chat } = await show()
    expect(picker()).toHaveTextContent('Claude Haiku')
    await userEvent.type(screen.getByLabelText('Question'), 'hi{Enter}')
    expect(chat.ask).toHaveBeenCalledWith('hi', 'haiku')
  })

  it('asks with Enter or the Ask button, clears the box, and ignores a blank question', async () => {
    const { chat } = await show()
    const box = screen.getByLabelText('Question')
    expect(box).toHaveFocus()
    await userEvent.type(box, '   {Enter}')
    expect(chat.ask).not.toHaveBeenCalled()
    await userEvent.clear(box)
    await userEvent.type(box, 'what happened?')
    await userEvent.click(screen.getByRole('button', { name: 'Ask' }))
    expect(chat.ask).toHaveBeenCalledWith('what happened?', 'haiku')
    expect(box).toHaveValue('')
  })

  it('shows progress steps', async () => {
    await show({
      turns: [{ question: 'q', notes: [] }],
      running: true,
      progress: { total: 14, checked: true, relevant: 10, done: 6, combining: false },
    })
    expect(screen.getByText('Checked all 14 sessions against their summaries')).toBeInTheDocument()
    expect(screen.getByText('10 look relevant')).toBeInTheDocument()
    expect(screen.getByText('Reading the relevant sessions: 6 of 10')).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '6')
    expect(screen.getByText('Claude Haiku')).toBeInTheDocument()
    expect(screen.queryByText('Writing the answer')).toBeNull()
  })

  it('says it is checking before the check is done, and writing at the end', async () => {
    const turns = [{ question: 'q', notes: [] }]
    const first = render(<AskWindow chat={chatOf({ turns, running: true, progress: { ...idle, total: 14 } })} onClose={vi.fn()} />)
    expect(await screen.findByText('Checking 14 sessions')).toBeInTheDocument()
    first.unmount()
    render(<AskWindow chat={chatOf({ turns, running: true, progress: { total: 14, checked: true, relevant: 3, done: 3, combining: true } })} onClose={vi.fn()} />)
    expect(await screen.findByText('Writing the answer')).toBeInTheDocument()
  })

  it('swaps Ask for Stop during a run, and Stop calls stop', async () => {
    const { chat } = await show({ turns: [{ question: 'q', notes: [] }], running: true, progress: idle })
    expect(screen.queryByRole('button', { name: 'Ask' })).toBeNull()
    expect(screen.queryByRole('button', { name: /^Model:/ })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Stop' }))
    expect(chat.stop).toHaveBeenCalled()
  })

  it('shows sources with label, time, agent and why', async () => {
    await show({
      turns: turnWith(
        result({
          Sources: [
            source('planner-drag', { At: '10:42', Why: 'Chose 15-minute snapping' }),
            source('main', { Agent: 'codex' }),
          ],
        }),
      ),
    })
    const [one, two] = screen.getAllByRole('listitem')
    expect(one).toHaveTextContent('planner-drag')
    expect(one).toHaveTextContent('10:42')
    expect(one).not.toHaveTextContent('NaN')
    expect(one).toHaveTextContent('Claude')
    expect(one).toHaveTextContent('Chose 15-minute snapping')
    expect(two).toHaveTextContent('main')
    expect(two).toHaveTextContent('Codex')
    expect(two.textContent).not.toMatch(/\d\d:\d\d/)
  })

  it('renders two sources from one session', async () => {
    await show({ turns: turnWith(result({ Sources: [source('a', { At: '09:30' }), source('a', { At: '11:15' })] })) })
    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(items[0]).toHaveTextContent('09:30')
    expect(items[1]).toHaveTextContent('11:15')
  })

  it('closes on a click that starts and ends on the scrim, not on a drag that ends there', async () => {
    const { onClose } = await show({ turns: turnWith(result()) })
    const scrim = screen.getByTestId('ask-scrim')
    await userEvent.pointer([{ keys: '[MouseLeft>]', target: screen.getByRole('dialog') }, { keys: '[/MouseLeft]', target: scrim }])
    expect(onClose).not.toHaveBeenCalled()
    await userEvent.pointer([{ keys: '[MouseLeft>]', target: scrim }, { keys: '[/MouseLeft]', target: scrim }])
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('Open calls the right action, then closes the window', async () => {
    const { onClose } = await show({ turns: turnWith(result({ Sources: [source('a'), source('c')] })) })
    const [one, two] = await screen.findAllByRole('button', { name: 'Open' })
    await userEvent.click(two)
    expect(mocks.focusSession).toHaveBeenCalledWith('c', 'resume')
    await userEvent.click(one)
    expect(mocks.focusSession).toHaveBeenCalledWith('a', 'focus')
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('keeps the window open and shows the error when Open fails', async () => {
    mocks.focusSession.mockRejectedValue(new Error('no window'))
    const { onClose } = await show({ turns: turnWith(result({ Sources: [source('a')] })) })
    await userEvent.click(await screen.findByRole('button', { name: 'Open' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('no window')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('leaves out Open where a session cannot be opened', async () => {
    mocks.fetchToday.mockResolvedValue(today([card({ ID: 'a' }), card({ ID: 'n', Host: 'other' })]))
    await show({ turns: turnWith(result({ Sources: [source('a'), source('n'), source('gone')] })) })
    await screen.findByRole('button', { name: 'Open' })
    expect(screen.getAllByRole('button', { name: 'Open' })).toHaveLength(1)
    expect(screen.getAllByRole('listitem')).toHaveLength(3)
  })

  it('says Read 10 of 14 sessions, or that it answered from summaries', async () => {
    await show({
      turns: [
        { question: 'q1', notes: [], result: result() },
        { question: 'q2', notes: [], result: result({ FromSummaries: true, Read: 0 }) },
        { question: 'q3', notes: [], result: result({ Read: 0 }) },
        { question: 'q4', notes: [], result: result({ Read: 0, Total: 0 }) },
      ],
    })
    expect(screen.getByText('Read 10 of 14 sessions')).toBeInTheDocument()
    expect(screen.getByText('Answered from summaries')).toBeInTheDocument()
    expect(screen.getByText('Read 0 of 14 sessions')).toBeInTheDocument()
    expect(screen.queryByText(/of 0 sessions/)).toBeNull()
  })

  it('names shortened and failed sessions', async () => {
    await show({ turns: turnWith(result({ Shortened: ['x'], Failed: ['y'], Read: 9, Total: 10 })) })
    expect(screen.getByText('Shortened to fit: x')).toBeInTheDocument()
    expect(screen.getByText("Couldn't read 1 of 10 sessions: y")).toBeInTheDocument()
  })

  it('shows a stopped turn and a failed turn', async () => {
    await show({ turns: [{ question: 'q1', notes: [], stopped: true }, { question: 'q2', notes: [], error: 'claude: logged out' }] })
    expect(screen.getByText('Stopped.')).toBeInTheDocument()
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('claude: logged out')
    expect(alert).toHaveClass('text-waiting')
  })

  it('renders answer text as plain text, not HTML', async () => {
    const { container } = render(<AskWindow chat={chatOf({ turns: [{ question: '<i>q</i>', notes: [], result: result({ Answer: '<b>x</b>' }) }] })} onClose={vi.fn()} />)
    expect(await screen.findByText('<b>x</b>')).toBeInTheDocument()
    expect(container.querySelector('b, i')).toBeNull()
  })

  it('closes on X, Escape and an outside click', async () => {
    const { onClose } = await show()
    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
    await userEvent.click(screen.getByRole('dialog'))
    expect(onClose).toHaveBeenCalledTimes(2)
    await userEvent.click(screen.getByTestId('ask-scrim'))
    expect(onClose).toHaveBeenCalledTimes(3)
  })

  it('explains how to install a tool when there are no models', async () => {
    mocks.fetchAskModels.mockResolvedValue({ Models: [], Off: false })
    render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    expect(await screen.findByText(/Asking needs Claude Code or Codex installed and logged in\. After installing one, press Check again in Settings\./)).toBeInTheDocument()
    expect(screen.getByRole('link')).toHaveAttribute('href', '#/help')
    expect(screen.queryByLabelText('Question')).toBeNull()
  })

  it('says summaries and chat are off with --no-narrative', async () => {
    mocks.fetchAskModels.mockResolvedValue({ Models: [], Off: true })
    render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    expect(await screen.findByText('Summaries and chat are switched off (--no-narrative).')).toBeInTheDocument()
  })

  it('shows a short error when the models cannot be loaded', async () => {
    mocks.fetchAskModels.mockRejectedValue(new Error('boom'))
    render(<AskWindow chat={chatOf()} onClose={vi.fn()} />)
    expect(await screen.findByRole('alert')).toHaveTextContent("Couldn't load the models.")
    expect(screen.queryByText(/Asking needs/)).toBeNull()
  })
})
