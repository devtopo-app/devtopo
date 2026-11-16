import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StatusBar, untilReset } from './StatusBar'

const status = { daemon: 'live', activeSessions: 1, waiting: 0, dayAIHours: 2, asOf: '' }

function serve(usage: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) =>
      Promise.resolve(new Response(JSON.stringify(url === '/api/usage' ? usage : status), { status: 200 })),
    ),
  )
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-10-29T12:00:00Z'))
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('StatusBar usage', () => {
  it('draws one bar per window, filled to the percent and coloured by level', async () => {
    serve({
      Windows: [
        { Label: '5h', Percent: 12.4, ResetsAt: '2026-10-29T16:40:00Z' },
        { Label: 'week', Percent: 70, ResetsAt: '2026-11-02T15:00:00Z' },
        { Label: 'Fable', Percent: 90, ResetsAt: '0001-01-01T00:00:00Z' },
      ],
      Status: '',
    })
    render(<StatusBar />)

    const five = await screen.findByTestId('usage-5h')
    expect(five).toHaveTextContent('5h')
    expect(five).toHaveTextContent('4h 40m')
    expect(five).toHaveAttribute('title', expect.stringContaining('12% used'))
    const fill = screen.getByTestId('usage-fill-5h')
    expect(fill.style.width).toBe('12.4%')
    expect(fill.style.background).toBe('var(--accent)')

    expect(screen.getByTestId('usage-week')).toHaveTextContent('4d 3h')
    expect(screen.getByTestId('usage-fill-week').style.background).toBe('var(--waiting)')

    expect(screen.getByTestId('usage-Fable')).toHaveTextContent('Fable')
    expect(screen.getByTestId('usage-fill-Fable').style.background).toBe('var(--danger)')
  })

  it('puts the Claude mark before the gauges', async () => {
    serve({ Windows: [{ Label: '5h', Percent: 10, ResetsAt: '' }], Status: '' })
    render(<StatusBar />)
    const gauge = await screen.findByTestId('usage-5h')
    const mark = screen.getByRole('img', { name: 'Claude usage' })
    expect(mark.compareDocumentPosition(gauge) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('shows the status quietly when there are no windows', async () => {
    serve({ Windows: [], Status: 'sign in to Claude Code again' })
    render(<StatusBar />)
    expect(await screen.findByText('sign in to Claude Code again')).toHaveClass('text-dim')
  })
})

describe('untilReset', () => {
  const now = new Date('2026-10-29T12:00:00Z')
  it('reads as days and hours, hours and minutes, or minutes', () => {
    expect(untilReset('2026-11-02T15:30:00Z', now)).toBe('4d 3h')
    expect(untilReset('2026-10-29T16:40:00Z', now)).toBe('4h 40m')
    expect(untilReset('2026-10-29T12:35:00Z', now)).toBe('35m')
  })
  it('is empty for no or past reset', () => {
    expect(untilReset('0001-01-01T00:00:00Z', now)).toBe('')
    expect(untilReset('2026-10-29T11:00:00Z', now)).toBe('')
  })
})

describe('StatusBar Codex usage', () => {
  it('draws a Codex group after the Claude one, with its own mark', async () => {
    serve({
      Windows: [{ Label: '5h', Percent: 10, ResetsAt: '' }],
      Status: '',
      Codex: [{ Label: '30d', Percent: 3, ResetsAt: '2026-11-28T12:00:00Z' }],
    })
    render(<StatusBar />)
    const codex = await screen.findByTestId('usage-codex-30d')
    expect(codex).toHaveTextContent('30d')
    expect(codex).toHaveTextContent('30d 0h')
    const mark = screen.getByRole('img', { name: 'Codex usage' })
    // OpenAI's six-fold knot: one link drawn six times around the centre.
    expect(mark.querySelectorAll('[transform^="rotate"]')).toHaveLength(6)
    expect(screen.getByTestId('usage-5h').compareDocumentPosition(mark) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(mark.compareDocumentPosition(codex) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('shows the Codex group even when Claude has only a status', async () => {
    serve({ Windows: [], Status: 'Claude Code not signed in', Codex: [{ Label: '5h', Percent: 50, ResetsAt: '' }] })
    render(<StatusBar />)
    expect(await screen.findByTestId('usage-codex-5h')).toBeInTheDocument()
    expect(screen.getByText('Claude Code not signed in')).toBeInTheDocument()
  })

  it('hides the Codex group when there is no Codex data', async () => {
    serve({ Windows: [{ Label: '5h', Percent: 10, ResetsAt: '' }], Status: '', Codex: [] })
    render(<StatusBar />)
    await screen.findByTestId('usage-5h')
    expect(screen.queryByRole('img', { name: 'Codex usage' })).toBeNull()
  })
})

describe('StatusBar update chip', () => {
  function serveUpdate(update: unknown) {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        const body = url === '/api/update' ? update : url === '/api/usage' ? { Windows: [], Status: '' } : status
        return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
      }),
    )
  }

  it('shows a quiet link to Settings when a newer release is out', async () => {
    serveUpdate({ Current: 'v1.0.0', Latest: 'v1.2.0', Available: true, Supported: true, Enabled: true })
    render(<StatusBar />)
    const chip = await screen.findByRole('link', { name: /update available: v1\.2\.0/i })
    expect(chip).toHaveAttribute('href', '#/settings?section=updates')
    expect(chip).toHaveClass('text-2xs')
  })

  it('shows nothing when up to date or the check is unsupported', async () => {
    serveUpdate({ Current: 'v1.2.0', Latest: 'v1.2.0', Available: false, Supported: true, Enabled: true })
    render(<StatusBar />)
    await screen.findByText(/running/)
    expect(screen.queryByText(/update available/i)).not.toBeInTheDocument()
  })
})
