import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Card, openAction } from './Card'
import type { CardData } from '../api'

const asOf = new Date()
asOf.setHours(14, 5, 0, 0)

const sample: CardData = {
  ID: 's',
  Project: 'github.com/x/s',
  Branch: 'main',
  Status: 'waiting',
  AIHours: 1.2,
  WallHours: 2.0,
  Prompts: 3,
  LastActivity: new Date().toISOString(),
  TodayPoints: ['fixed the flaky test', 'wrote the docs'],
  TodayAsOf: asOf.toISOString(),
  Signals: { prompt: 3, commit: 1 },
  Host: 'orca',
  Live: true,
  CanFocus: true,
}

const fetchMock = vi.fn()
beforeEach(() => vi.stubGlobal('fetch', fetchMock))
afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

async function open(card: CardData = sample) {
  render(<Card card={card} />)
  await userEvent.click(screen.getByTestId('card-meta'))
}

describe('Card', () => {
  it("reveals today's points and their time only after a click", async () => {
    render(<Card card={sample} />)
    expect(screen.queryByText('fixed the flaky test')).toBeNull()
    await userEvent.click(screen.getByTestId('card-meta'))
    expect(screen.getAllByRole('listitem').map((li) => li.textContent)).toEqual(['fixed the flaky test', 'wrote the docs'])
    expect(screen.getByText('as of 14:05')).toBeInTheDocument()
  })

  it('says when the session has no activity today', async () => {
    await open({ ...sample, LastActivity: '2020-01-01T10:00:00Z', TodayPoints: null, TodayAsOf: '0001-01-01T00:00:00Z' })
    expect(screen.getByText('No activity today')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More detail' })).toBeNull()
  })

  it('says when today has no summary yet', async () => {
    await open({ ...sample, TodayPoints: null, TodayAsOf: '0001-01-01T00:00:00Z' })
    expect(screen.getByText('No summary yet')).toBeInTheDocument()
  })

  it('fetches and shows the detailed summary on demand', async () => {
    let resolve!: (r: Response) => void
    fetchMock.mockReturnValue(new Promise<Response>((r) => (resolve = r)))
    await open()
    await userEvent.click(screen.getByRole('button', { name: 'More detail' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s/detail', expect.objectContaining({ method: 'POST' }))
    expect(screen.getByText('Summarizing...')).toBeInTheDocument()
    resolve(new Response(JSON.stringify({ Text: 'What was done\n- shipped it', AsOf: asOf.toISOString() })))
    expect(await screen.findByText(/shipped it/)).toBeInTheDocument()
    expect(screen.getByText('github.com/x/s')).toBeInTheDocument() // the card stayed open
    expect(screen.queryByText('fixed the flaky test')).not.toBeNull()
  })

  it('hides the detailed summary again, keeping the card open, and fetches a fresh one on request', async () => {
    fetchMock.mockImplementation(async () => new Response(JSON.stringify({ Text: 'What was done\n- shipped it', AsOf: asOf.toISOString() })))
    await open()
    expect(screen.queryByRole('button', { name: 'Hide detail' })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'More detail' }))
    expect(await screen.findByText(/shipped it/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More detail' })).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: 'Hide detail' }))
    expect(screen.queryByText(/shipped it/)).toBeNull()
    expect(screen.queryByRole('button', { name: 'Hide detail' })).toBeNull()
    expect(screen.getByText('fixed the flaky test')).toBeInTheDocument() // the card stayed open

    await userEvent.click(screen.getByRole('button', { name: 'More detail' }))
    expect(await screen.findByText(/shipped it/)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('shows the error when the detail fails', async () => {
    fetchMock.mockResolvedValue(new Response('claude: logged out\n', { status: 502 }))
    await open()
    await userEvent.click(screen.getByRole('button', { name: 'More detail' }))
    expect(await screen.findByText('claude: logged out')).toBeInTheDocument()
  })

  describe('refreshing the summary', () => {
    const later = new Date(asOf)
    later.setHours(15, 30, 0, 0)
    const refreshButton = () => screen.getByRole('button', { name: 'Refresh summary' })

    it('shows the fresh points and their time, and keeps the card open', async () => {
      let resolve!: (r: Response) => void
      fetchMock.mockReturnValue(new Promise<Response>((r) => (resolve = r)))
      await open()
      await userEvent.click(refreshButton())
      expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s/points', expect.objectContaining({ method: 'POST' }))
      expect(refreshButton()).toBeDisabled() // one refresh at a time
      expect(screen.getByText('fixed the flaky test')).toBeInTheDocument() // the old ones stay meanwhile
      resolve(new Response(JSON.stringify({ Points: ['shipped the fix'], AsOf: later.toISOString() })))
      expect(await screen.findByText('shipped the fix')).toBeInTheDocument()
      expect(screen.getByText('as of 15:30')).toBeInTheDocument()
      expect(screen.queryByText('fixed the flaky test')).toBeNull()
      expect(refreshButton()).toBeEnabled()
    })

    it('keeps the fresh points when a poll brings older ones, and takes newer ones', async () => {
      fetchMock.mockResolvedValue(new Response(JSON.stringify({ Points: ['shipped the fix'], AsOf: later.toISOString() })))
      const { rerender } = render(<Card card={sample} />)
      await userEvent.click(screen.getByTestId('card-meta'))
      await userEvent.click(refreshButton())
      await screen.findByText('shipped the fix')
      rerender(<Card card={{ ...sample }} />)
      expect(screen.getByText('shipped the fix')).toBeInTheDocument()
      const newest = new Date(later)
      newest.setHours(16, 0, 0, 0)
      rerender(<Card card={{ ...sample, TodayPoints: ['moved on'], TodayAsOf: newest.toISOString() }} />)
      expect(screen.getByText('moved on')).toBeInTheDocument()
      expect(screen.getByText('as of 16:00')).toBeInTheDocument()
    })

    it('shows the error and keeps the old points when the refresh fails', async () => {
      fetchMock.mockResolvedValue(new Response('claude: logged out\n', { status: 502 }))
      await open()
      await userEvent.click(refreshButton())
      expect(await screen.findByText('claude: logged out')).toBeInTheDocument()
      expect(screen.getByText('fixed the flaky test')).toBeInTheDocument()
      expect(screen.getByText('as of 14:05')).toBeInTheDocument()
    })

    it('offers a refresh before the first summary of today', async () => {
      await open({ ...sample, TodayPoints: null, TodayAsOf: '0001-01-01T00:00:00Z' })
      expect(refreshButton()).toBeInTheDocument()
    })

    it('offers no refresh for a session with no activity today', async () => {
      await open({ ...sample, LastActivity: '2020-01-01T10:00:00Z', TodayPoints: null, TodayAsOf: '0001-01-01T00:00:00Z' })
      expect(screen.queryByRole('button', { name: 'Refresh summary' })).toBeNull()
    })
  })

  it('opens the session in Orca without toggling the card', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    render(<Card card={sample} />)
    await userEvent.click(screen.getByRole('button', { name: 'Open in Orca' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s/focus', { method: 'POST' })
    expect(screen.queryByText('worked on the thing')).toBeNull()
    vi.unstubAllGlobals()
  })

  it('names the app a session runs in, offers resume for a closed terminal one, and nothing for an unknown live app', () => {
    const label = (Host: string, Live: boolean) => {
      const { unmount } = render(<Card card={{ ...sample, Host, Live }} />)
      const b = screen.queryByRole('button', { name: /^(Open in|Resume)/ })
      const text = b?.textContent ?? null
      unmount()
      return text
    }
    expect(label('orca', true)).toBe('Open in Orca')
    expect(label('vscode', true)).toBe('Open in VS Code')
    expect(label('cursor', true)).toBe('Open in Cursor')
    expect(label('windows-terminal', true)).toBe('Open in Terminal')
    expect(label('console', true)).toBe('Open in Terminal')
    expect(label('terminal', true)).toBe('Open in Terminal')
    expect(label('iterm', true)).toBe('Open in iTerm')
    expect(label('claude-desktop', true)).toBe('Open in Claude app')
    expect(label('', true)).toBeNull()
    // these apps keep a session open between turns, with no claude running
    expect(label('vscode', false)).toBe('Open in VS Code')
    expect(label('cursor', false)).toBe('Open in Cursor')
    expect(label('claude-desktop', false)).toBe('Open in Claude app')
    expect(label('orca', false)).toBe('Resume')
    expect(label('windows-terminal', false)).toBe('Resume')
    expect(label('console', false)).toBe('Resume')
    expect(label('iterm', false)).toBe('Resume')
    expect(label('', false)).toBe('Resume')
  })

  it('offers no way to focus where the app cannot be brought forward (Linux), but still resumes a closed session', () => {
    const label = (Host: string, Live: boolean) => {
      const { unmount } = render(<Card card={{ ...sample, Host, Live, CanFocus: false }} />)
      const b = screen.queryByRole('button', { name: /^(Open in|Resume)/ })
      const text = b?.textContent ?? null
      unmount()
      return text
    }
    expect(label('linux-terminal', true)).toBeNull()
    expect(label('vscode', true)).toBeNull()
    expect(label('', true)).toBeNull()
    // an app-owned session with no claude running is resumed, not opened
    expect(label('vscode', false)).toBe('Resume')
    expect(label('linux-terminal', false)).toBe('Resume')
    expect(label('', false)).toBe('Resume')
  })

  it('resumes an idle app session where focusing is not supported', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    render(<Card card={{ ...sample, Host: 'vscode', Live: false, CanFocus: false }} />)
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s/resume', { method: 'POST' })
    vi.unstubAllGlobals()
  })

  it('opens an app session that has no claude running, rather than resuming it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    render(<Card card={{ ...sample, Host: 'claude-desktop', Live: false }} />)
    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude app' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s/focus', { method: 'POST' })
    vi.unstubAllGlobals()
  })

  it('resumes a closed session', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    render(<Card card={{ ...sample, Live: false }} />)
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s/resume', { method: 'POST' })
    vi.unstubAllGlobals()
  })

  it('says nothing when an app cannot open the session', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('not open in Orca\n', { status: 404 }))
    vi.stubGlobal('fetch', fetchMock)
    render(<Card card={sample} />)
    await userEvent.click(screen.getByRole('button', { name: 'Open in Orca' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    await act(async () => {})
    expect(screen.queryByText('not open in Orca')).toBeNull()
    vi.unstubAllGlobals()
  })

  it('titles a session by its name, else its branch, else its repo, with the repo always in the subtitle', () => {
    const { rerender } = render(<Card card={{ ...sample, Name: 'Bedrock fallback' }} />)
    expect(screen.getByRole('button', { name: 'Bedrock fallback' })).toBeInTheDocument()
    expect(screen.getByTestId('card-meta')).toHaveTextContent(/^github\.com\/x\/s · main · 1\.2 AI-h/)
    rerender(<Card card={{ ...sample, Name: '' }} />)
    expect(screen.getByRole('button', { name: 'main' })).toBeInTheDocument()
    rerender(<Card card={{ ...sample, Name: '', Branch: 'HEAD' }} />)
    expect(screen.getByRole('button', { name: 's' })).toBeInTheDocument()
  })

  it('colours the branch, AI hours and working in both themes, and keeps the repo dim', () => {
    render(<Card card={{ ...sample, Status: 'running' }} />)
    const meta = within(screen.getByTestId('card-meta'))
    expect(meta.getByText('main')).toHaveClass('bg-raised', 'text-tag')
    expect(meta.getByText('1.2 AI-h')).toHaveClass('text-aiH')
    expect(meta.getByText(/working/)).toHaveClass('text-running')
    expect(meta.getByText('github.com/x/s')).not.toHaveClass('text-tag', 'text-aiH', 'text-running')
    expect(screen.getByTestId('card-meta').innerHTML).not.toContain('dark:')
  })

  it('shows AI hours and prompts, not wall hours', () => {
    const { rerender } = render(<Card card={{ ...sample, Prompts: 3 }} />)
    expect(screen.getByTestId('card-meta')).toHaveTextContent('1.2 AI-h · 3 prompts')
    expect(screen.getByTestId('card-meta')).not.toHaveTextContent('wall-h')
    rerender(<Card card={{ ...sample, Prompts: 1 }} />)
    expect(screen.getByTestId('card-meta')).toHaveTextContent('1 prompt')
    expect(screen.getByTestId('card-meta')).not.toHaveTextContent('1 prompts')
  })

  it('renames a session by clicking its title and typing, without opening the card', async () => {
    fetchMock.mockResolvedValue(new Response('{"Name":"Bedrock fallback"}', { status: 200 }))
    render(<Card card={{ ...sample, Name: '' }} />)
    await userEvent.click(screen.getByRole('button', { name: 'main' }))
    const input = screen.getByRole('textbox', { name: 'Session name' })
    await userEvent.clear(input)
    await userEvent.type(input, 'Bedrock fallback{Enter}')
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/s', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ name: 'Bedrock fallback' }) }))
    expect(await screen.findByRole('button', { name: 'Bedrock fallback' })).toBeInTheDocument()
    expect(screen.queryByText('fixed the flaky test')).toBeNull() // the card did not open
  })

  it('leaves the name alone on Escape', async () => {
    render(<Card card={{ ...sample, Name: 'Old name' }} />)
    await userEvent.click(screen.getByRole('button', { name: 'Old name' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Session name' }), 'x{Escape}')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Old name' })).toBeInTheDocument()
  })

  it('shows the names of the sub-agents running now as plain text, and nothing when none run', () => {
    const { rerender } = render(<Card card={{ ...sample, RunningAgents: ['usage', 'Investigate AI hours'] }} />)
    const agents = screen.getByRole('list', { name: 'Running sub-agents' })
    expect(agents).toHaveTextContent('usage · Investigate AI hours')
    for (const li of agents.querySelectorAll('li')) expect(li).not.toHaveClass('rounded-full', 'border')
    rerender(<Card card={{ ...sample, RunningAgents: null }} />)
    expect(screen.queryByRole('list', { name: 'Running sub-agents' })).toBeNull()
  })

  it('heads the running sub-agents with how many run, in the style of the repo name', () => {
    const { rerender } = render(<Card card={{ ...sample, RunningAgents: ['usage'] }} />)
    expect(screen.getByText('1 agent running')).toHaveClass('text-xs', 'text-dim')
    rerender(<Card card={{ ...sample, RunningAgents: ['usage', 'Investigate AI hours'] }} />)
    expect(screen.getByText('2 agents running')).toBeInTheDocument()
    rerender(<Card card={{ ...sample, RunningAgents: [] }} />)
    expect(screen.queryByText(/agents? running/)).toBeNull()
  })

  it('shows whitelisted signals and never tool_call', async () => {
    await open()
    const signals = screen.getByTestId('signals')
    expect(signals.textContent).toContain('3 prompts')
    expect(signals.textContent).toContain('1 commit')
    expect(signals.textContent).not.toContain('tool_call')
  })

  it('keeps a failed resume on screen for 8 seconds', async () => {
    vi.useFakeTimers()
    try {
      fetchMock.mockResolvedValue({ ok: false, status: 409, text: async () => 'no folder to resume in' })
      render(<Card card={{ ...sample, Live: false }} />)
      fireEvent.click(screen.getByRole('button', { name: 'Resume' }))
      await vi.advanceTimersByTimeAsync(0)
      const message = 'no folder to resume in'
      expect(screen.getByText(message)).toBeInTheDocument()
      await vi.advanceTimersByTimeAsync(7900)
      expect(screen.getByText(message)).toBeInTheDocument()
      await vi.advanceTimersByTimeAsync(200)
      expect(screen.queryByText(message)).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it.each([
    [5, 2, '5 prompts · 2 decisions'],
    [1, 1, '1 prompt · 1 decision'],
    [5, 0, '5 prompts'],
    [5, undefined, '5 prompts'],
  ])('counts %s prompts and %s decisions on the top line', (Prompts, Decisions, text) => {
    render(<Card card={{ ...sample, Prompts, Decisions }} />)
    const meta = screen.getByTestId('card-meta').textContent ?? ''
    expect(meta).toContain(text)
    if (!Decisions) expect(meta).not.toContain('decision')
  })

  it.each([
    [{ prompt: 5 }, '5 prompts'],
    [{ prompt: 1 }, '1 prompt'],
    [{ decision: 2, prompt: 5 }, '5 prompts, 2 decisions'],
  ])('words the signals %j with plurals and prompts first', async (Signals, text) => {
    await open({ ...sample, Signals })
    expect(screen.getByTestId('signals')).toHaveTextContent(text)
  })
})

describe('Card for a Codex session', () => {
  const codex: CardData = { ...sample, Agent: 'codex', Title: 'Add a readme', Host: 'codex-app', Live: false }

  it('shows a Codex tag and the title Codex gave the thread, and a Claude tag in its own colour for Claude', () => {
    const { unmount } = render(<Card card={codex} />)
    expect(screen.getByText('Codex')).toHaveClass('text-codex')
    expect(screen.queryByText('Claude')).toBeNull()
    expect(screen.getByRole('button', { name: 'Add a readme' })).toBeInTheDocument()
    unmount()
    render(<Card card={{ ...sample, Agent: 'claude' }} />)
    expect(screen.queryByText('Codex')).toBeNull()
    expect(screen.getByText('Claude')).toHaveClass('text-claude')
  })

  it('tags a card with no agent as Claude', () => {
    render(<Card card={sample} />)
    expect(screen.getByText('Claude')).toBeInTheDocument()
  })

  it("prefers the user's own name to the Codex title", () => {
    render(<Card card={{ ...codex, Name: 'mine' }} />)
    expect(screen.getByRole('button', { name: 'mine' })).toBeInTheDocument()
  })

  it('opens a Codex app session in Codex, and resumes a Codex CLI one', () => {
    const { unmount } = render(<Card card={codex} />)
    expect(screen.getByRole('button', { name: 'Open in Codex' })).toBeInTheDocument()
    unmount()
    render(<Card card={{ ...codex, Host: '' }} />)
    expect(screen.getByRole('button', { name: 'Resume' })).toBeInTheDocument()
  })
})

describe('openAction', () => {
  it('focuses a live session in a known app, and resumes a closed one', () => {
    expect(openAction(sample)).toEqual({ action: 'focus', label: 'Open in Orca' })
    expect(openAction({ ...sample, Live: false, Host: '' })).toEqual({ action: 'resume', label: 'Resume' })
  })

  it('focuses an app-owned session that is not live', () => {
    expect(openAction({ ...sample, Live: false, Host: 'vscode' })).toEqual({ action: 'focus', label: 'Open in VS Code' })
  })

  it('gives nothing for a live session that cannot be focused or has no known app', () => {
    expect(openAction({ ...sample, CanFocus: false })).toBeNull()
    expect(openAction({ ...sample, Host: 'other' })).toBeNull()
  })
})

describe('when the agent finished', () => {
  const meta = () => screen.getByTestId('card-meta')
  const ago = (minutes: number) => new Date(Date.now() - minutes * 60000).toISOString()

  it.each(['waiting', 'idle'])('shows on a %s session', (Status) => {
    render(<Card card={{ ...sample, Status, FinishedAt: ago(5) }} />)
    expect(meta()).toHaveTextContent('finished 5 minutes ago')
  })

  it('does not show while it is running', () => {
    render(<Card card={{ ...sample, Status: 'running', FinishedAt: ago(1) }} />)
    expect(meta()).not.toHaveTextContent('finished')
    expect(meta()).toHaveTextContent('working')
  })

  it('leaves it out without a time', () => {
    render(<Card card={{ ...sample, FinishedAt: undefined }} />)
    expect(meta()).not.toHaveTextContent('finished')
  })
})

describe('how long the current task has run', () => {
  const meta = () => screen.getByTestId('card-meta')
  afterEach(() => vi.useRealTimers())

  it('counts up by itself, with no new data', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-05T12:05:00Z'))
    render(<Card card={{ ...sample, Status: 'running', WorkingSince: '2026-11-05T12:00:00Z' }} />)
    expect(meta()).toHaveTextContent('working 5m 00s')
    act(() => {
      vi.advanceTimersByTime(30_000)
    })
    expect(meta()).toHaveTextContent('working 5m 30s')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('still says working without a start', () => {
    render(<Card card={{ ...sample, Status: 'running' }} />)
    expect(meta()).toHaveTextContent(/working$/)
  })

  it('does not show once it is waiting', () => {
    render(<Card card={{ ...sample, Status: 'waiting', WorkingSince: '2026-11-05T12:00:00Z' }} />)
    expect(meta()).not.toHaveTextContent('working')
  })
})

describe('background tasks', () => {
  it('notes them on a waiting session', () => {
    render(<Card card={{ ...sample, Status: 'waiting', BackgroundTasks: 1 }} />)
    expect(screen.getByText('1 background task running')).toBeInTheDocument()
  })

  it('counts more than one', () => {
    render(<Card card={{ ...sample, Status: 'running', BackgroundTasks: 2 }} />)
    expect(screen.getByText('2 background tasks running')).toBeInTheDocument()
  })

  it('says nothing without any, or on an idle session', () => {
    const { unmount } = render(<Card card={{ ...sample, Status: 'waiting' }} />)
    expect(screen.queryByText(/background task/)).toBeNull()
    unmount()
    render(<Card card={{ ...sample, Status: 'idle', BackgroundTasks: 1 }} />)
    expect(screen.queryByText(/background task/)).toBeNull()
  })
})
